package repl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"codecrew/internal/disp"
	"codecrew/internal/llm"
	"codecrew/internal/memory"
	"codecrew/internal/role"
	"codecrew/internal/session"
)

// pipelineStage 是流水线中的一个阶段。
type pipelineStage struct {
	roleName string
	intro    string // 阶段开始时打印的标题
	prompt   string // 传给该角色的指令模板，%s 会被替换为上游输出
}

// defaultPipeline 是默认流水线：架构师拆解 → 开发实现 → 审查 → 测试。
var defaultPipeline = []pipelineStage{
	{
		roleName: "architect",
		intro:    "阶段 1/4 · 架构师拆解任务",
		prompt:   "请完成以下任务的设计与拆解。先读懂现状，再输出：现状摘要 → 目标与约束 → 方案 → 影响面 → 任务拆解（用 plan 工具落成条目）。\n\n任务：%s",
	},
	{
		roleName: "developer",
		intro:    "阶段 2/4 · 开发工程师实现",
		prompt:   "根据以下架构设计与任务拆解，开始编码实现。按 plan 中的条目逐项完成，修改前先读相关文件，完成后跑必要的构建或检查。\n\n上游输出：\n%s",
	},
	{
		roleName: "reviewer",
		intro:    "阶段 3/4 · 代码审查",
		prompt:   "对刚才的实现做严格代码审查。用 read/glob/grep 查看改动，重点检查：正确性、边界条件、错误处理、代码风格、安全隐患、是否符合项目约定。输出问题清单（按严重程度排序）与修改建议。如果没有问题，明确说「通过审查」。\n\n上游输出：\n%s",
	},
	{
		roleName: "tester",
		intro:    "阶段 4/4 · 测试验证",
		prompt:   "根据审查结果与实现，运行测试验证。先跑现有测试套件，如有失败则修复；如缺少测试则补充关键路径的测试。最后输出测试结果总结。\n\n上游输出：\n%s",
	},
}

// RunPipeline 执行默认流水线。task 是用户的原始需求描述。
// 每个阶段独立运行 Agent 循环，阶段间传递上一阶段的最终输出。
// 流水线结束后，把完整摘要追加到主对话历史。
func (r *REPL) RunPipeline(task string) error {
	if r.client == nil {
		return fmt.Errorf("还没有可用的模型，先配置再运行流水线")
	}
	task = strings.TrimSpace(task)
	if task == "" {
		return fmt.Errorf("流水线任务不能为空")
	}

	// 检查所有需要的角色是否存在
	for _, stage := range defaultPipeline {
		if _, ok := role.Get(r.roles, stage.roleName); !ok {
			return fmt.Errorf("流水线需要角色 %q，但当前未加载；可用角色见 /roles", stage.roleName)
		}
	}

	fmt.Fprintf(r.out, "\n%s 启动流水线：%s\n", bright("⚙"), disp.Truncate(task, 80))
	fmt.Fprintln(r.out, "  阶段：架构师拆解 → 开发实现 → 代码审查 → 测试验证")
	fmt.Fprintln(r.out, "  "+dim("每个阶段独立运行 Agent 循环，结果自动传递给下一阶段"))

	upstream := task
	var stageOutputs []string
	pipelineStart := time.Now()

	for i, stage := range defaultPipeline {
		fmt.Fprintf(r.out, "\n%s %s\n", bright("──"), stage.intro)
		fmt.Fprintln(r.out, "  "+dim(strings.Repeat("─", 50)))

		userInput := fmt.Sprintf(stage.prompt, upstream)
		output, err := r.runRoleTurn(stage.roleName, userInput)
		if err != nil {
			fmt.Fprintf(r.out, "\n  %s 阶段 %s 失败：%v\n", bright("✗"), stage.roleName, err)
			// 把已完成的阶段摘要写入主对话
			r.appendPipelineResult(task, stageOutputs, pipelineStart, fmt.Sprintf("在 %s 阶段失败: %v", stage.roleName, err))
			return err
		}
		output = strings.TrimSpace(output)
		if output == "" {
			output = "（该阶段未产生文本输出）"
		}
		stageOutputs = append(stageOutputs, fmt.Sprintf("【%s】\n%s", stage.intro, output))
		upstream = output

		// 阶段摘要
		summary := disp.Truncate(strings.Join(strings.Fields(output), " "), 120)
		fmt.Fprintf(r.out, "\n  %s 阶段完成：%s\n", bright("✓"), dim(summary))
		_ = i
	}

	r.appendPipelineResult(task, stageOutputs, pipelineStart, "")
	return nil
}

