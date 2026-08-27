package repl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"codecrew/internal/config"
	"codecrew/internal/disp"
	"codecrew/internal/llm"
	"codecrew/internal/role"
	"codecrew/internal/session"
	"codecrew/internal/tool"
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
	usage   usageTracker

	plan     *tool.PlanTool
	started  time.Time
	compacts int
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
		cfg:     cfg,
		roles:   roles,
		current: current,
		opt:     opt,
		out:     opt.Stdout,
		scanner: newScanner(opt.Stdin),
		started: time.Now(),
	}
	r.registry = tool.NewDefaultRegistry(cfg.WorkDir())
	r.plan = findPlanner(r.registry)
	r.applyRole(current)
	r.client = r.buildClient()
	r.history = []llm.Message{llm.TextMessage("system", current.Prompt)}

	if store, err := session.DefaultStore(); err == nil {
		r.store = store
		r.openSession(opt.SessionID)
	}
	r.registry.SetApprover(r.approve)
	return r, nil
}

func findPlanner(reg *tool.Registry) *tool.PlanTool {
	if t, ok := reg.Get("plan"); ok {
		if p, ok := t.(*tool.PlanTool); ok {
			return p
		}
	}
	return nil
}

// ---------------------------------------------------------------- 主循环

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
}

func (r *REPL) handleInput(input string) {
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
		if arg == "" {
			r.printPlan()
		} else {
			r.addPlanTask(arg)
		}
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

// ------------------------------------------------------------ Agent 循环

func (r *REPL) runTurn(userInput string) {
	if r.client == nil {
		fmt.Fprintln(r.out, "\n  ⚠ 还没有可用的模型，先配置再开始：")
		for _, line := range strings.Split(configHint(r.cfg), "\n") {
			fmt.Fprintf(r.out, "    %s\n", line)
		}
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

	for round := 0; round < maxRounds; round++ {
		if round > 0 {
			fmt.Fprintln(r.out) // 工具执行完，与模型的新一段回复之间留一行
		}
		text, calls, usage, err := r.client.Chat(ctx, r.history, r.registry.Schemas(), func(delta string) {
			fmt.Fprint(r.out, delta)
		})
		if err != nil {
			fmt.Fprintf(r.out, "\n  ✗ %v\n", err)
			if text != "" {
				r.history = append(r.history, llm.TextMessage("assistant", text))
			}
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
			return
		}
		if err := r.runToolCalls(ctx, calls); err != nil {
			return
		}
	}
	fmt.Fprintf(r.out, "\n  ⚠ 已达到单轮最多 %d 次工具调用的上限（可用 max_tool_rounds 调整）\n", maxRounds)
	r.history = append(r.history, llm.TextMessage("system", fmt.Sprintf("（本轮工具调用达到 %d 次上限，已暂停，请人工确认后续步骤）", maxRounds)))
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
		r.session.Append(m)
	}
}

func (r *REPL) historyText() string {
	var sb strings.Builder
	for _, m := range r.history {
		sb.WriteString(m.Content)
	}
	return sb.String()
}

// ------------------------------------------------------------- 上下文管理

func estimateTokens(text string) int {
	cjk := disp.Width(text) - disp.RuneCount(text)
	return disp.RuneCount(text) + cjk/2 + 1
}

func (r *REPL) contextTokens() int {
	total := 0
	for _, m := range r.history {
		total += estimateTokens(m.Content) + estimateTokens(tool.SummaryRaw(m.ToolCalls))
	}
	return total
}

func (r *REPL) compactIfNeeded() {
	limit := r.cfg.MaxContextTokens
	if limit <= 0 {
		return
	}
	used := r.contextTokens()
	if used < limit {
		return
	}
	if err := r.compact(false); err != nil {
		fmt.Fprintf(r.out, "  ⚠ 上下文压缩失败：%v\n", err)
	}
}

// compact 保留 system 提示与最近若干条消息，把更早的历史压成一条摘要。
func (r *REPL) compact(force bool) error {
	if len(r.history) < 5 {
		if force {
			fmt.Fprintln(r.out, "  上下文还很短，无需压缩")
		}
		return nil
	}
	keep := 6
	// 从尾部往前数，至少保留 keep 条，且不能切断 assistant→tool 的配对
	head := r.history[1:]
	cut := len(head) - keep
	if cut <= 1 {
		if force {
			fmt.Fprintln(r.out, "  上下文还很短，无需压缩")
		}
		return nil
	}
	for cut < len(head) {
		if !startsToolGroup(head[cut:]) {
			break
		}
		cut++
	}
	if cut >= len(head) {
		return nil
	}

	victim := head[:cut]
	rest := head[cut:]
	summary, err := r.summarize(victim)
	if err != nil {
		return err
	}
	r.history = append([]llm.Message{r.history[0]}, llm.TextMessage("system", "前文摘要：\n"+summary))
	r.history = append(r.history, rest...)
	r.compacts++
	fmt.Fprintf(r.out, "  %s 已压缩 %d 条历史（当前约 %d tokens，累计压缩 %d 次）\n",
		dim("✓"), len(victim), r.contextTokens(), r.compacts)
	return nil
}

func startsToolGroup(msgs []llm.Message) bool {
	return len(msgs) > 0 && msgs[0].Role == "tool"
}

func (r *REPL) summarize(msgs []llm.Message) (string, error) {
	var sb strings.Builder
	for _, m := range msgs {
		label := m.Role
		if m.Name != "" {
			label += "(" + m.Name + ")"
		}
		body := disp.Truncate(strings.ReplaceAll(m.Content, "\n", " "), 400)
		if len(m.ToolCalls) > 0 {
			body += " 调用: " + tool.SummaryRaw(m.ToolCalls)
		}
		fmt.Fprintf(&sb, "[%s] %s\n", label, body)
	}
	prompt := []llm.Message{
		llm.TextMessage("system", "你是对话压缩器。把下面的多轮 Agent 对话压成不超过 12 条要点，必须保留：用户目标、已确认的关键决定、改过哪些文件与结果、尚未完成的步骤。用中文，不要编造。"),
		llm.TextMessage("user", sb.String()),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if text, err := r.client.Complete(ctx, prompt); err == nil && strings.TrimSpace(text) != "" {
		return strings.TrimSpace(text), nil
	}
	// 兜底：模型不可用时做机械截断，绝不静默丢信息
	var fallback strings.Builder
	for _, m := range msgs {
		if m.Role == "user" {
			fmt.Fprintf(&fallback, "用户：%s\n", disp.Truncate(m.Content, 120))
		}
	}
	if fallback.Len() == 0 {
		return "（更早的对话内容已省略）", nil
	}
	return strings.TrimSpace(fallback.String()), nil
}

// --------------------------------------------------------------- 权限闸门

func (r *REPL) approve(t tool.Tool, args map[string]any) tool.Decision {
	if r.opt.AutoYes {
		return tool.DecisionAllow
	}
	if t.Name() == "bash" {
		if cmd, ok := args["command"].(string); ok && tool.IsDangerousCommand(cmd) {
			fmt.Fprintf(r.out, "\n  ⚠ 高危命令：%s\n", bright(cmd))
			if ok, _ := r.askConfirm("确认执行？[y/N]"); ok {
				return tool.DecisionAllow
			}
			return tool.DecisionDeny
		}
	}
	fmt.Fprintf(r.out, "  %s 请求执行 %s: %s\n", bright(r.current.Name), t.Name(), dim(tool.Summary(t, args)))
	ok, always := r.askConfirm("允许？[y/N/a=本角色内始终允许]")
	if !ok {
		return tool.DecisionDeny
	}
	if always {
		r.registry.Remember(t.Name())
		fmt.Fprintf(r.out, "  ✓ %s 在本次角色内不再询问\n", t.Name())
	}
	return tool.DecisionAllow
}

// askConfirm 打印提示并等待一行输入；EOF 视为拒绝并请求退出。
func (r *REPL) askConfirm(prompt string) (bool, bool) {
	fmt.Fprintf(r.out, "  %s\n", prompt)
	if !r.scanner.Scan() {
		r.exit = true
		fmt.Fprintln(r.out, "  (输入结束，按拒绝处理)")
		return false, false
	}
	answer := strings.ToLower(strings.TrimSpace(r.scanner.Text()))
	switch answer {
	case "a", "always", "allow all", "一直", "总是":
		return true, true
	case "y", "yes", "是", "允许", "好", "ok":
		return true, false
	default:
		return false, false
	}
}

// ------------------------------------------------------------------ 命令

func (r *REPL) switchRole(name string) {
	if name == "" {
		r.printRoles()
		return
	}
	target, ok := role.Get(r.roles, name)
	if !ok {
		// 容忍用户刚往 roles/ 里丢文件却没 /reload
		if refreshed, err := role.Load(r.roleDirs()...); err == nil {
			r.roles = refreshed
			target, ok = role.Get(refreshed, name)
		}
	}
	if !ok {
		names := make([]string, 0, len(r.roles))
		for _, rr := range r.roles {
			names = append(names, rr.Name)
		}
		fmt.Fprintf(r.out, "  ✗ 未找到角色 %q，可用: %s\n", name, strings.Join(names, ", "))
		return
	}
	r.current = target
	r.applyRole(target)
	if len(r.history) > 0 && r.history[0].Role == "system" {
		r.history[0] = llm.TextMessage("system", target.Prompt)
	} else {
		r.history = append([]llm.Message{llm.TextMessage("system", target.Prompt)}, r.history...)
	}
	unknown := target.Tools
	filtered := unknown[:0]
	for _, t := range unknown {
		if _, ok := r.registry.Get(t); ok {
			filtered = append(filtered, t)
		}
	}
	fmt.Fprintf(r.out, "  ✓ 已切换到 %s（%s）\n", bright(target.Name), target.Description)
	if len(filtered) < len(unknown) {
		fmt.Fprintf(r.out, "    %s\n", dim("提示：该角色声明了未注册的工具，已自动忽略"))
	}
}

func (r *REPL) applyRole(target role.Role) {
	r.registry.SetRoleAllowed(target.Tools)
	r.registry.SetPermissions(r.cfg.Permissions)
}

func (r *REPL) switchModel(spec string) {
	if spec == "" {
		r.printModels()
		return
	}
	provider, modelID, err := r.cfg.Resolve(spec)
	if err != nil {
		fmt.Fprintf(r.out, "  ✗ %v\n", err)
		return
	}
	r.cfg.Model = spec
	r.client = llm.New(provider.BaseURL, provider.APIKey, modelID)
	fmt.Fprintf(r.out, "  ✓ 已切换模型: %s → %s\n", bright(spec), provider.BaseURL)
}

func (r *REPL) reload() {
	cfg, err := config.Load(r.opt.BaseDir, r.opt.ConfigPath)
	if err != nil {
		fmt.Fprintf(r.out, "  ✗ 重载失败: %v\n", err)
		return
	}
	r.cfg = cfg
	r.registry = tool.NewDefaultRegistry(cfg.WorkDir())
	r.plan = findPlanner(r.registry)
	r.applyRole(r.current)
	r.client = r.buildClient()
	r.printReloaded()
}

func (r *REPL) printReloaded() {
	if r.client == nil {
		fmt.Fprintln(r.out, "  ✓ 配置已重载，但模型不可用（缺少 model 或供应商密钥）")
		return
	}
	fmt.Fprintf(r.out, "  ✓ 配置已重载：%s（工作目录 %s）\n", bright(r.cfg.Model), r.cfg.WorkDir())
}

func (r *REPL) printRoles() {
	roles, err := role.Load(r.roleDirs()...)
	if err != nil {
		fmt.Fprintf(r.out, "  ✗ 读取角色失败: %v\n", err)
		return
	}
	r.roles = roles
	fmt.Fprintln(r.out, "\n  可用角色:")
	for _, rr := range roles {
		marker := "  "
		if rr.Name == r.current.Name {
			marker = "→ "
		}
		fmt.Fprintf(r.out, "    %s%s %s\n", marker, disp.Pad(rr.Name, 12), dim(rr.Description))
		fmt.Fprintf(r.out, "      %s\n", dim("工具: "+strings.Join(rr.Tools, ", ")))
	}
	fmt.Fprintln(r.out, "\n  切换: /role developer  自定义: 在 roles/ 放 .md 后 /reload")
}

func (r *REPL) printModels() {
	if r.cfg.Empty() {
		fmt.Fprintln(r.out, "  还没有配置任何供应商，输入 /config 查看步骤")
		return
	}
	specs := r.cfg.ModelSpecs()
	fmt.Fprintln(r.out, "\n  可用模型:")
	if len(specs) == 0 {
		for _, name := range r.cfg.ProviderNames() {
			fmt.Fprintf(r.out, "    %s %s\n", dim("-"), name+"/<模型名>")
		}
		fmt.Fprintln(r.out, "  （在 codecrew.json 里给供应商加 models 列表，可直接列出可选项）")
		return
	}
	for _, spec := range specs {
		marker := "  "
		if spec == r.cfg.Model {
			marker = "→ "
		}
		fmt.Fprintf(r.out, "    %s%s\n", marker, spec)
	}
	fmt.Fprintln(r.out, "\n  切换: /model deepseek/deepseek-chat")
}

func (r *REPL) printConfig() {
	fmt.Fprintln(r.out)
	if r.cfg.Empty() {
		fmt.Fprintln(r.out, "  当前状态: 未配置")
		for _, line := range strings.Split(configHint(r.cfg), "\n") {
			fmt.Fprintf(r.out, "  %s\n", line)
		}
		return
	}
	fmt.Fprintln(r.out, "  已配置的供应商:")
	for _, name := range r.cfg.ProviderNames() {
		p := r.cfg.Providers[name]
		status := "✓"
		if p.APIKey == "" {
			status = "✗"
		}
		fmt.Fprintf(r.out, "    %s %s\n", status, disp.Pad(name, 10))
		fmt.Fprintf(r.out, "      %-10s %s\n", "base_url:", p.BaseURL)
		fmt.Fprintf(r.out, "      %-10s %s\n", "api_key:", config.MaskKey(p.APIKey))
		if len(p.Models) > 0 {
			fmt.Fprintf(r.out, "      %-10s %s\n", "models:", strings.Join(p.Models, ", "))
		}
	}
	fmt.Fprintf(r.out, "\n  当前模型: %s\n", bright(r.cfg.Model))
	fmt.Fprintf(r.out, "  工作目录: %s\n", r.cfg.WorkDir())
	fmt.Fprintf(r.out, "  上下文预算: %d tokens / 单轮工具上限: %d\n", r.cfg.MaxContextTokens, r.cfg.MaxToolRounds)
	fmt.Fprintln(r.out, "  权限档位: "+permissionSummary(r.cfg))
	fmt.Fprintln(r.out, "\n  配置文件（按优先级）:")
	for _, path := range config.Paths(r.opt.BaseDir, r.opt.ConfigPath) {
		marker := "  "
		if path == r.cfg.Source {
			marker = "→ "
		}
		state := dim("不存在")
		if _, err := os.Stat(path); err == nil {
			state = ""
		}
		fmt.Fprintf(r.out, "    %s%s %s\n", marker, path, state)
	}
}

func permissionSummary(cfg *config.Config) string {
	if len(cfg.Permissions) == 0 {
		return dim("默认（写入/编辑/命令需确认，只读工具放行）")
	}
	var parts []string
	for _, k := range sortedKeys(cfg.Permissions) {
		parts = append(parts, k+"="+cfg.Permissions[k])
	}
	return strings.Join(parts, ", ")
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j++ {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func (r *REPL) printTools() {
	fmt.Fprintln(r.out, "\n  工具与权限:")
	for _, name := range r.registry.AllToolNames() {
		t, ok := r.registry.Get(name)
		if !ok {
			continue
		}
		decision := r.registry.Decide(name)
		inRole := "✗"
		if decision != tool.DecisionDeny {
			inRole = "✓"
		}
		fmt.Fprintf(r.out, "    %s %s %-8s %s\n", inRole, disp.Pad(name, 8), decision, dim(t.Description()))
	}
	fmt.Fprintln(r.out, "\n  调整: 修改 roles/*.md 的 tools，或用 /allow <tool> 临时放行")
}

func (r *REPL) printPermissions() {
	fmt.Fprintln(r.out, "\n  当前权限档位（allow 放行 / ask 询问 / deny 拒绝）:")
	for _, name := range r.registry.AllToolNames() {
		fmt.Fprintf(r.out, "    %-8s %s\n", disp.Pad(name, 8), r.registry.Decide(name))
	}
	fmt.Fprintln(r.out, "  配置方式: codecrew.json 里 \"permissions\": {\"bash\": \"allow\", \"write\": \"ask\"}")
}

func (r *REPL) allowTool(arg string) {
	name := strings.TrimSpace(arg)
	if name == "" {
		fmt.Fprintln(r.out, "  用法: /allow <tool>，例如 /allow bash")
		return
	}
	if _, ok := r.registry.Get(name); !ok {
		fmt.Fprintf(r.out, "  ✗ 未知工具 %s\n", name)
		return
	}
	if !r.currentHas(name) {
		fmt.Fprintf(r.out, "  ✗ 角色 %s 未声明 %s，请改 roles/ 里的白名单\n", r.current.Name, name)
		return
	}
	r.cfg.Permissions = setPerm(r.cfg.Permissions, name, "allow")
	r.registry.SetPermissions(r.cfg.Permissions)
	fmt.Fprintf(r.out, "  ✓ 本次运行内 %s 设为 allow（要长期生效请写入 codecrew.json）\n", name)
}

func setPerm(m map[string]string, k, v string) map[string]string {
	if m == nil {
		m = map[string]string{}
	}
	m[k] = v
	return m
}

func (r *REPL) currentHas(name string) bool {
	for _, t := range r.current.Tools {
		if t == name {
			return true
		}
	}
	return false
}

func (r *REPL) printPlan() {
	if r.plan == nil {
		fmt.Fprintln(r.out, "  当前没有计划工具")
		return
	}
	tasks := r.plan.Tasks()
	if len(tasks) == 0 {
		fmt.Fprintln(r.out, "  计划为空。让模型「先拆计划」即可自动填充，或用 /plan <文字> 手动加一条")
		return
	}
	fmt.Fprintln(r.out, "\n  任务计划:")
	done := 0
	for _, task := range tasks {
		mark := "[ ]"
		switch task.Status {
		case "doing":
			mark = "[>]"
		case "done":
			mark = "[x]"
			done++
		case "blocked":
			mark = "[!]"
		}
		fmt.Fprintf(r.out, "    %s #%-3d %s\n", mark, task.ID, task.Title)
	}
	fmt.Fprintf(r.out, "    %s\n", dim(fmt.Sprintf("进度 %d/%d", done, len(tasks))))
}

func (r *REPL) addPlanTask(title string) {
	if r.plan == nil {
		fmt.Fprintln(r.out, "  当前没有计划工具")
		return
	}
	out, err := r.plan.Execute(context.Background(), map[string]any{"action": "add", "title": title})
	if err != nil {
		fmt.Fprintf(r.out, "  ✗ %v\n", err)
		return
	}
	fmt.Fprintf(r.out, "  ✓ %s\n", out)
}

func (r *REPL) printContext() {
	used := r.contextTokens()
	limit := r.cfg.MaxContextTokens
	pct := 0
	if limit > 0 {
		pct = used * 100 / limit
	}
	bar := strings.Repeat("#", min(30, pct*30/100)) + strings.Repeat(".", max(0, 30-len(strings.Repeat("#", min(30, pct*30/100)))))
	fmt.Fprintf(r.out, "\n  上下文: %s [%s] %d%%\n", bright(fmt.Sprintf("%d / %d tokens", used, limit)), bar, pct)
	fmt.Fprintf(r.out, "  消息条数: %d（含 %d 条 system/摘要）\n", len(r.history), r.systemCount())
	fmt.Fprintln(r.out, "  "+dim("/compact 立即压缩，/clear 清空重来"))
}

func (r *REPL) systemCount() int {
	n := 0
	for _, m := range r.history {
		if m.Role == "system" {
			n++
		}
	}
	return n
}

func (r *REPL) clearHistory() {
	if len(r.history) > 0 && r.history[0].Role == "system" {
		r.history = []llm.Message{r.history[0]}
	} else {
		r.history = nil
	}
	fmt.Fprintln(r.out, "  ✓ 已清空对话历史（角色与配置保持不变）")
}

func (r *REPL) undo() {
	if len(r.history) <= 1 {
		fmt.Fprintln(r.out, "  没有可撤销的内容")
		return
	}
	removed := 0
	for len(r.history) > 1 {
		last := r.history[len(r.history)-1]
		r.history = r.history[:len(r.history)-1]
		removed++
		if last.Role == "user" {
			break
		}
	}
	fmt.Fprintf(r.out, "  ✓ 已回退 %d 条消息\n", removed)
}

func (r *REPL) printCost() {
	total := r.usage.prompt + r.usage.completion
	fmt.Fprintf(r.out, "\n  本次会话: %d 轮模型调用，%s\n", r.usage.turns, bright(fmt.Sprintf("tokens %d in / %d out / 共 %d", r.usage.prompt, r.usage.completion, total)))
	fmt.Fprintf(r.out, "  本地估算当前上下文: %d tokens\n", r.contextTokens())
	fmt.Fprintf(r.out, "  用时 %s，压缩 %d 次\n", time.Since(r.started).Round(time.Second), r.compacts)
	fmt.Fprintln(r.out, "  "+dim("供应商计费口径可能不同，此处仅作量级参考"))
}

func (r *REPL) listSessions() {
	if r.store == nil {
		fmt.Fprintln(r.out, "  会话存储不可用")
		return
	}
	list, err := r.store.List()
	if err != nil {
		fmt.Fprintf(r.out, "  ✗ 读取会话失败: %v\n", err)
		return
	}
	if len(list) == 0 {
		fmt.Fprintf(r.out, "  还没有历史会话（保存在 %s）\n", r.store.Dir())
		return
	}
	fmt.Fprintf(r.out, "\n  历史会话（目录 %s）:\n", r.store.Dir())
	for i, m := range list[:min(10, len(list))] {
		fmt.Fprintf(r.out, "    %2d. %s  %s/%d 条  %s\n", i+1, m.ID, m.Role, m.Messages, disp.Truncate(m.Preview, 40))
	}
	fmt.Fprintln(r.out, "\n  续聊: /resume <ID>")
}

func (r *REPL) resumeSession(arg string) {
	if r.store == nil {
		fmt.Fprintln(r.out, "  会话存储不可用")
		return
	}
	if arg == "" {
		r.listSessions()
		return
	}
	id := arg
	if n, err := strconv.Atoi(arg); err == nil && n > 0 {
		list, _ := r.store.List()
		if n <= len(list) {
			id = list[n-1].ID
		}
	}
	meta, messages, err := r.store.Load(id)
	if err != nil {
		fmt.Fprintf(r.out, "  ✗ 找不到会话 %s\n", arg)
		return
	}
	r.closeSession()
	r.history = []llm.Message{llm.TextMessage("system", r.current.Prompt)}
	r.openSession(id)
	if meta.Role != "" {
		r.switchRole(meta.Role)
	}
	if meta.Model != "" {
		r.switchModel(meta.Model)
	}
	fmt.Fprintf(r.out, "  ✓ 已恢复会话 %s（%d 条消息）\n", meta.ID, len(messages))
}

func (r *REPL) newSession() {
	r.closeSession()
	r.history = []llm.Message{llm.TextMessage("system", r.current.Prompt)}
	r.usage = usageTracker{}
	r.openSession("")
	fmt.Fprintf(r.out, "  ✓ 已开新会话 %s\n", r.sessionPath())
}

func (r *REPL) openSession(id string) {
	if r.store == nil {
		return
	}
	if id != "" {
		meta, messages, err := r.store.Load(id)
		if err == nil {
			for _, m := range messages {
				if m.Role != "system" {
					r.history = append(r.history, m)
				}
			}
			sess, serr := r.store.New(session.Meta{
				ID: meta.ID, Role: r.current.Name, Model: r.cfg.Model, WorkDir: r.cfg.WorkDir(),
			})
			if serr == nil {
				for _, m := range messages {
					if m.Role != "system" {
						sess.Append(m)
					}
				}
				r.session = sess
				fmt.Fprintf(r.out, "  ✓ 续聊会话 %s（%d 条消息）\n", meta.ID, len(messages))
				return
			}
		}
		fmt.Fprintf(r.out, "  ⚠ 找不到会话 %s，改为新开会话\n", id)
	}
	sess, err := r.store.New(session.Meta{Role: r.current.Name, Model: r.cfg.Model, WorkDir: r.cfg.WorkDir()})
	if err != nil {
		fmt.Fprintf(r.out, "  ⚠ 会话无法保存: %v\n", err)
		return
	}
	r.session = sess
}

func (r *REPL) saveSession() {
	if r.session != nil {
		r.session.Flush()
	}
}

func (r *REPL) closeSession() {
	if r.session != nil {
		r.session.Close()
		r.session = nil
	}
}

func (r *REPL) sessionPath() string {
	if r.session == nil {
		return "(未保存)"
	}
	return r.session.Path()
}

// ------------------------------------------------------------------ 展示

// SetRole 在启动阶段设置角色，找不到时报错。
func (r *REPL) SetRole(name string) error {
	target, ok := role.Get(r.roles, name)
	if !ok {
		// 容忍用户刚往 roles/ 里丢文件却没 /reload
		if refreshed, err := role.Load(r.roleDirs()...); err == nil {
			r.roles = refreshed
			target, ok = role.Get(refreshed, name)
		}
	}
	if !ok {
		names := make([]string, 0, len(r.roles))
		for _, rr := range r.roles {
			names = append(names, rr.Name)
		}
		return fmt.Errorf("未找到角色 %q，可用: %s", name, strings.Join(names, ", "))
	}
	r.current = target
	r.applyRole(target)
	r.history = []llm.Message{llm.TextMessage("system", target.Prompt)}
	return nil
}

// History 返回当前对话历史副本，便于测试与二次加工。
func (r *REPL) History() []llm.Message {
	out := make([]llm.Message, len(r.history))
	copy(out, r.history)
	return out
}

// roleDirs 返回角色搜索路径：随二进制分发的 roles/ 在前，用户项目里的 roles/ 可覆盖它。
func (r *REPL) roleDirs() []string {
	return roleDirsFor(r.opt.BaseDir, r.cfg.WorkDir())
}

func roleDirsFor(baseDir, workDir string) []string {
	dirs := []string{filepath.Join(baseDir, "roles")}
	if workDir != "" && filepath.Clean(workDir) != filepath.Clean(baseDir) {
		dirs = append(dirs, filepath.Join(workDir, "roles"))
	}
	return dirs
}

func (r *REPL) buildClient() *llm.Client {
	if r.cfg.Empty() || r.cfg.Model == "" {
		return nil
	}
	provider, modelID, err := r.cfg.Resolve(r.cfg.Model)
	if err != nil {
		fmt.Fprintf(r.out, "  ⚠ 模型配置有误: %v\n", err)
		return nil
	}
	if provider.APIKey == "" && !isLocal(provider.BaseURL) {
		fmt.Fprintf(r.out, "  ⚠ 供应商 %s 没有 api_key，若为本地模型可忽略\n", firstProvider(r.cfg, provider.BaseURL))
	}
	return llm.New(provider.BaseURL, provider.APIKey, modelID)
}

func isLocal(baseURL string) bool {
	lower := strings.ToLower(baseURL)
	return strings.Contains(lower, "localhost") || strings.Contains(lower, "127.0.0.1") || strings.Contains(lower, "0.0.0.0")
}

func firstProvider(cfg *config.Config, baseURL string) string {
	for _, name := range cfg.ProviderNames() {
		if cfg.Providers[name].BaseURL == baseURL {
			return name
		}
	}
	return "?"
}

func (r *REPL) modelLabel() string {
	if r.cfg.Model == "" || r.client == nil {
		return "未配置"
	}
	_, modelID, _ := strings.Cut(r.cfg.Model, "/")
	return modelID
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
		{"/context · /compact", "查看上下文占用 / 立即压缩历史"},
		{"/clear · /undo", "清空历史 / 回退上一轮"},
		{"/sessions · /resume <id> · /new", "历史会话列表 / 续聊 / 新开会话"},
		{"/cost", "本次会话 token 与耗时统计"},
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

// -------------------------------------------------------------- 小工具函数

func dim(s string) string    { return "\x1b[2m" + s + "\x1b[0m" }
func bright(s string) string { return "\x1b[1m" + s + "\x1b[0m" }

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
