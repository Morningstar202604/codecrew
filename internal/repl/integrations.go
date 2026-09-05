package repl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codecrew/internal/disp"
	"codecrew/internal/eval"
	"codecrew/internal/knowledge"
	"codecrew/internal/llm"
	"codecrew/internal/orchestration"
	"codecrew/internal/planner"
	"codecrew/internal/reasoning"
	"codecrew/internal/tool"
	"codecrew/internal/verify"
)

// initReasoning 初始化推理配置、失败存储和反思引擎。
func (r *REPL) initReasoning() {
	cfg := r.cfg.Reasoning
	r.reasoningCfg = reasoning.Config{
		Mode:              reasoning.ParseMode(cfg.Mode),
		ShowThoughts:      cfg.GetShowThoughts(),
		AutoReflect:       cfg.GetAutoReflect(),
		ReflectionDepth:   cfg.ReflectionDepth,
		InjectReflections: cfg.GetInjectReflections(),
	}
	r.reasoningCfg.Validate()
	// 失败存储
	if base, err := os.UserHomeDir(); err == nil {
		r.failureStore = reasoning.NewFailureStore(filepath.Join(base, ".codecrew"))
	}
	// 反思引擎（如果 client 已初始化）
	if r.client != nil {
		r.reflexion = reasoning.NewReflexionEngine(&llmAdapter{client: r.client}, r.reasoningCfg, r.failureStore)
	}
}

// rebuildReflexion 在 client 变化后重建反思引擎。
func (r *REPL) rebuildReflexion() {
	if r.client != nil {
		r.reflexion = reasoning.NewReflexionEngine(&llmAdapter{client: r.client}, r.reasoningCfg, r.failureStore)
	}
}

// triggerReflexion 触发反思（如果配置允许）。
func (r *REPL) triggerReflexion(ctx context.Context, task, summary string, failed bool) {
	if r.reflexion == nil || !r.reflexion.ShouldReflect(failed) {
		return
	}
	roleName := r.current.Name
	if summary == "" {
		summary = "（无输出摘要）"
	}
	fmt.Fprintf(r.out, "\n  %s\n", dim("🔄 自我反思中..."))
	result := r.reflexion.Reflect(ctx, task, summary, roleName, failed)
	if result.Error != nil {
		fmt.Fprintf(r.out, "  %s\n", dim("反思失败: "+result.Error.Error()))
		return
	}
	if result.Content == "" {
		return
	}
	if failed {
		fmt.Fprintf(r.out, "  %s\n", "📝 失败反思：")
	} else {
		fmt.Fprintf(r.out, "  %s\n", "📝 执行反思：")
	}
	for _, line := range strings.Split(result.Content, "\n") {
		if strings.TrimSpace(line) != "" {
			fmt.Fprintf(r.out, "    %s\n", dim(line))
		}
	}
	fmt.Fprintf(r.out, "  %s\n", dim(fmt.Sprintf("（反思耗时 %s，已存入 %s 的经验库）", result.Duration.Round(time.Millisecond), roleName)))
}

// handleReasoning 处理 /reasoning 命令，查看或切换推理模式。
func (r *REPL) handleReasoning(arg string) {
	if arg == "" {
		fmt.Fprintf(r.out, "\n  当前推理模式: %s\n", bright(r.reasoningCfg.Mode.String()))
		fmt.Fprintf(r.out, "  显示思考过程: %v\n", r.reasoningCfg.ShowThoughts)
		fmt.Fprintf(r.out, "  自动反思: %v\n", r.reasoningCfg.AutoReflect)
		fmt.Fprintf(r.out, "  反思深度: %d\n", r.reasoningCfg.ReflectionDepth)
		fmt.Fprintln(r.out, "  支持模式:")
		fmt.Fprintln(r.out, "    standard  - 标准模式（隐式 ReAct，现有行为）")
		fmt.Fprintln(r.out, "    react     - 显式 ReAct（Thought → Action → Observation）")
		fmt.Fprintln(r.out, "    reflexion - 反思模式（任务完成后自动反思，失败时深度反思）")
		fmt.Fprintln(r.out, "  用法: /reasoning <模式>")
		return
	}
	mode := reasoning.ParseMode(arg)
	r.reasoningCfg.Mode = mode
	r.rebuildReflexion()
	// 重新应用角色（更新 system prompt）
	r.applyRole(r.current)
	r.history[0] = llm.TextMessage("system", r.systemPromptFor(r.current))
	fmt.Fprintf(r.out, "\n  ✓ 推理模式已切换为: %s\n", bright(mode.String()))
	if mode == reasoning.ModeReAct || mode == reasoning.ModeReflexion {
		fmt.Fprintln(r.out, "  模型将显式输出思考过程（Thought:），任务完成后自动反思")
	}
}

