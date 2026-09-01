package repl

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"codecrew/internal/config"
	"codecrew/internal/disp"
	"codecrew/internal/llm"
	"codecrew/internal/memory"
	"codecrew/internal/reasoning"
	"codecrew/internal/role"
	"codecrew/internal/tool"
)

func (r *REPL) historyText() string {
	var sb strings.Builder
	for _, m := range r.history {
		sb.WriteString(m.Content)
	}
	return sb.String()
}

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
	// write / edit 前展示统一 diff 预览
	if t.Name() == "write" || t.Name() == "edit" {
		r.showDiffPreview(t.Name(), args)
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

// showDiffPreview 在 write/edit 前展示统一 diff。失败时静默跳过，不影响主流程。
func (r *REPL) showDiffPreview(toolName string, args map[string]any) {
	path, _ := args["path"].(string)
	if path == "" {
		return
	}
	// 解析为工作目录下的绝对路径，用于读取原文件
	absPath := path
	if r.cfg.WorkDir() != "" {
		if abs, err := tool.SafePath(r.cfg.WorkDir(), path); err == nil {
			absPath = abs
		}
	}
	var diff string
	var err error
	switch toolName {
	case "write":
		content, _ := args["content"].(string)
		diff = tool.PreviewWrite(absPath, content)
	case "edit":
		oldText, _ := args["old_text"].(string)
		newText, _ := args["new_text"].(string)
		diff, err = tool.PreviewEdit(absPath, oldText, newText)
	}
	if err != nil || diff == "" {
		return
	}
	fmt.Fprintln(r.out)
	fmt.Fprintln(r.out, "  "+bright("变更预览："))
	for _, line := range strings.Split(diff, "\n") {
		if line == "" {
			continue
		}
		prefix := "  "
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			prefix = "  " + dim(line)
			fmt.Fprintln(r.out, prefix)
			continue
		}
		if strings.HasPrefix(line, "@@") {
			fmt.Fprintf(r.out, "  %s\n", dim(line))
			continue
		}
		if strings.HasPrefix(line, "-") {
			fmt.Fprintf(r.out, "  \x1b[31m%s\x1b[0m\n", line)
			continue
		}
		if strings.HasPrefix(line, "+") {
			fmt.Fprintf(r.out, "  \x1b[32m%s\x1b[0m\n", line)
			continue
		}
		fmt.Fprintf(r.out, "  %s\n", line)
	}
	fmt.Fprintln(r.out)
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

func (r *REPL) applyRole(target role.Role) {
	r.registry.SetRoleAllowed(target.Tools)
	r.registry.SetPermissions(r.cfg.Permissions)
}

// systemPromptFor 构建角色的 system prompt，并自动注入该角色的长期记忆。
func (r *REPL) systemPromptFor(target role.Role) string {
	prompt := target.Prompt
	if r.memory != nil {
		if mem, err := r.memory.Load(target.Name); err == nil && mem != "" {
			prompt = memory.InjectPrompt(prompt, mem)
		}
	}
	// ReAct 模式：添加显式推理格式指令
	if r.reasoningCfg.Mode == reasoning.ModeReAct || r.reasoningCfg.Mode == reasoning.ModeReflexion {
		prompt += "\n" + reasoning.ReActSystemPrompt()
	}
	// 注入历史反思经验
	if r.reasoningCfg.InjectReflections && r.reflexion != nil {
		if reflections := r.reflexion.InjectReflections(target.Name); reflections != "" {
			prompt += reflections
		}
	}
	// 注入情景记忆（最近任务摘要）
	if r.cfg.Knowledge.GetInjectEpisodic() && r.episodicStore != nil {
		if episodic := r.episodicStore.InjectPrompt(r.cfg.Knowledge.GetEpisodicCount()); episodic != "" {
			prompt += episodic
		}
	}
	return prompt
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
	client := llm.New(provider.BaseURL, provider.APIKey, modelID)
	if r.cfg.Temperature != nil {
		client.Temperature = r.cfg.Temperature
	}
	return client
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

func dim(s string) string { return "\x1b[2m" + s + "\x1b[0m" }

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
