package repl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"codecrew/internal/config"
	"codecrew/internal/disp"
	"codecrew/internal/eval"
	"codecrew/internal/i18n"
	"codecrew/internal/knowledge"
	"codecrew/internal/llm"
	"codecrew/internal/mcp"
	"codecrew/internal/memory"
	"codecrew/internal/orchestration"
	"codecrew/internal/planner"
	"codecrew/internal/reasoning"
	"codecrew/internal/role"
	"codecrew/internal/session"
	"codecrew/internal/tool"
	"codecrew/internal/verify"
)

// Options 是启动 REPL 所需的一切。
type Options struct {
	BaseDir    string // 随二进制分发的 roles/ 覆盖目录，可为空
	Stdin      io.Reader
	Stdout     io.Writer
	AutoYes    bool   // --yes：跳过交互确认（仅 allow/deny 生效）
	SessionID  string // --session：续聊指定会话
	First      string // --print：单轮模式下要发送的首条指令
	Print      bool   // 单轮模式
	ConfigPath string // --config：显式配置文件
}

// REPL 是终端交互层，持有会话状态并驱动 Agent 循环。
type REPL struct {
	cfg      *config.Config
	roles    []role.Role
	current  role.Role
	registry *tool.Registry
	client   *llm.Client
	history  []llm.Message

	opt     Options
	scanner *bufio.Scanner
	out     io.Writer
	exit    bool

	store   *session.Store
	session *session.Session
	memory  *memory.Store
	usage   usageTracker

	plan     *tool.PlanTool
	started  time.Time
	compacts int

	reasoningCfg reasoning.Config           // 推理配置
	reflexion    *reasoning.ReflexionEngine // 反思引擎
	failureStore *reasoning.FailureStore    // 失败经验存储
	thoughts     []string                   // 当前轮的思考过程

	verifyEngine *verify.Engine // 验证引擎
	verifyCfg    verify.Config  // 验证配置

	plannerEnabled bool                // 计划模式是否开启
	currentPlan    *planner.Plan       // 当前执行计划
	decomposer     *planner.Decomposer // 任务分解器

	codebaseIndex *knowledge.CodebaseIndex // 代码库索引
	searcher      *knowledge.Searcher      // 语义搜索器
	episodicStore *knowledge.EpisodicStore // 情景记忆存储

	supervisorState *orchestration.SupervisorState // Supervisor 模式状态
	hitlState       *orchestration.HITLState       // Human-in-the-Loop 状态
	evalHarness     *eval.Harness                  // 评估框架

	cmdHistory []string      // 命令历史
	mcpClients []*mcp.Client // MCP 服务器客户端，关闭时清理
	lang       i18n.Language // 界面语言
}

type usageTracker struct {
	prompt     int
	completion int
	turns      int
}

// New 组装一个 REPL。
func New(cfg *config.Config, opt Options) (*REPL, error) {
	if opt.Stdout == nil {
		opt.Stdout = os.Stdout
	}
	if opt.Stdin == nil {
		opt.Stdin = os.Stdin
	}
	roles, err := role.Load(roleDirsFor(opt.BaseDir, cfg.WorkDir())...)
	if err != nil {
		return nil, err
	}
	current := roles[0]
	for _, r := range roles {
		if r.Name == "developer" {
			current = r
			break
		}
	}

	r := &REPL{
		cmdHistory: []string{},
		cfg:        cfg,
		roles:      roles,
		current:    current,
		opt:        opt,
		out:        opt.Stdout,
		scanner:    newScanner(opt.Stdin),
		started:    time.Now(),
	}
	r.registry = tool.NewDefaultRegistry(cfg.WorkDir())
	r.plan = findPlanner(r.registry)
	r.applyRole(current)
	r.client = r.buildClient()
	r.lang = i18n.Parse(cfg.Language)
	r.connectMCPServers()
	// 初始化推理配置和失败存储（必须在创建 history 之前，这样 systemPromptFor 才能读取推理配置）
	r.initReasoning()
	// 初始化验证引擎
	r.initVerify()
	// 初始化规划器
	r.initPlanner()
	// 初始化知识系统（代码库索引、语义搜索、情景记忆）
	r.initKnowledge()
	// 初始化编排和评估（Supervisor、HITL、Eval）
	r.initOrchestration()
	r.history = []llm.Message{llm.TextMessage("system", r.systemPromptFor(current))}

	if store, err := session.DefaultStore(); err == nil {
		r.store = store
		r.openSession(opt.SessionID)
	}
	if mem, err := memory.DefaultStore(); err == nil {
		r.memory = mem
	}
	r.registry.SetApprover(r.approve)
	return r, nil
}