// handleFailures 处理 /failures 命令，查看或清空失败经验。
func (r *REPL) handleFailures(arg string) {
	if r.failureStore == nil {
		fmt.Fprintln(r.out, "\n  ⚠ 失败经验存储不可用")
		return
	}
	if arg == "clear" {
		if err := r.failureStore.Clear(r.current.Name); err != nil {
			fmt.Fprintf(r.out, "\n  ✗ 清空失败: %v\n", err)
		} else {
			fmt.Fprintf(r.out, "\n  ✓ 已清空 %s 的失败经验\n", r.current.Name)
		}
		return
	}
	list, err := r.failureStore.List(r.current.Name)
	if err != nil {
		fmt.Fprintf(r.out, "\n  ✗ 读取失败经验: %v\n", err)
		return
	}
	if len(list) == 0 {
		fmt.Fprintf(r.out, "\n  %s 还没有失败经验\n", r.current.Name)
		return
	}
	fmt.Fprintf(r.out, "\n  %s 的失败经验（共 %d 条，最新在前）:\n", r.current.Name, len(list))
	for i, f := range list[:min(10, len(list))] {
		fmt.Fprintf(r.out, "    %d. [%s] %s\n", i+1, f.Timestamp.Format("2006-01-02 15:04"), disp.Truncate(f.Task, 50))
		fmt.Fprintf(r.out, "       错误: %s\n", dim(disp.Truncate(f.Error, 80)))
		if f.Reflection != "" {
			fmt.Fprintf(r.out, "       反思: %s\n", dim(disp.Truncate(f.Reflection, 80)))
		}
	}
	if len(list) > 10 {
		fmt.Fprintf(r.out, "    ... 还有 %d 条\n", len(list)-10)
	}
	fmt.Fprintln(r.out, "  用法: /failures clear 清空失败经验")
}

// initVerify 初始化验证引擎。
func (r *REPL) initVerify() {
	cfg := r.cfg.Verify
	commands := cfg.Commands
	if len(commands) == 0 {
		// 自动检测项目类型
		workingDir := r.cfg.WorkingDir
		if workingDir == "" {
			workingDir = "."
		}
		commands = verify.DetectCommands(workingDir)
	}
	r.verifyCfg = verify.Config{
		Enabled:         cfg.GetEnabled(),
		AutoVerify:      cfg.GetAutoVerify(),
		Commands:        commands,
		MaxRepairRounds: cfg.MaxRepairRounds,
		TimeoutSeconds:  cfg.TimeoutSeconds,
		WorkingDir:      r.cfg.WorkingDir,
	}
	if r.verifyCfg.Enabled {
		r.verifyEngine = verify.NewEngine(r.verifyCfg)
	}
}

// runVerify 执行验证，返回结果。
func (r *REPL) runVerify(ctx context.Context) verify.Result {
	if r.verifyEngine == nil {
		return verify.Result{Passed: true, Total: 0}
	}
	fmt.Fprintf(r.out, "\n  %s\n", dim("🔍 正在验证代码..."))
	result := r.verifyEngine.Run(ctx)
	r.printVerifyResult(result)
	return result
}

// printVerifyResult 打印验证结果。
func (r *REPL) printVerifyResult(result verify.Result) {
	if result.Total == 0 {
		fmt.Fprintln(r.out, "  ⚠ 没有配置验证命令")
		return
	}
	for _, c := range result.Commands {
		if c.Passed {
			fmt.Fprintf(r.out, "  %s %s (%s)\n", bright("✓"), c.Command, c.Duration.Round(time.Millisecond))
		} else {
			fmt.Fprintf(r.out, "  %s %s (%s)\n", bright("✗"), c.Command, c.Duration.Round(time.Millisecond))
			// 显示错误摘要（前 5 行）
			summary := verify.ExtractErrorSummary(c.Output, 5)
			for _, line := range strings.Split(summary, "\n") {
				if strings.TrimSpace(line) != "" {
					fmt.Fprintf(r.out, "    %s\n", dim(line))
				}
			}
		}
	}
	fmt.Fprintf(r.out, "  %s\n", result.Summary())
}

