package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"codecrew/internal/llm"
)

// Decision 是权限闸门的判定结果。
type Decision int

const (
	DecisionAllow Decision = iota
	DecisionAsk
	DecisionDeny
)

func (d Decision) String() string {
	switch d {
	case DecisionAllow:
		return "allow"
	case DecisionAsk:
		return "ask"
	default:
		return "deny"
	}
}

// ParsePermission 把配置字符串转成 Decision，未知值按 ask 处理（默认更安全）。
func ParsePermission(v string) Decision {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "allow", "true", "auto", "y":
		return DecisionAllow
	case "deny", "block", "never", "off":
		return DecisionDeny
	default:
		return DecisionAsk
	}
}

// Approver 由交互层注入：展示待执行动作并决定是否放行。
type Approver func(t Tool, args map[string]any) Decision

// Registry 持有工具集合，并实施「角色白名单 → 用户确认 → 权限档位」三层闸门。
type Registry struct {
	tools     map[string]Tool
	order     []string
	roleAllow map[string]bool
	modes     map[string]Decision // 来自配置：tool -> allow/ask/deny
	defaults  map[string]Decision // 工具自带的默认档位
	approver  Approver            // 交互确认，nil 表示按配置放行
	approved  map[string]bool     // 本次会话已确认过的工具
}

// NewRegistry 创建空注册表。
func NewRegistry() *Registry {
	return &Registry{
		tools:     map[string]Tool{},
		roleAllow: map[string]bool{},
		modes:     map[string]Decision{},
		defaults:  map[string]Decision{},
		approved:  map[string]bool{},
	}
}

// NewDefaultRegistry 注册全部内置工具，工作目录作为文件类工具的根。
func NewDefaultRegistry(workDir string) *Registry {
	r := NewRegistry()
	r.Register(NewReadTool(workDir), DecisionAllow)
	r.Register(NewGlobTool(workDir), DecisionAllow)
	r.Register(NewGrepTool(workDir), DecisionAllow)
	r.Register(NewPlanTool(), DecisionAllow)
	r.Register(NewWriteTool(workDir), DecisionAsk)
	r.Register(NewEditTool(workDir), DecisionAsk)
	r.Register(NewBashTool(workDir), DecisionAsk)
	return r
}

// Register 加入一个工具，并声明它在缺省配置下的权限档位。
func (r *Registry) Register(t Tool, mode Decision) {
	name := t.Name()
	if _, dup := r.tools[name]; !dup {
		r.order = append(r.order, name)
	}
	r.tools[name] = t
	r.defaults[name] = mode
}

// SetRoleAllowed 用角色白名单重置允许集合；未列出的工具对当前角色不可见。
func (r *Registry) SetRoleAllowed(names []string) {
	r.roleAllow = map[string]bool{}
	for _, n := range names {
		r.roleAllow[n] = true
	}
	r.approved = map[string]bool{}
}

// SetPermissions 应用配置里的 permissions 映射（tool 或 "*" -> allow/ask/deny）。
func (r *Registry) SetPermissions(m map[string]string) {
	r.modes = map[string]Decision{}
	for k, v := range m {
		r.modes[k] = ParsePermission(v)
	}
}

// Remember 记住某工具已被用户放行（本角色内不再询问）。
func (r *Registry) Remember(name string) { r.approved[name] = true }

// ResetApprovals 清空「本次会话已确认」记录（切换角色时调用）。
func (r *Registry) ResetApprovals() { r.approved = map[string]bool{} }

// SetApprover 注入交互确认逻辑。
func (r *Registry) SetApprover(a Approver) { r.approver = a }

// Decide 计算一次调用的权限判定。
func (r *Registry) Decide(name string) Decision {
	if !r.roleAllow[name] {
		return DecisionDeny
	}
	if d, ok := r.modes[name]; ok {
		return d
	}
	if d, ok := r.modes["*"]; ok {
		return d
	}
	if r.approved[name] {
		return DecisionAllow
	}
	if d, ok := r.defaults[name]; ok {
		return d
	}
	return DecisionAsk
}

// denyReason 给出被拒绝的准确原因，供回填给模型的错误文案使用。
func (r *Registry) denyReason(name string) string {
	if !r.roleAllow[name] {
		return fmt.Sprintf("工具 %s 未授权给当前角色，请在 roles/*.md 的 tools 中添加", name)
	}
	if d, ok := r.modes[name]; ok && d == DecisionDeny {
		return fmt.Sprintf("工具 %s 被 permissions 配置设为 deny，如需使用请修改 codecrew.json 或用 /allow %s 临时放行", name, name)
	}
	if d, ok := r.modes["*"]; ok && d == DecisionDeny {
		return fmt.Sprintf("工具 %s 命中通配 deny，请为它在 permissions 里单独放行", name)
	}
	return fmt.Sprintf("工具 %s 当前不可用", name)
}