// llmAdapter 将 llm.Client 适配为 reasoning.LLMClient 接口。
// llmAdapter 通用 LLM 适配器，所有模块共享（ChatMessage 已统一为 llm.ChatMessage 类型别名）。
type llmAdapter struct {
	client *llm.Client
}

func (a *llmAdapter) Complete(ctx context.Context, messages []reasoning.ChatMessage) (string, error) {
	llmMessages := make([]llm.Message, len(messages))
	for i, m := range messages {
		llmMessages[i] = llm.TextMessage(m.Role, m.Content)
	}
	return a.client.Complete(ctx, llmMessages)
}

// executeOneTurn 执行一轮对话（简化版，用于计划模式中的任务执行）。
func (r *REPL) executeOneTurn(ctx context.Context) (string, []llm.ToolCall, *llm.Usage, error) {
	r.compactIfNeeded()
	fmt.Fprintln(r.out)

	text, calls, usage, err := r.client.Chat(ctx, r.history, r.registry.Schemas(), func(delta string) {
		fmt.Fprint(r.out, delta)
	})
	if err != nil {
		fmt.Fprintf(r.out, "\n  ✗ %v\n", err)
		if text != "" {
			msg := llm.TextMessage("assistant", text)
			r.history = append(r.history, msg)
			r.appendSession(msg)
		}
		return text, calls, usage, err
	}
	if usage != nil {
		r.usage.prompt += usage.PromptTokens
		r.usage.completion += usage.CompletionTokens
	} else {
		r.usage.prompt += estimateTokens(r.historyText())
	}
	r.usage.completion += estimateTokens(text)
	r.usage.turns++
	if text != "" {
		fmt.Fprintln(r.out)
	}
	r.history = append(r.history, llm.Message{Role: "assistant", Content: text, ToolCalls: calls})
	r.appendSession(llm.Message{Role: "assistant", Content: text, ToolCalls: calls})

	// 执行工具调用
	if len(calls) > 0 {
		if err := r.runToolCalls(ctx, calls); err != nil {
			return text, calls, usage, err
		}
	}

	return text, calls, usage, nil
}

// Run 进入 REPL。opt.First 非空时执行单轮后返回。
func (r *REPL) Run() error {
	r.printWelcome()
	if r.opt.First != "" {
		r.handleInput(r.opt.First)
		r.saveSession()
		return nil
	}
	for !r.exit {
		fmt.Fprintf(r.out, "\n%s → %s\n> ", r.current.Name, r.modelLabel())
		if !r.scanner.Scan() {
			fmt.Fprintln(r.out)
			break
		}
		input := strings.TrimSpace(r.scanner.Text())
		if input == "" {
			continue
		}
		r.handleInput(input)
	}
	r.saveSession()
	return nil
}

// chineseCommands 把中文别名映射到规范命令名。
var chineseCommands = map[string]string{
	"角色": "roles", "模型": "model", "配置": "config", "重载": "reload",
	"帮助": "help", "退出": "exit", "清空": "clear", "压缩": "compact",
	"上下文": "context", "工具": "tools", "权限": "permissions", "成本": "cost",
	"会话": "sessions", "恢复": "resume", "新建": "new", "保存": "save", "撤销": "undo",
	"流水线": "pipeline", "圆桌": "roundtable", "记忆": "memory",
	"推理": "reasoning", "失败": "failures", "验证": "verify",
	"索引": "index", "搜索": "index", "编排": "supervisor",
	"批准": "approve", "拒绝": "deny", "评估": "eval",
}

