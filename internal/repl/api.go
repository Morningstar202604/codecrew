package repl

import (
	"context"
	"fmt"
	"time"

	"codecrew/internal/config"
	"codecrew/internal/eval"
	"codecrew/internal/knowledge"
	"codecrew/internal/llm"
	"codecrew/internal/memory"
	"codecrew/internal/orchestration"
	"codecrew/internal/reasoning"
	"codecrew/internal/role"
	"codecrew/internal/session"
	"codecrew/internal/tool"
	"codecrew/internal/verify"
)

func (r *REPL) AllToolNames() []string { return r.registry.AllToolNames() }

// ToolDecision 返回某工具的当前权限判定。
func (r *REPL) ToolDecision(name string) string { return r.registry.Decide(name).String() }

// ToolDescription 返回某工具的描述，不存在时返回名称本身。
func (r *REPL) ToolDescription(name string) string {
	if t, ok := r.registry.Get(name); ok {
		return t.Description()
	}
	return name
}

// AllowTool 临时放行某工具（本次运行内生效）。
func (r *REPL) AllowTool(name string) error {
	if _, ok := r.registry.Get(name); !ok {
		return fmt.Errorf("未知工具 %s", name)
	}
	if !r.currentHas(name) {
		return fmt.Errorf("角色 %s 未声明 %s", r.current.Name, name)
	}
	r.cfg.Permissions = setPerm(r.cfg.Permissions, name, "allow")
	r.registry.SetPermissions(r.cfg.Permissions)
	return nil
}

// CompactNow 立即压缩上下文。
func (r *REPL) CompactNow() error { return r.compact(true) }

// ClearHistory 清空对话历史（保留角色）。
func (r *REPL) ClearHistory() { r.clearHistory() }

// Undo 回退上一轮。
func (r *REPL) Undo() { r.undo() }

// ExitRequested 返回是否请求退出（EOF 等情况）。
func (r *REPL) ExitRequested() bool { return r.exit }

// SetExit 清除退出标志，供 Web 模式复用 REPL 实例时使用。
func (r *REPL) SetExit(v bool) { r.exit = v }

// HandleReasoningAPI 处理推理模式切换（Web API 用）。
func (r *REPL) HandleReasoningAPI(mode string) {
	r.handleReasoning(mode)
}

// ReasoningStatus 返回推理模式状态（Web API 用）。
func (r *REPL) ReasoningStatus() (string, bool, bool, int) {
	return r.reasoningCfg.Mode.String(), r.reasoningCfg.ShowThoughts, r.reasoningCfg.AutoReflect, r.reasoningCfg.ReflectionDepth
}

// ListFailures 返回失败经验列表（Web API 用）。
func (r *REPL) ListFailures() []reasoning.Failure {
	if r.failureStore == nil {
		return nil
	}
	list, _ := r.failureStore.List(r.current.Name)
	return list
}

// ClearFailures 清空失败经验（Web API 用）。
func (r *REPL) ClearFailures() {
	if r.failureStore != nil {
		r.failureStore.Clear(r.current.Name)
	}
}

// VerifyEnabled 返回验证是否启用（Web API 用）。
func (r *REPL) VerifyEnabled() bool {
	return r.verifyEngine != nil
}

// RunVerifyAPI 运行验证（Web API 用）。
func (r *REPL) RunVerifyAPI(withRepair bool) verify.Result {
	result := r.runVerify(context.Background())
	if withRepair && !result.Passed {
		r.repairLoop(context.Background(), result)
	}
	return result
}

// IndexStatus 返回索引状态（Web API 用）。
func (r *REPL) IndexStatus() map[string]any {
	if r.codebaseIndex == nil {
		return map[string]any{"enabled": false}
	}
	meta := r.codebaseIndex.Meta()
	return map[string]any{
		"enabled":      true,
		"root_dir":     meta.RootDir,
		"file_count":   meta.FileCount,
		"symbol_count": meta.SymbolCount,
		"updated_at":   meta.UpdatedAt,
		"is_stale":     r.codebaseIndex.IsStale(),
	}
}

// BuildIndex 构建索引（Web API 用，异步）。
func (r *REPL) BuildIndex() {
	if r.codebaseIndex != nil {
		r.codebaseIndex.Build()
	}
}