// autoVerifyAfterTool 在工具调用后自动验证（仅当修改了文件时）。
func (r *REPL) autoVerifyAfterTool(ctx context.Context, toolName string) {
	if !r.verifyCfg.AutoVerify || r.verifyEngine == nil {
		return
	}
	// 只有 write 和 edit 工具会修改文件
	if toolName != "write" && toolName != "edit" {
		return
	}
	result := r.runVerify(ctx)
	if !result.Passed {
		// 验证失败，触发修复循环
		r.repairLoop(ctx, result)
	}
}

// repairLoop 修复循环：验证失败后，让模型分析错误并修复，再验证，直到通过或达到上限。
func (r *REPL) repairLoop(ctx context.Context, initialResult verify.Result) verify.RepairResult {
	maxRounds := r.verifyCfg.GetMaxRepairRounds()
	result := verify.RepairResult{MaxRounds: maxRounds}
	currentResult := initialResult

	for round := 1; round <= maxRounds; round++ {
		if currentResult.Passed {
			result.Fixed = true
			result.FinalResult = &currentResult
			break
		}

		fmt.Fprintf(r.out, "\n  %s\n", dim(fmt.Sprintf("🔧 第 %d/%d 轮修复中...", round, maxRounds)))

		// 构造修复 prompt
		errors := currentResult.FailedOutput()
		repairPrompt := verify.BuildRepairPrompt(errors, round, maxRounds)

		// 将修复请求加入历史
		r.history = append(r.history, llm.TextMessage("user", repairPrompt))

		// 调用模型修复（使用 runTurn 的内部逻辑，但不触发新的自动验证）
		text, calls, _, err := r.client.Chat(ctx, r.history, r.registry.Schemas(), func(delta string) {
			fmt.Fprint(r.out, delta)
		})
		if err != nil {
			fmt.Fprintf(r.out, "\n  ✗ 修复调用失败: %v\n", err)
			result.Rounds = append(result.Rounds, verify.RepairRound{
				Round: round, Fixed: false, Summary: "修复调用失败: " + err.Error(),
			})
			break
		}
		if text != "" {
			fmt.Fprintln(r.out)
		}

		// 执行工具调用（修复代码）
		if len(calls) > 0 {
			if err := r.runToolCalls(ctx, calls); err != nil {
				fmt.Fprintf(r.out, "  ✗ 修复工具执行失败: %v\n", err)
			}
		}

		// 将模型回复加入历史
		r.history = append(r.history, llm.Message{Role: "assistant", Content: text, ToolCalls: calls})

		// 再次验证
		fmt.Fprintf(r.out, "\n  %s\n", dim("🔍 重新验证..."))
		currentResult = r.verifyEngine.Run(ctx)
		r.printVerifyResult(currentResult)

		result.Rounds = append(result.Rounds, verify.RepairRound{
			Round:    round,
			Fixed:    currentResult.Passed,
			Summary:  text,
			ErrorOut: currentResult.FailedOutput(),
		})

		if currentResult.Passed {
			result.Fixed = true
			result.FinalResult = &currentResult
			break
		}
	}

	// 打印修复结果
	fmt.Fprintln(r.out)
	if result.Fixed {
		fmt.Fprintf(r.out, "  %s\n", bright(result.Summary()))
	} else {
		fmt.Fprintf(r.out, "  %s\n", result.Summary())
		fmt.Fprintln(r.out, "  建议：手动检查错误，或增加 max_repair_rounds")
	}

	return result
}

// handleVerify 处理 /verify 命令。
func (r *REPL) handleVerify(arg string) {
	if r.verifyEngine == nil {
		fmt.Fprintln(r.out, "\n  ⚠ 验证功能未启用")
		return
	}
	if arg == "config" {
		fmt.Fprintf(r.out, "\n  验证配置:\n")
		fmt.Fprintf(r.out, "    启用: %v\n", r.verifyCfg.Enabled)
		fmt.Fprintf(r.out, "    自动验证: %v\n", r.verifyCfg.AutoVerify)
		fmt.Fprintf(r.out, "    最大修复轮数: %d\n", r.verifyCfg.GetMaxRepairRounds())
		fmt.Fprintf(r.out, "    验证命令:\n")
		for _, cmd := range r.verifyCfg.Commands {
			fmt.Fprintf(r.out, "      - %s\n", cmd)
		}
		return
	}
	if arg == "repair" {
		// 手动触发验证+修复
		result := r.runVerify(context.Background())
		if !result.Passed {
			r.repairLoop(context.Background(), result)
		}
		return
	}
	// 默认：只验证不修复
	r.runVerify(context.Background())
}