func (r *REPL) handleInput(input string) {
	r.addToHistory(input)
	// !n 重复执行历史命令
	if strings.HasPrefix(input, "!") {
		idxStr := strings.TrimPrefix(input, "!")
		if idxStr == "!" {
			// !! 重复上一条
			if len(r.cmdHistory) > 0 {
				input = r.cmdHistory[len(r.cmdHistory)-1]
				fmt.Fprintf(r.out, "\n%% 重复执行: %s\n", input)
			}
		} else if n, err := strconv.Atoi(idxStr); err == nil && n >= 1 && n <= len(r.cmdHistory) {
			input = r.cmdHistory[n-1]
			fmt.Fprintf(r.out, "\n%% 重复执行: %s\n", input)
		}
	}
	name, arg := splitCommand(input)
	switch name {
	case "":
		r.runTurn(input)
	case "exit", "quit":
		r.exit = true
		fmt.Fprintln(r.out, "\n再见！会话已保存，可用 /resume 续聊。")
	case "help":
		r.printHelp()
	case "roles":
		r.printRoles()
	case "role":
		r.switchRole(arg)
	case "model":
		r.switchModel(arg)
	case "config":
		r.printConfig()
	case "reload":
		r.reload()
	case "tools":
		r.printTools()
	case "permissions":
		r.printPermissions()
	case "allow":
		r.allowTool(arg)
	case "plan":
		r.handlePlan(arg)
	case "index":
		r.handleIndex(arg)
	case "supervisor":
		r.handleSupervisor(arg)
	case "approve":
		r.handleApprove(arg)
	case "deny":
		r.handleDeny(arg)
	case "eval":
		r.handleEval(arg)
	case "pipeline":
		if err := r.RunPipeline(arg); err != nil {
			fmt.Fprintf(r.out, "  ✗ 流水线失败: %v\n", err)
		}
	case "roundtable":
		topic, rounds := parseRoundtableArgs(arg)
		if err := r.RunRoundtable(topic, rounds); err != nil {
			fmt.Fprintf(r.out, "  ✗ 圆桌讨论失败: %v\n", err)
		}
	case "memory":
		r.handleMemory(arg)
	case "context", "ctx":
		r.printContext()
	case "compact":
		r.compact(true)
	case "clear":
		r.clearHistory()
	case "undo":
		r.undo()
	case "cost":
		r.printCost()
	case "reasoning":
		r.handleReasoning(arg)
	case "failures":
		r.handleFailures(arg)
	case "verify":
		r.handleVerify(arg)
	case "language", "lang":
		r.handleLanguage(arg)
	case "sessions":
		r.listSessions()
	case "resume":
		r.resumeSession(arg)
	case "new":
		r.newSession()
	case "save":
		r.saveSession()
		fmt.Fprintf(r.out, "  ✓ 会话已保存: %s\n", r.sessionPath())
	default:
		fmt.Fprintf(r.out, "  ✗ 未知命令 /%s，输入 /help 查看可用命令\n", name)
	}
}

// splitCommand 解析 "/cmd arg"、"cmd arg"（中文别名）与普通对话。
// 返回的 name 为空表示这是一句话，应交给模型。
// newScanner 统一 stdin 扫描器配置（支持超长粘贴内容）。
func newScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	return sc
}

func splitCommand(input string) (string, string) {
	text := strings.TrimSpace(input)
	body := strings.TrimPrefix(text, "/")
	if body == text {
		// 没有斜杠：只有命中中文别名表时才当命令用
		if strings.Contains(body, " ") {
			first, rest, _ := strings.Cut(body, " ")
			if mapped, ok := chineseCommands[first]; ok {
				return mapped, strings.TrimSpace(rest)
			}
			return "", ""
		}
		if mapped, ok := chineseCommands[body]; ok {
			return mapped, ""
		}
		return "", ""
	}
	name, arg, _ := strings.Cut(body, " ")
	name = strings.ToLower(strings.TrimSpace(name))
	if mapped, ok := chineseCommands[name]; ok {
		name = mapped
	}
	return name, strings.TrimSpace(arg)
}