// SearchCodeAPI 搜索代码（Web API 用）。
func (r *REPL) SearchCodeAPI(query string, limit int) []knowledge.SearchResult {
	if r.searcher == nil {
		return nil
	}
	return r.searcher.Search(query, limit)
}

// SupervisorAPI 处理 Supervisor 操作（Web API 用）。
func (r *REPL) SupervisorAPI(action, worker, task string, id int, result string) {
	if r.supervisorState == nil {
		r.supervisorState = orchestration.NewSupervisorState()
	}
	switch action {
	case "on":
		r.supervisorState.Enabled = true
	case "off":
		r.supervisorState.Enabled = false
	case "assign":
		r.supervisorState.AssignTask(task, worker)
	case "done":
		r.supervisorState.UpdateTask(id, "done", result)
	}
}

// SupervisorStatus 返回 Supervisor 状态（Web API 用）。
func (r *REPL) SupervisorStatus() map[string]any {
	if r.supervisorState == nil {
		return map[string]any{"enabled": false}
	}
	done, total := r.supervisorState.Progress()
	return map[string]any{
		"enabled":  r.supervisorState.Enabled,
		"goal":     r.supervisorState.Goal,
		"workers":  r.supervisorState.Workers,
		"tasks":    r.supervisorState.Tasks,
		"progress": map[string]int{"done": done, "total": total},
	}
}

// ApproveAPI 批准操作（Web API 用）。
func (r *REPL) ApproveAPI(id int) {
	if r.hitlState != nil {
		r.hitlState.Approve(id)
	}
}

// DenyAPI 拒绝操作（Web API 用）。
func (r *REPL) DenyAPI(id int) {
	if r.hitlState != nil {
		r.hitlState.Deny(id)
	}
}

// RunEvalAPI 运行评估（Web API 用，异步）。
func (r *REPL) RunEvalAPI() {
	if r.evalHarness != nil {
		r.evalHarness.Run(context.Background(), "Web 触发评估", nil)
	}
}

// ListEvalReports 返回评估报告列表（Web API 用）。
func (r *REPL) ListEvalReports() []eval.EvalReport {
	if r.evalHarness == nil {
		return nil
	}
	reports, _ := r.evalHarness.ListReports()
	return reports
}

// Send 向 REPL 发送一条输入（可以是普通对话或 / 命令），阻塞执行直到完成。
func (r *REPL) Send(input string) {
	r.handleInput(input)
	r.saveSession()
}

// CurrentRoleName 返回当前角色名。
func (r *REPL) CurrentRoleName() string { return r.current.Name }

// CurrentRole 返回当前角色的完整信息。
func (r *REPL) CurrentRole() role.Role { return r.current }

// Roles 返回所有可用角色。
func (r *REPL) Roles() []role.Role {
	out := make([]role.Role, len(r.roles))
	copy(out, r.roles)
	return out
}

// CurrentModel 返回当前模型 spec。
func (r *REPL) CurrentModel() string { return r.cfg.Model }

// Config 返回当前配置。
func (r *REPL) Config() *config.Config { return r.cfg }

// History 返回当前会话的消息历史。
func (r *REPL) History() []llm.Message {
	out := make([]llm.Message, len(r.history))
	copy(out, r.history)
	return out
}

// MemoryStore 返回角色记忆存储。
func (r *REPL) MemoryStore() *memory.Store { return r.memory }

// SessionStore 返回会话存储。
func (r *REPL) SessionStore() *session.Store { return r.store }

// PlanTasks 返回当前计划任务列表。
func (r *REPL) PlanTasks() []tool.Task {
	if r.plan == nil {
		return nil
	}
	return r.plan.Tasks()
}

// ContextStats 返回上下文使用量和限制。
func (r *REPL) ContextStats() (used, limit int) {
	return r.contextTokens(), r.cfg.MaxContextTokens
}

// CostStats 返回本次会话的统计信息。
func (r *REPL) CostStats() (turns, prompt, completion int, elapsed time.Duration) {
	return r.usage.turns, r.usage.prompt, r.usage.completion, time.Since(r.started)
}

// CompactionCount 返回历史压缩次数。
func (r *REPL) CompactionCount() int { return r.compacts }

// ToolNames 返回当前角色允许使用的工具名。
func (r *REPL) ToolNames() []string { return r.registry.AllowedNames() }
