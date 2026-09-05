package repl

import (
	"context"
	"fmt"
	"os"
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
		r.history[0] = llm.TextMessage("system", r.systemPromptFor(target))
	} else {
		r.history = append([]llm.Message{llm.TextMessage("system", r.systemPromptFor(target))}, r.history...)
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
	r.rebuildReflexion()
	r.initPlanner()
	r.initKnowledge()
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
	r.initReasoning()
	r.initVerify()
	r.initPlanner()
	r.initKnowledge()
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

// handleMemory 处理 /memory 命令：查看 / 添加 / 清空当前角色的长期记忆。
func (r *REPL) handleMemory(arg string) {
	if r.memory == nil {
		fmt.Fprintln(r.out, "  ✗ 记忆存储不可用")
		return
	}
	roleName := r.current.Name
	arg = strings.TrimSpace(arg)

	// 子命令：add / clear / list
	if strings.HasPrefix(arg, "add ") {
		note := strings.TrimPrefix(arg, "add ")
		if err := r.memory.Append(roleName, note); err != nil {
			fmt.Fprintf(r.out, "  ✗ %v\n", err)
			return
		}
		fmt.Fprintf(r.out, "  ✓ 已添加到 %s 的记忆\n", bright(roleName))
		// 重新注入记忆到 system prompt
		if len(r.history) > 0 && r.history[0].Role == "system" {
			r.history[0] = llm.TextMessage("system", r.systemPromptFor(r.current))
		}
		return
	}
	if arg == "clear" {
		if err := r.memory.Clear(roleName); err != nil {
			fmt.Fprintf(r.out, "  ✗ %v\n", err)
			return
		}
		fmt.Fprintf(r.out, "  ✓ 已清空 %s 的记忆\n", bright(roleName))
		if len(r.history) > 0 && r.history[0].Role == "system" {
			r.history[0] = llm.TextMessage("system", r.systemPromptFor(r.current))
		}
		return
	}
	if arg == "list" || arg == "" {
		mem, err := r.memory.Load(roleName)
		if err != nil {
			fmt.Fprintf(r.out, "  ✗ %v\n", err)
			return
		}
		fmt.Fprintf(r.out, "\n  %s 的长期记忆（%s）:\n", bright(roleName), r.memory.Path(roleName))
		if mem == "" {
			fmt.Fprintln(r.out, "  （暂无记忆。用 /memory add <内容> 添加，记忆会自动注入到 system prompt）")
		} else {
			for _, line := range strings.Split(mem, "\n") {
				fmt.Fprintf(r.out, "  %s\n", line)
			}
		}
		// 列出其他有记忆的角色
		if all, err := r.memory.List(); err == nil && len(all) > 1 {
			var others []string
			for _, n := range all {
				if n != roleName {
					others = append(others, n)
				}
			}
			if len(others) > 0 {
				fmt.Fprintf(r.out, "\n  %s\n", dim("其他角色也有记忆: "+strings.Join(others, ", ")))
			}
		}
		fmt.Fprintln(r.out, "\n  用法: /memory 查看  /memory add <内容>  /memory clear 清空")
		return
	}
	fmt.Fprintln(r.out, "  用法: /memory [add <内容>|clear|list]")
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
	r.history = []llm.Message{llm.TextMessage("system", r.systemPromptFor(r.current))}
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
	r.history = []llm.Message{llm.TextMessage("system", r.systemPromptFor(r.current))}
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
	r.history = []llm.Message{llm.TextMessage("system", r.systemPromptFor(target))}
	return nil
}

// printHistory 显示命令历史。
func (r *REPL) printHistory() {
	fmt.Fprintln(r.out, "\n  命令历史")
	fmt.Fprintln(r.out, "  "+strings.Repeat("─", 50))
	for i, cmd := range r.cmdHistory {
		fmt.Fprintf(r.out, "  %3d  %s\n", i+1, cmd)
	}
	if len(r.cmdHistory) == 0 {
		fmt.Fprintln(r.out, "  (暂无历史记录)")
	}
	fmt.Fprintln(r.out)
}

// addToHistory 添加命令到历史。
func (r *REPL) addToHistory(cmd string) {
	if cmd == "" || strings.HasPrefix(cmd, "/history") {
		return
	}
	// 避免连续重复
	if len(r.cmdHistory) > 0 && r.cmdHistory[len(r.cmdHistory)-1] == cmd {
		return
	}
	r.cmdHistory = append(r.cmdHistory, cmd)
	// 最多保留 100 条
	if len(r.cmdHistory) > 100 {
		r.cmdHistory = r.cmdHistory[1:]
	}
}