func (r *REPL) runTurn(userInput string) {
	if r.client == nil {
		fmt.Fprintln(r.out, "\n  ⚠ 还没有可用的模型，先配置再开始：")
		for _, line := range strings.Split(configHint(r.cfg), "\n") {
			fmt.Fprintf(r.out, "    %s\n", line)
		}
		return
	}
	// 计划模式：自动触发规划（排除简单命令和短输入）
	if r.plannerEnabled && r.currentPlan == nil && len(userInput) > 20 {
		r.runPlanMode(context.Background(), userInput)
		return
	}
	r.compactIfNeeded()
	r.history = append(r.history, llm.TextMessage("user", userInput))
	r.appendSession(llm.TextMessage("user", userInput))
	fmt.Fprintln(r.out)

	ctx := context.Background()
	maxRounds := r.cfg.MaxToolRounds
	if maxRounds <= 0 {
		maxRounds = 12
	}

	// ReAct 模式：维护 Thought 显示状态
	inThought := false
	thoughtBuffer := ""
	r.thoughts = nil

	for round := 0; round < maxRounds; round++ {
		if round > 0 {
			fmt.Fprintln(r.out)
		}
		text, calls, usage, err := r.client.Chat(ctx, r.history, r.registry.Schemas(), func(delta string) {
			// ReAct 模式：检测 Thought 前缀，用不同样式显示
			if r.reasoningCfg.Mode == reasoning.ModeReAct || r.reasoningCfg.Mode == reasoning.ModeReflexion {
				thoughtBuffer += delta
				if !inThought {
					trimmed := strings.TrimSpace(thoughtBuffer)
					if strings.HasPrefix(trimmed, "Thought:") {
						inThought = true
						fmt.Fprint(r.out, "\n  "+dim("💭 "))
						rest := strings.TrimPrefix(trimmed, "Thought:")
						if rest != "" {
							fmt.Fprint(r.out, dim(rest))
						}
						thoughtBuffer = ""
						return
					}
					if len(thoughtBuffer) > 200 {
						fmt.Fprint(r.out, thoughtBuffer)
						thoughtBuffer = ""
					}
				} else {
					if strings.Contains(delta, "\n") {
						if strings.Contains(thoughtBuffer, "Action:") || strings.Contains(thoughtBuffer, "Final Answer:") {
							inThought = false
							fmt.Fprintln(r.out)
							thoughtBuffer = ""
							return
						}
						if strings.HasSuffix(thoughtBuffer, "\n\n") {
							inThought = false
							fmt.Fprintln(r.out)
							thoughtBuffer = ""
							return
						}
					}
					thoughtBuffer += delta
					fmt.Fprint(r.out, dim(delta))
					return
				}
			}
			fmt.Fprint(r.out, delta)
		})
		if err != nil {
			fmt.Fprintf(r.out, "\n  ✗ %v\n", err)
			if text != "" {
				msg := llm.TextMessage("assistant", text)
				r.history = append(r.history, msg)
				r.appendSession(msg)
			}
			// 失败时触发深度反思
			r.triggerReflexion(ctx, userInput, text, true)
			// 记录情景记忆（失败）
			r.recordEpisodicMemory(userInput, err.Error(), false, nil)
			return
		}
		if usage != nil {
			r.usage.prompt += usage.PromptTokens
			r.usage.completion += usage.CompletionTokens
		} else {
			r.usage.prompt += estimateTokens(r.historyText())
		}
		r.usage.completion += estimateTokens(text)
		r.usage.turns++
		if text != "" {
			fmt.Fprintln(r.out)
		}
		r.history = append(r.history, llm.Message{Role: "assistant", Content: text, ToolCalls: calls})
		r.appendSession(llm.Message{Role: "assistant", Content: text, ToolCalls: calls})

		if len(calls) == 0 {
			// 任务完成，触发反思
			r.triggerReflexion(ctx, userInput, text, false)
			// 记录情景记忆
			r.recordEpisodicMemory(userInput, text, true, nil)
			return
		}
		if err := r.runToolCalls(ctx, calls); err != nil {
			return
		}
	}
	fmt.Fprintf(r.out, "\n  ⚠ 已达到单轮最多 %d 次工具调用的上限（可用 max_tool_rounds 调整）\n", maxRounds)
	r.history = append(r.history, llm.TextMessage("system", fmt.Sprintf("（本轮工具调用达到 %d 次上限，已暂停，请人工确认后续步骤）", maxRounds)))
	// 达到上限视为失败，触发深度反思
	r.triggerReflexion(ctx, userInput, "", true)
}

func (r *REPL) runToolCalls(ctx context.Context, calls []llm.ToolCall) error {
	for _, call := range calls {
		var args map[string]any
		if call.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
				r.feedBack(call, fmt.Sprintf("参数不是合法 JSON: %v", err), true)
				continue
			}
		}
		if args == nil {
			args = map[string]any{}
		}
		t, ok := r.registry.Get(call.Function.Name)
		if !ok {
			r.feedBack(call, fmt.Sprintf("未知工具 %s", call.Function.Name), true)
			continue
		}
		fmt.Fprintf(r.out, "  🔧 %s %s\n", call.Function.Name, dim(tool.Summary(t, args)))
		result, err := r.registry.Execute(ctx, call.Function.Name, args)
		if err != nil {
			fmt.Fprintf(r.out, "     %s\n", dim("→ "+err.Error()))
			r.feedBack(call, "错误: "+err.Error(), true)
			continue
		}
		preview := firstLine(result)
		if preview != "" {
			fmt.Fprintf(r.out, "     %s\n", dim("→ "+disp.Truncate(preview, 100)))
		}
		r.feedBack(call, result, false)
		// 工具执行后自动验证（仅当修改了文件时）
		r.autoVerifyAfterTool(ctx, call.Function.Name)
	}
	return nil
}