// initPlanner 初始化规划器。
func (r *REPL) initPlanner() {
	cfg := r.cfg.Planner
	r.plannerEnabled = cfg.GetEnabled()
	if r.client != nil {
		r.decomposer = planner.NewDecomposer(&llmAdapter{client: r.client})
	}
}

// initKnowledge 初始化知识系统。
func (r *REPL) initKnowledge() {
	cfg := r.cfg.Knowledge
	if !cfg.GetEnabled() {
		return
	}

	workingDir := r.cfg.WorkingDir
	if workingDir == "" {
		workingDir = "."
	}

	// 创建代码库索引
	knowledgeCfg := knowledge.Config{
		Enabled:       cfg.GetEnabled(),
		AutoIndex:     cfg.GetAutoIndex(),
		IndexInterval: cfg.IndexInterval,
		MaxResults:    cfg.MaxResults,
		ContextLines:  cfg.ContextLines,
	}
	r.codebaseIndex = knowledge.NewCodebaseIndex(knowledgeCfg, workingDir)

	// 尝试加载已有索引
	if !r.codebaseIndex.Load() {
		// 没有缓存索引，如果开启自动索引则构建
		if cfg.GetAutoIndex() {
			fmt.Fprintf(r.out, "  %s\n", dim("📚 正在构建代码库索引..."))
			if err := r.codebaseIndex.Build(); err != nil {
				fmt.Fprintf(r.out, "  %s\n", dim("⚠ 索引构建失败: "+err.Error()))
			}
		}
	} else if r.codebaseIndex.IsStale() && cfg.GetAutoIndex() {
		// 索引过期，后台更新
		go func() {
			r.codebaseIndex.Build()
		}()
	}

	// 创建搜索器
	r.searcher = knowledge.NewSearcher(r.codebaseIndex)

	// 创建情景记忆存储
	r.episodicStore = knowledge.NewEpisodicStore()

	// 注册 search_code 工具
	if r.searcher != nil {
		r.registry.Register(tool.NewSearchCodeTool(r.searcher), tool.DecisionAsk)
	}
}

// handleIndex 处理 /index 命令。
func (r *REPL) handleIndex(arg string) {
	if r.codebaseIndex == nil {
		fmt.Fprintln(r.out, "\n  ⚠ 知识系统未启用")
		return
	}

	switch arg {
	case "status", "":
		meta := r.codebaseIndex.Meta()
		fmt.Fprintf(r.out, "\n  代码库索引状态:\n")
		fmt.Fprintf(r.out, "    根目录: %s\n", meta.RootDir)
		fmt.Fprintf(r.out, "    文件数: %d\n", meta.FileCount)
		fmt.Fprintf(r.out, "    符号数: %d\n", meta.SymbolCount)
		fmt.Fprintf(r.out, "    最后更新: %s\n", meta.UpdatedAt.Format("2006-01-02 15:04:05"))
		fmt.Fprintf(r.out, "    是否过期: %v\n", r.codebaseIndex.IsStale())
		fmt.Fprintln(r.out, "\n  用法: /index build|status|files|search <关键词>")

	case "build":
		fmt.Fprintln(r.out, "\n  📚 正在构建代码库索引...")
		if err := r.codebaseIndex.Build(); err != nil {
			fmt.Fprintf(r.out, "  ✗ 索引构建失败: %v\n", err)
		} else {
			meta := r.codebaseIndex.Meta()
			fmt.Fprintf(r.out, "  ✓ 索引构建完成: %d 个文件, %d 个符号\n", meta.FileCount, meta.SymbolCount)
		}

	case "files":
		files := r.codebaseIndex.Files()
		fmt.Fprintf(r.out, "\n  索引文件列表（共 %d 个）:\n", len(files))
		for i, f := range files {
			if i >= 30 {
				fmt.Fprintf(r.out, "    ... 还有 %d 个文件\n", len(files)-30)
				break
			}
			fmt.Fprintf(r.out, "    %s (%s, %d 行, %d 符号)\n", f.Path, f.Language, f.Lines, len(f.Symbols))
		}

	default:
		// /index search <关键词> 或直接 /index <关键词>
		query := arg
		if strings.HasPrefix(query, "search ") {
			query = strings.TrimPrefix(query, "search ")
		}
		if r.searcher == nil {
			fmt.Fprintln(r.out, "\n  ⚠ 搜索器未初始化")
			return
		}
		results := r.searcher.Search(query, 5)
		if len(results) == 0 {
			fmt.Fprintf(r.out, "\n  没有找到与 %q 匹配的结果\n", query)
			return
		}
		fmt.Fprintf(r.out, "\n  搜索结果（关键词: %q）:\n\n", query)
		for i, res := range results {
			symbol := ""
			if res.SymbolName != "" {
				symbol = fmt.Sprintf(" [%s]", res.SymbolName)
			}
			fmt.Fprintf(r.out, "  %d. %s:%d%s (相关性: %.2f)\n", i+1, res.File, res.Line, symbol, res.Score)
			fmt.Fprintf(r.out, "     %s\n", disp.Truncate(res.Content, 80))
		}
	}
}

