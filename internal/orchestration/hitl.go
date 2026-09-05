package orchestration

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// PendingApproval 待人工审批的操作。
type PendingApproval struct {
	ID          int        `json:"id"`
	Type        string     `json:"type"` // tool_call / task / plan / dangerous_action
	Description string     `json:"description"`
	Detail      string     `json:"detail,omitempty"`
	RequestedAt time.Time  `json:"requested_at"`
	Status      string     `json:"status"` // pending / approved / denied / timeout
	DecidedAt   *time.Time `json:"decided_at,omitempty"`
}

// HITLState Human-in-the-Loop 状态。
type HITLState struct {
	mu          sync.Mutex
	Enabled     bool              `json:"enabled"`
	Pending     []PendingApproval `json:"pending"`
	History     []PendingApproval `json:"history"`
	NextID      int               `json:"next_id"`
	AutoApprove map[string]bool   `json:"auto_approve"` // 自动批准的操作类型
	TimeoutSecs int               `json:"timeout_secs"` // 超时时间（秒）
}

// NewHITLState 创建 HITL 状态。
func NewHITLState() *HITLState {
	return &HITLState{
		Enabled:     false,
		NextID:      1,
		AutoApprove: make(map[string]bool),
		TimeoutSecs: 300, // 默认 5 分钟超时
	}
}

// RequestApproval 请求人工审批。
func (h *HITLState) RequestApproval(approvalType, description, detail string) *PendingApproval {
	h.mu.Lock()
	defer h.mu.Unlock()

	// 检查是否自动批准
	if h.AutoApprove[approvalType] {
		now := time.Now()
		approved := PendingApproval{
			ID:          h.NextID,
			Type:        approvalType,
			Description: description,
			Detail:      detail,
			RequestedAt: now,
			Status:      "approved",
			DecidedAt:   &now,
		}
		h.History = append(h.History, approved)
		h.NextID++
		return &approved
	}

	approval := PendingApproval{
		ID:          h.NextID,
		Type:        approvalType,
		Description: description,
		Detail:      detail,
		RequestedAt: time.Now(),
		Status:      "pending",
	}
	h.Pending = append(h.Pending, approval)
	h.NextID++
	return &approval
}

// Approve 批准待审批操作。
func (h *HITLState) Approve(id int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	for i := range h.Pending {
		if h.Pending[i].ID == id {
			now := time.Now()
			h.Pending[i].Status = "approved"
			h.Pending[i].DecidedAt = &now
			h.History = append(h.History, h.Pending[i])
			h.Pending = append(h.Pending[:i], h.Pending[i+1:]...)
			return true
		}
	}
	return false
}

// Deny 拒绝待审批操作。
func (h *HITLState) Deny(id int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	for i := range h.Pending {
		if h.Pending[i].ID == id {
			now := time.Now()
			h.Pending[i].Status = "denied"
			h.Pending[i].DecidedAt = &now
			h.History = append(h.History, h.Pending[i])
			h.Pending = append(h.Pending[:i], h.Pending[i+1:]...)
			return true
		}
	}
	return false
}

// GetPending 获取待审批操作。
func (h *HITLState) GetPending(id int) *PendingApproval {
	h.mu.Lock()
	defer h.mu.Unlock()

	for i := range h.Pending {
		if h.Pending[i].ID == id {
			return &h.Pending[i]
		}
	}
	return nil
}

// ListPending 列出所有待审批操作。
func (h *HITLState) ListPending() []PendingApproval {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]PendingApproval, len(h.Pending))
	copy(out, h.Pending)
	return out
}

// HasPending 是否有待审批操作。
func (h *HITLState) HasPending() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.Pending) > 0
}

// SetAutoApprove 设置自动批准。
func (h *HITLState) SetAutoApprove(approvalType string, auto bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.AutoApprove[approvalType] = auto
}

// Render 返回 HITL 状态的文本表示。
func (h *HITLState) Render() string {
	h.mu.Lock()
	defer h.mu.Unlock()

	var sb strings.Builder
	fmt.Fprintf(&sb, "🤖 Human-in-the-Loop: %s\n", map[bool]string{true: "开启", false: "关闭"}[h.Enabled])
	fmt.Fprintf(&sb, "   待审批: %d 项\n", len(h.Pending))

	if len(h.Pending) > 0 {
		fmt.Fprintln(&sb, "\n待审批列表:")
		for _, p := range h.Pending {
			elapsed := time.Since(p.RequestedAt).Round(time.Second)
			fmt.Fprintf(&sb, "  #%d [%s] %s (等待 %s)\n", p.ID, p.Type, p.Description, elapsed)
			if p.Detail != "" {
				fmt.Fprintf(&sb, "      详情: %s\n", truncate(p.Detail, 100))
			}
		}
		fmt.Fprintln(&sb, "\n用法: /approve <ID> 批准, /deny <ID> 拒绝")
	}

	if len(h.AutoApprove) > 0 {
		fmt.Fprintln(&sb, "\n自动批准:")
		for t, auto := range h.AutoApprove {
			if auto {
				fmt.Fprintf(&sb, "  - %s: 自动批准\n", t)
			}
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}