func (r *REPL) feedBack(call llm.ToolCall, content string, isErr bool) {
	if strings.TrimSpace(content) == "" {
		content = "（无输出）"
	}
	msg := llm.Message{Role: "tool", Content: content, ToolCallID: call.ID, Name: call.Function.Name}
	r.history = append(r.history, msg)
	r.appendSession(msg)
	_ = isErr
}

func (r *REPL) appendSession(m llm.Message) {
	if r.session != nil {
		if err := r.session.Append(m); err != nil {
			fmt.Fprintf(r.out, "  ⚠ 会话写入失败: %v\n", err)
		}
	}
}

func (r *REPL) printWelcome() {
	fmt.Fprintln(r.out)
	title := "CodeCrew — 你的 AI 开发团队"
	inner := 46
	pad := inner - disp.Width(title)
	left := pad / 2
	right := pad - left
	fmt.Println("  ╔" + strings.Repeat("═", inner) + "╗")
	fmt.Printf("  ║%s%s%s║\n", strings.Repeat(" ", left), title, strings.Repeat(" ", right))
	fmt.Println("  ╚" + strings.Repeat("═", inner) + "╝")
	fmt.Fprintln(r.out)

	if r.cfg.Empty() || r.cfg.Model == "" || r.client == nil {
		fmt.Fprintln(r.out, "  首次使用？三步搞定：")
		fmt.Fprintln(r.out)
		for _, line := range strings.Split(configHint(r.cfg), "\n") {
			fmt.Fprintf(r.out, "  %s\n", line)
		}
	} else {
		fmt.Fprintf(r.out, "  当前模型: %s   工作目录: %s\n", bright(r.cfg.Model), r.cfg.WorkDir())
		if !r.cfg.Empty() {
			if p, _, err := r.cfg.Resolve(r.cfg.Model); err == nil {
				fmt.Fprintf(r.out, "  供应商: %s   密钥: %s\n", firstProvider(r.cfg, p.BaseURL), config.MaskKey(p.APIKey))
			}
		}
		fmt.Fprintln(r.out)
	}

	fmt.Fprintln(r.out, "  可用角色:")
	for _, rr := range r.roles {
		marker := "  "
		if rr.Name == r.current.Name {
			marker = "→ "
		}
		fmt.Fprintf(r.out, "    %s%s %s\n", marker, disp.Pad(rr.Name, 12), dim(rr.Description))
	}
	fmt.Fprintf(r.out, "\n  当前角色工具: %s\n", strings.Join(r.registry.AllowedNames(), ", "))
	fmt.Fprintln(r.out, "  "+dim("输入 /help 看命令，/config 看配置，/exit 退出"))
	fmt.Fprintln(r.out)
}