// recordEpisodicMemory 记录一条情景记忆。
func (r *REPL) recordEpisodicMemory(task, result string, success bool, files []string) {
	if r.episodicStore == nil {
		return
	}
	sessionID := ""
	if r.session != nil {
		sessionID = r.session.Meta().ID
	}
	r.episodicStore.Add(task, result, success, files, sessionID)
}

// initOrchestration 初始化编排和评估组件。
func (r *REPL) initOrchestration() {
	r.supervisorState = orchestration.NewSupervisorState()
	r.hitlState = orchestration.NewHITLState()
	if r.client != nil {
		r.evalHarness = eval.NewHarness(&llmAdapter{client: r.client})
	}
}

// handleSupervisor 处理 /supervisor 命令。
func (r *REPL) handleSupervisor(arg string) {
	if r.supervisorState == nil {
		r.supervisorState = orchestration.NewSupervisorState()
	}

	switch {
	case arg == "on":
		r.supervisorState.Enabled = true
		fmt.Fprintln(r.out, "\n  ✓ Supervisor 模式已开启")
	case arg == "off":
		r.supervisorState.Enabled = false
		fmt.Fprintln(r.out, "\n  ✓ Supervisor 模式已关闭")
	case arg == "status" || arg == "":
		fmt.Fprintln(r.out)
		fmt.Fprintln(r.out, r.supervisorState.Render())
	case strings.HasPrefix(arg, "assign "):
		// /supervisor assign <worker> <task>
		parts := strings.SplitN(strings.TrimPrefix(arg, "assign "), " ", 2)
		if len(parts) < 2 {
			fmt.Fprintln(r.out, "\n  用法: /supervisor assign <worker> <task>")
			return
		}
		assignment := r.supervisorState.AssignTask(parts[1], parts[0])
		fmt.Fprintf(r.out, "\n  ✓ 已分配任务 #%d 给 %s: %s\n", assignment.ID, assignment.Worker, assignment.Task)
	case strings.HasPrefix(arg, "done "):
		// /supervisor done <id> <result>
		parts := strings.SplitN(strings.TrimPrefix(arg, "done "), " ", 2)
		if len(parts) < 1 {
			fmt.Fprintln(r.out, "\n  用法: /supervisor done <id> [result]")
			return
		}
		id := 0
		fmt.Sscanf(parts[0], "%d", &id)
		result := ""
		if len(parts) > 1 {
			result = parts[1]
		}
		r.supervisorState.UpdateTask(id, "done", result)
		fmt.Fprintf(r.out, "\n  ✓ 任务 #%d 已标记完成\n", id)
	default:
		// /supervisor <goal>：设置目标并开启
		r.supervisorState.Goal = arg
		r.supervisorState.Enabled = true
		fmt.Fprintf(r.out, "\n  ✓ Supervisor 目标已设置: %s\n", arg)
		fmt.Fprintln(r.out, "  用法: /supervisor assign <worker> <task> 分配任务")
	}
}

