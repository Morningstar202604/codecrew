// Package orchestration 实现多 Agent 编排模式。
//
// 包含 Supervisor 监督者模式、Human-in-the-Loop 人工介入等编排能力。
package orchestration

import (
	"codecrew/internal/llm"
	"context"
	"fmt"
	"strings"
	"time"
)

// WorkerRole 工作者角色定义。
type WorkerRole struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Specialty   string `json:"specialty"` // 专长领域
}

// TaskAssignment 任务分配。
type TaskAssignment struct {
	ID         int        `json:"id"`
	Task       string     `json:"task"`
	Worker     string     `json:"worker"`
	Status     string     `json:"status"` // pending / running / done / failed / blocked
	Result     string     `json:"result,omitempty"`
	AssignedAt time.Time  `json:"assigned_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// SupervisorState Supervisor 模式状态。
type SupervisorState struct {
	Enabled    bool             `json:"enabled"`
	Goal       string           `json:"goal"`
	Workers    []WorkerRole     `json:"workers"`
	Tasks      []TaskAssignment `json:"tasks"`
	NextTaskID int              `json:"next_task_id"`
}

// NewSupervisorState 创建 Supervisor 状态。
func NewSupervisorState() *SupervisorState {
	return &SupervisorState{
		Workers:    defaultWorkers(),
		NextTaskID: 1,
	}
}

// defaultWorkers 返回默认工作者角色。
func defaultWorkers() []WorkerRole {
	return []WorkerRole{
		{Name: "architect", Description: "系统架构师", Specialty: "架构设计、技术选型、任务拆解"},
		{Name: "developer", Description: "开发者", Specialty: "代码实现、功能开发、Bug 修复"},
		{Name: "reviewer", Description: "代码审查者", Specialty: "代码审查、质量把控、最佳实践"},
		{Name: "tester", Description: "测试工程师", Specialty: "测试用例、质量验证、问题发现"},
	}
}

// AssignTask 分配任务给工作者。
func (s *SupervisorState) AssignTask(task, worker string) *TaskAssignment {
	assignment := &TaskAssignment{
		ID:         s.NextTaskID,
		Task:       task,
		Worker:     worker,
		Status:     "pending",
		AssignedAt: time.Now(),
	}
	s.Tasks = append(s.Tasks, *assignment)
	s.NextTaskID++
	return assignment
}

// UpdateTask 更新任务状态。
func (s *SupervisorState) UpdateTask(id int, status, result string) {
	for i := range s.Tasks {
		if s.Tasks[i].ID == id {
			s.Tasks[i].Status = status
			if result != "" {
				s.Tasks[i].Result = result
			}
			if status == "done" || status == "failed" {
				now := time.Now()
				s.Tasks[i].FinishedAt = &now
			}
			return
		}
	}
}

// NextPendingTask 返回下一个待处理任务。
func (s *SupervisorState) NextPendingTask() *TaskAssignment {
	for i := range s.Tasks {
		if s.Tasks[i].Status == "pending" {
			return &s.Tasks[i]
		}
	}
	return nil
}

// Progress 返回完成进度。
func (s *SupervisorState) Progress() (done, total int) {
	for _, t := range s.Tasks {
		if t.Status == "done" {
			done++
		}
		total++
	}
	return
}

// IsComplete 返回是否所有任务都完成。
func (s *SupervisorState) IsComplete() bool {
	for _, t := range s.Tasks {
		if t.Status != "done" && t.Status != "failed" && t.Status != "skipped" {
			return false
		}
	}
	return true
}

// Render 返回 Supervisor 状态的文本表示。
func (s *SupervisorState) Render() string {
	var sb strings.Builder
	done, total := s.Progress()
	fmt.Fprintf(&sb, "👔 Supervisor 模式: %s\n", s.Goal)
	fmt.Fprintf(&sb, "   进度: %d/%d\n\n", done, total)

	fmt.Fprintln(&sb, "工作者:")
	for _, w := range s.Workers {
		fmt.Fprintf(&sb, "  - %s (%s): %s\n", w.Name, w.Description, w.Specialty)
	}

	if len(s.Tasks) > 0 {
		fmt.Fprintln(&sb, "\n任务分配:")
		for _, t := range s.Tasks {
			mark := "[ ]"
			switch t.Status {
			case "running":
				mark = "[>]"
			case "done":
				mark = "[x]"
			case "failed":
				mark = "[!]"
			}
			fmt.Fprintf(&sb, "  %s #%d [%s] %s\n", mark, t.ID, t.Worker, t.Task)
			if t.Result != "" && (t.Status == "done" || t.Status == "failed") {
				fmt.Fprintf(&sb, "      → %s\n", truncate(t.Result, 80))
			}
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// LLMClient Supervisor 需要的 LLM 接口。
type LLMClient interface {
	Complete(ctx context.Context, messages []ChatMessage) (string, error)
}

// ChatMessage 聊天消息（复用 llm 包的通用类型）。
type ChatMessage = llm.ChatMessage

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