// appendPipelineResult 把流水线完整结果追加到主对话历史，供用户后续追问。
func (r *REPL) appendPipelineResult(task string, outputs []string, start time.Time, errMsg string) {
	elapsed := time.Since(start).Round(time.Second)
	var sb strings.Builder
	fmt.Fprintf(&sb, "流水线执行结果（耗时 %s）\n\n", elapsed)
	fmt.Fprintf(&sb, "原始任务：%s\n\n", task)
	for _, out := range outputs {
		sb.WriteString(out)
		sb.WriteString("\n\n")
	}
	if errMsg != "" {
		fmt.Fprintf(&sb, "状态：%s\n", errMsg)
	} else {
		sb.WriteString("状态：全部阶段完成\n")
	}
	result := sb.String()
	r.history = append(r.history, llm.TextMessage("user", "流水线任务："+task))
	r.history = append(r.history, llm.TextMessage("assistant", result))
	r.appendSession(llm.TextMessage("user", "流水线任务："+task))
	r.appendSession(llm.TextMessage("assistant", result))
}

// runRoleTurn 用指定角色运行一轮完整的 Agent 循环，不影响主对话历史与当前角色。
// 返回该轮最终的 assistant 文本。
func (r *REPL) runRoleTurn(roleName, userInput string) (string, error) {
	target, ok := role.Get(r.roles, roleName)
	if !ok {
		return "", fmt.Errorf("角色 %s 不存在", roleName)
	}

	// 保存主对话状态
	savedRole := r.current
	savedHistory := r.history
	savedSession := r.session

	// 初始化该角色的独立历史
	systemPrompt := target.Prompt
	if r.memory != nil {
		if mem, err := r.memory.Load(roleName); err == nil {
			systemPrompt = memory.InjectPrompt(systemPrompt, mem)
		}
	}
	stageHistory := []llm.Message{llm.TextMessage("system", systemPrompt)}
	stageHistory = append(stageHistory, llm.TextMessage("user", userInput))

	// 临时切换到目标角色
	r.current = target
	r.history = stageHistory
	r.session = nil // 流水线阶段不写入主会话
	r.applyRole(target)

	maxRounds := r.cfg.MaxToolRounds
	if maxRounds <= 0 {
		maxRounds = 12
	}

	ctx := context.Background()
	var finalText string
	for round := 0; round < maxRounds; round++ {
		r.compactIfNeeded()
		text, calls, usage, err := r.client.Chat(ctx, r.history, r.registry.Schemas(), func(delta string) {
			fmt.Fprint(r.out, delta)
		})
		if err != nil {
			r.restoreState(savedRole, savedHistory, savedSession)
			return "", fmt.Errorf("模型调用失败: %w", err)
		}
		if usage != nil {
			r.usage.prompt += usage.PromptTokens
			r.usage.completion += usage.CompletionTokens
		}
		r.usage.turns++

		if text != "" {
			fmt.Fprintln(r.out)
		}
		r.history = append(r.history, llm.Message{Role: "assistant", Content: text, ToolCalls: calls})
		finalText = text

		if len(calls) == 0 {
			break
		}
		if err := r.runToolCalls(ctx, calls); err != nil {
			r.restoreState(savedRole, savedHistory, savedSession)
			return "", err
		}
	}

	// 恢复主对话状态
	r.restoreState(savedRole, savedHistory, savedSession)
	return finalText, nil
}

// restoreState 恢复 runRoleTurn 保存的主对话状态。
func (r *REPL) restoreState(savedRole role.Role, savedHistory []llm.Message, savedSession *session.Session) {
	r.current = savedRole
	r.history = savedHistory
	r.session = savedSession
	r.applyRole(savedRole)
}