// handleApprove 处理 /approve 命令。
func (r *REPL) handleApprove(arg string) {
	if r.hitlState == nil {
		fmt.Fprintln(r.out, "\n  ⚠ HITL 未初始化")
		return
	}
	if arg == "" {
		fmt.Fprintln(r.out)
		fmt.Fprintln(r.out, r.hitlState.Render())
		return
	}
	id := 0
	fmt.Sscanf(arg, "%d", &id)
	if r.hitlState.Approve(id) {
		fmt.Fprintf(r.out, "\n  ✓ 已批准操作 #%d\n", id)
	} else {
		fmt.Fprintf(r.out, "\n  ✗ 未找到待审批操作 #%d\n", id)
	}
}

// handleDeny 处理 /deny 命令。
func (r *REPL) handleDeny(arg string) {
	if r.hitlState == nil {
		fmt.Fprintln(r.out, "\n  ⚠ HITL 未初始化")
		return
	}
	if arg == "" {
		fmt.Fprintln(r.out, "\n  用法: /deny <ID>")
		return
	}
	id := 0
	fmt.Sscanf(arg, "%d", &id)
	if r.hitlState.Deny(id) {
		fmt.Fprintf(r.out, "\n  ✓ 已拒绝操作 #%d\n", id)
	} else {
		fmt.Fprintf(r.out, "\n  ✗ 未找到待审批操作 #%d\n", id)
	}
}

// handleEval 处理 /eval 命令。
func (r *REPL) handleEval(arg string) {
	if r.evalHarness == nil {
		if r.client != nil {
			r.evalHarness = eval.NewHarness(&llmAdapter{client: r.client})
		} else {
			fmt.Fprintln(r.out, "\n  ⚠ 评估框架未初始化")
			return
		}
	}

	switch {
	case arg == "run" || arg == "":
		fmt.Fprintln(r.out, "\n  📊 正在运行评估...")
		report := r.evalHarness.Run(context.Background(), "默认评估集", nil)
		fmt.Fprintln(r.out)
		fmt.Fprintln(r.out, eval.RenderReport(report))
	case arg == "list":
		reports, err := r.evalHarness.ListReports()
		if err != nil {
			fmt.Fprintf(r.out, "\n  ✗ 获取报告列表失败: %v\n", err)
			return
		}
		if len(reports) == 0 {
			fmt.Fprintln(r.out, "\n  暂无评估报告")
			return
		}
		fmt.Fprintf(r.out, "\n  历史评估报告（共 %d 份）:\n", len(reports))
		for i, rep := range reports {
			if i >= 10 {
				fmt.Fprintf(r.out, "    ... 还有 %d 份\n", len(reports)-10)
				break
			}
			fmt.Fprintf(r.out, "    %s | %s | 通过率 %.1f%% | 得分 %d/%d\n",
				rep.StartedAt.Format("2006-01-02 15:04"), rep.Name, rep.PassRate, rep.TotalScore, rep.MaxScore)
		}
	default:
		fmt.Fprintln(r.out, "\n  用法: /eval [run|list]")
		fmt.Fprintln(r.out, "    run  - 运行默认评估集")
		fmt.Fprintln(r.out, "    list - 查看历史报告")
	}
}

// handlePlan 处理 /plan 命令。
func (r *REPL) handlePlan(arg string) {
	// 计划模式控制命令
	switch arg {
	case "on":
		r.plannerEnabled = true
		fmt.Fprintln(r.out, "\n  ✓ 计划模式已开启。复杂任务将先分解为子任务再逐步执行。")
		return
	case "off":
		r.plannerEnabled = false
		r.currentPlan = nil
		fmt.Fprintln(r.out, "\n  ✓ 计划模式已关闭。")
		return
	case "mode":
		fmt.Fprintf(r.out, "\n  计划模式: %s\n", map[bool]string{true: "开启", false: "关闭"}[r.plannerEnabled])
		if r.currentPlan != nil {
			fmt.Fprintln(r.out)
			fmt.Fprintln(r.out, r.currentPlan.Render())
		} else {
			fmt.Fprintln(r.out, "  当前没有执行中的计划")
		}
		fmt.Fprintln(r.out, "\n  用法: /plan on|off|mode")
		return
	case "clear":
		r.currentPlan = nil
		if r.plan != nil {
			r.plan.Execute(context.Background(), map[string]any{"action": "clear"})
		}
		fmt.Fprintln(r.out, "\n  ✓ 已清除当前计划")
		return
	}

	// /plan <目标>：如果目标较长，触发规划执行；否则添加到 PlanTool
	if arg != "" && len(arg) > 20 {
		r.runPlanMode(context.Background(), arg)
		return
	}

	// 兼容已有 PlanTool：/plan 显示计划，/plan <短描述> 添加任务
	if arg == "" {
		r.printPlan()
	} else {
		r.addPlanTask(arg)
	}
}