// AllowedNames 返回当前角色可见的工具名（用于喂给模型）。
func (r *Registry) AllowedNames() []string {
	var names []string
	for _, name := range r.order {
		if d := r.Decide(name); d != DecisionDeny {
			names = append(names, name)
		}
	}
	return names
}

// AllToolNames 返回注册顺序的工具名。
func (r *Registry) AllToolNames() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Get 按名取工具。
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Schemas 输出 OpenAI function calling 格式的工具声明，仅含当前角色可见的工具。
func (r *Registry) Schemas() []map[string]any {
	var out []map[string]any
	for _, name := range r.order {
		if r.Decide(name) == DecisionDeny {
			continue
		}
		t := r.tools[name]
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name(),
				"description": t.Description(),
				"parameters":  t.Schema(),
			},
		})
	}
	return out
}

// Execute 经过权限闸门后执行工具；模型看到的错误以文本形式返回，便于自我纠正。
func (r *Registry) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("未知工具 %s，可用工具: %s", name, strings.Join(r.AllowedNames(), ", "))
	}
	switch r.Decide(name) {
	case DecisionDeny:
		return "", fmt.Errorf("%s", r.denyReason(name))
	case DecisionAsk:
		if r.approver != nil {
			if d := r.approver(t, args); d == DecisionDeny {
				return "", fmt.Errorf("用户拒绝执行 %s", name)
			} else if d == DecisionAllow {
				r.approved[name] = true
			}
		}
	}

	out, err := t.Execute(ctx, args)
	if err != nil {
		return "", err
	}
	return FormatOutput(out, MaxOutputLines), nil
}

// Summary 生成一行人类可读的调用摘要，用于确认提示与日志。
func Summary(t Tool, args map[string]any) string {
	switch t.Name() {
	case "bash":
		if cmd, ok := args["command"].(string); ok {
			return compactOneLine(cmd, 160)
		}
	case "write", "edit", "read", "glob", "grep":
		var parts []string
		for _, key := range []string{"path", "pattern", "query", "root", "action", "command"} {
			if v, ok := args[key].(string); ok && v != "" {
				parts = append(parts, key+"="+compactOneLine(v, 60))
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	}
	data, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return compactOneLine(string(data), 160)
}

func compactOneLine(s string, max int) string {
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// SummaryRaw 把一批工具调用压成一行摘要，用于上下文与日志展示。
func SummaryRaw(calls []llm.ToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	var parts []string
	for _, c := range calls {
		parts = append(parts, c.Function.Name+"("+compactOneLine(c.Function.Arguments, 60)+")")
	}
	return strings.Join(parts, ", ")
}

// IsDangerousCommand 识别需要强制确认的破坏性命令。
func IsDangerousCommand(cmd string) bool {
	lower := strings.ToLower(cmd)
	for _, p := range []string{
		"rm -rf", "rm -r ", "rmdir /s", "del /f", "rd /s", "format ", "mkfs", "dd if=",
		"shutdown", "reboot", "diskpart", "git push --force", "git push -f",
		"git reset --hard", "clean -fdx", "chmod -r 777", "chmod 000", "sudo ",
		"reg delete", "taskkill /f", "dispatch/expression", "set-executionpolicy",
	} {
		if strings.Contains(lower, p) {
			return true
		}
	}
	// 管道到 shell 解释器执行：curl/wget/iwr ... | sh/bash/powershell/iex
	fields := strings.Fields(strings.ReplaceAll(lower, "|", " | "))
	for i, f := range fields {
		if f != "|" || i+1 >= len(fields) {
			continue
		}
		if pipeTarget(fields[i+1]) && i > 0 && pipeSource(fields[i-1]) {
			return true
		}
	}
	return false
}

func pipeTarget(field string) bool {
	field = strings.Trim(field, "'\";")
	switch field {
	case "sh", "bash", "zsh", "iex", "iwr", "python", "node", "cmd", "powershell", "wsh":
		return true
	}
	return strings.HasPrefix(field, "invoke-")
}

func pipeSource(field string) bool {
	field = strings.Trim(field, "'\";")
	switch field {
	case "curl", "wget", "iwr", "invoke-webrequest", "fetch", "cat", "echo":
		return true
	}
	return strings.HasPrefix(field, "http://") || strings.HasPrefix(field, "https://")
}