func (r *REPL) printHelp() {
	fmt.Fprintln(r.out, "\n  命令列表")
	fmt.Fprintln(r.out, "  "+strings.Repeat("─", 62))
	rows := [][2]string{
		{"/roles  ·  /role <name>", "查看 / 切换角色（含中文别名 角色）"},
		{"/model  ·  /model <spec>", "查看 / 切换模型，spec 形如 deepseek/deepseek-chat"},
		{"/config", "查看供应商、密钥脱敏与配置文件路径"},
		{"/reload", "重新读取 codecrew.json 并重建模型连接"},
		{"/tools · /permissions · /allow <tool>", "查看工具授权情况、临时放行某工具"},
		{"/plan", "查看模型拆解出来的任务计划"},
		{"/pipeline <任务>", "流水线：架构师拆解→开发→审查→测试（中文 流水线）"},
		{"/roundtable <话题> [轮数]", "圆桌讨论：多角色辩论后输出共识与分歧（中文 圆桌）"},
		{"/memory [add <内容>|clear]", "角色长期记忆，自动注入 system prompt（中文 记忆）"},
		{"/context · /compact", "查看上下文占用 / 立即压缩历史"},
		{"/clear · /undo", "清空历史 / 回退上一轮"},
		{"/sessions · /resume <id> · /new", "历史会话列表 / 续聊 / 新开会话"},
		{"/cost", "本次会话 token 与耗时统计"},
		{"/reasoning [mode] · /failures", "推理模式（standard/react/reflexion/cot）/ 失败经验（中文 推理/失败）"},
		{"/verify [repair]", "代码验证与自愈，可选 repair 自动修复（中文 验证）"},
		{"/index [build|search <q>]", "代码库索引管理与代码搜索（中文 索引）"},
		{"/supervisor [on|off|assign] · /approve · /deny", "多角色编排与人工审批（中文 编排/批准/拒绝）"},
		{"/eval [run|list]", "能力评估框架，运行测试用例并生成报告（中文 评估）"},
		{"/save · /exit", "手动落盘 / 退出"},
	}
	for _, row := range rows {
		fmt.Fprintf(r.out, "  %s  %s\n", disp.Pad(row[0], 40), dim(row[1]))
	}
	fmt.Fprintln(r.out, "\n  直接输入文字即可与当前角色对话；写文件与执行命令会先征求确认")
	fmt.Fprintln(r.out, "  命令行参数: --role <name> --model <spec> --cwd <dir> --yes --session <id> --print <text>")
	fmt.Fprintln(r.out)
}

func configHint(cfg *config.Config) string {
	return strings.Join([]string{
		"1. 复制模板: cp codecrew.example.json codecrew.json",
		"2. 编辑 codecrew.json，填入 base_url / api_key",
		"3. 回到这里执行 /reload（或设置 CREW_BASE_URL / CREW_API_KEY / CREW_MODEL）",
	}, "\n")
}

// connectMCPServers 连接配置中的 MCP 服务器并注册其工具。
func (r *REPL) connectMCPServers() {
	if len(r.cfg.MCPServers) == 0 {
		return
	}
	for name, srv := range r.cfg.MCPServers {
		if srv.Disabled || srv.Command == "" {
			continue
		}
		client, err := mcp.NewClient(srv.Command, srv.Args...)
		if err != nil {
			fmt.Fprintf(r.out, "  ⚠ MCP 服务器 %q 连接失败: %v\n", name, err)
			continue
		}
		tools, err := client.ListTools()
		if err != nil {
			fmt.Fprintf(r.out, "  ⚠ MCP 服务器 %q 获取工具列表失败: %v\n", name, err)
			client.Close()
			continue
		}
		for _, t := range tools {
			adapter := mcp.NewToolAdapter(client, t)
			r.registry.Register(adapter, tool.DecisionAsk)
		}
		r.mcpClients = append(r.mcpClients, client)
		fmt.Fprintf(r.out, "  ✓ MCP 服务器 %q (%s v%s)：注册 %d 个工具\n",
			name, client.ServerName(), client.ServerVersion(), len(tools))
	}
}

// CloseMCP 关闭所有 MCP 服务器连接。
func (r *REPL) CloseMCP() {
	for _, c := range r.mcpClients {
		c.Close()
	}
	r.mcpClients = nil
}

// T 返回当前语言的翻译。args 可选，用于格式化。
func (r *REPL) T(key string, args ...any) string {
	return i18n.T(r.lang, key, args...)
}

// Language 返回当前界面语言。
func (r *REPL) Language() i18n.Language { return r.lang }

// SetLanguage 设置界面语言。
func (r *REPL) SetLanguage(lang i18n.Language) { r.lang = lang }

// handleLanguage 处理 /language 命令，查看或切换语言。
func (r *REPL) handleLanguage(arg string) {
	if arg == "" {
		fmt.Fprintf(r.out, "\n  当前语言: %s\n", r.lang)
		fmt.Fprintln(r.out, "  支持: zh-CN (中文), en-US (English)")
		fmt.Fprintln(r.out, "  用法: /language zh-CN 或 /language en-US")
		return
	}
	lang := i18n.Parse(arg)
	r.lang = lang
	if lang == i18n.EnUS {
		fmt.Fprintf(r.out, "\n  Language switched to: %s\n", lang)
	} else {
		fmt.Fprintf(r.out, "\n  语言已切换为: %s\n", lang)
	}
}