// runPlanMode 执行计划模式：分解任务 → 逐步执行 → 动态调整 → 总结。
func (r *REPL) runPlanMode(ctx context.Context, goal string) {
	if r.decomposer == nil {
		fmt.Fprintln(r.out, "\n  ⚠ 规划器未初始化")
		return
	}

	fmt.Fprintf(r.out, "\n  %s\n", dim("📋 正在分解任务..."))

	// 1. 分解任务
	plan, err := r.decomposer.Decompose(ctx, goal, "")
	if err != nil {
		fmt.Fprintf(r.out, "  ✗ 任务分解失败: %v\n", err)
		// 降级为普通执行
		r.runTurn(goal)
		return
	}
	r.currentPlan = plan

	fmt.Fprintln(r.out)
	fmt.Fprintln(r.out, plan.Render())

	// 2. 逐步执行
	adjustRounds := 0
	maxAdjust := 2
	if r.cfg.Planner.MaxAdjustRounds > 0 {
		maxAdjust = r.cfg.Planner.MaxAdjustRounds
	}

	for !plan.IsComplete() {
		task := plan.NextTask()
		if task == nil {
			// 没有可执行的任务，可能有依赖问题
			if plan.HasFailed() {
				fmt.Fprintln(r.out, "\n  ⚠ 有任务失败且无法继续，计划终止")
			} else {
				fmt.Fprintln(r.out, "\n  ⚠ 没有可执行的任务，计划终止")
			}
			break
		}

		// 标记任务为执行中
		plan.UpdateTaskStatus(task.ID, planner.StatusRunning)
		fmt.Fprintf(r.out, "\n  %s\n", dim(fmt.Sprintf("▶ 执行任务 #%d: %s", task.ID, task.Title)))

		// 执行任务（调用普通的 runTurn，但用任务描述作为输入）
		taskPrompt := planner.ExecuteTaskPrompt(task, plan)
		r.history = append(r.history, llm.TextMessage("user", taskPrompt))
		r.appendSession(llm.TextMessage("user", taskPrompt))

		// 执行一轮（简化版，不触发新的计划模式）
		text, calls, _, err := r.executeOneTurn(ctx)
		if err != nil {
			plan.UpdateTaskStatus(task.ID, planner.StatusFailed, err.Error())
			fmt.Fprintf(r.out, "  ✗ 任务 #%d 失败: %v\n", task.ID, err)

			// 自动调整计划
			if r.cfg.Planner.GetAutoAdjust() && adjustRounds < maxAdjust {
				adjustRounds++
				fmt.Fprintf(r.out, "  %s\n", dim(fmt.Sprintf("🔄 自动调整计划（第 %d/%d 轮）...", adjustRounds, maxAdjust)))
				newPlan, err := r.decomposer.AdjustPlan(ctx, plan, task, err.Error())
				if err == nil && newPlan != nil {
					plan = newPlan
					r.currentPlan = plan
					fmt.Fprintln(r.out, plan.Render())
					continue
				}
			}
			continue
		}

		// 任务完成
		result := text
		if result == "" && len(calls) > 0 {
			result = fmt.Sprintf("执行了 %d 个工具调用", len(calls))
		}
		plan.UpdateTaskStatus(task.ID, planner.StatusDone, disp.Truncate(result, 100))
		fmt.Fprintf(r.out, "  ✓ 任务 #%d 完成\n", task.ID)
	}

	// 3. 显示最终结果
	fmt.Fprintln(r.out)
	fmt.Fprintln(r.out, plan.Render())

	if plan.IsComplete() && !plan.HasFailed() {
		fmt.Fprintln(r.out, "\n  ✓ 计划全部完成！")
	} else {
		fmt.Fprintln(r.out, "\n  ⚠ 计划未完全完成，请检查失败的任务")
	}

	r.currentPlan = nil
}

func findPlanner(reg *tool.Registry) *tool.PlanTool {
	if t, ok := reg.Get("plan"); ok {
		if p, ok := t.(*tool.PlanTool); ok {
			return p
		}
	}
	return nil
}
