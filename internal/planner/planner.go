// Package planner 实现 Plan-and-Execute 规划与执行分离。
//
// 将复杂任务先分解为有依赖关系的子任务（DAG），再按顺序逐步执行，
// 执行中可根据结果动态调整计划，提高复杂任务的完成率。
package planner

import (
	"codecrew/internal/llm"
	"context"
	"fmt"
	"strings"
	"time"
)

// TaskStatus 任务状态。
type TaskStatus string

const (
	StatusPending TaskStatus = "pending" // 待执行
	StatusRunning TaskStatus = "running" // 执行中
	StatusDone    TaskStatus = "done"    // 已完成
	StatusFailed  TaskStatus = "failed"  // 失败
	StatusSkipped TaskStatus = "skipped" // 已跳过
)

// Task 是计划中的一个子任务。
type Task struct {
	ID          int        `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Status      TaskStatus `json:"status"`
	DependsOn   []int      `json:"depends_on,omitempty"` // 依赖的任务 ID
	Result      string     `json:"result,omitempty"`     // 执行结果摘要
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

// Plan 是一个完整的执行计划。
type Plan struct {
	ID        string    `json:"id"`
	Goal      string    `json:"goal"` // 原始目标
	Tasks     []Task    `json:"tasks"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NewPlan 创建新计划。
func NewPlan(goal string, tasks []Task) *Plan {
	now := time.Now()
	return &Plan{
		ID:        fmt.Sprintf("plan-%d", now.Unix()),
		Goal:      goal,
		Tasks:     tasks,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Progress 返回完成进度（已完成/总数）。
func (p *Plan) Progress() (done, total int) {
	for _, t := range p.Tasks {
		if t.Status == StatusDone || t.Status == StatusSkipped {
			done++
		}
		total++
	}
	return
}

// IsComplete 返回计划是否全部完成。
func (p *Plan) IsComplete() bool {
	for _, t := range p.Tasks {
		if t.Status != StatusDone && t.Status != StatusSkipped && t.Status != StatusFailed {
			return false
		}
	}
	return true
}

// HasFailed 返回是否有任务失败。
func (p *Plan) HasFailed() bool {
	for _, t := range p.Tasks {
		if t.Status == StatusFailed {
			return true
		}
	}
	return false
}

// NextTask 返回下一个可执行的任务（依赖已满足且状态为 pending）。
func (p *Plan) NextTask() *Task {
	for i := range p.Tasks {
		if p.Tasks[i].Status != StatusPending {
			continue
		}
		// 检查依赖是否都已完成
		if p.dependenciesSatisfied(p.Tasks[i]) {
			return &p.Tasks[i]
		}
	}
	return nil
}

// dependenciesSatisfied 检查任务的依赖是否都已完成。
func (p *Plan) dependenciesSatisfied(task Task) bool {
	for _, depID := range task.DependsOn {
		dep := p.GetTask(depID)
		if dep == nil {
			return false
		}
		if dep.Status != StatusDone && dep.Status != StatusSkipped {
			return false
		}
	}
	return true
}

// GetTask 按 ID 获取任务。
func (p *Plan) GetTask(id int) *Task {
	for i := range p.Tasks {
		if p.Tasks[i].ID == id {
			return &p.Tasks[i]
		}
	}
	return nil
}

// UpdateTaskStatus 更新任务状态。
func (p *Plan) UpdateTaskStatus(id int, status TaskStatus, result ...string) {
	task := p.GetTask(id)
	if task == nil {
		return
	}
	now := time.Now()
	task.Status = status
	if status == StatusRunning && task.StartedAt == nil {
		task.StartedAt = &now
	}
	if status == StatusDone || status == StatusFailed || status == StatusSkipped {
		task.FinishedAt = &now
	}
	if len(result) > 0 {
		task.Result = result[0]
	}
	p.UpdatedAt = now
}

// AddTask 添加新任务。
func (p *Plan) AddTask(title, description string, dependsOn ...int) int {
	maxID := 0
	for _, t := range p.Tasks {
		if t.ID > maxID {
			maxID = t.ID
		}
	}
	newID := maxID + 1
	p.Tasks = append(p.Tasks, Task{
		ID:          newID,
		Title:       title,
		Description: description,
		Status:      StatusPending,
		DependsOn:   dependsOn,
	})
	p.UpdatedAt = time.Now()
	return newID
}

// Render 返回计划的文本表示。
func (p *Plan) Render() string {
	var sb strings.Builder
	done, total := p.Progress()
	fmt.Fprintf(&sb, "📋 执行计划: %s\n", p.Goal)
	fmt.Fprintf(&sb, "   进度: %d/%d\n\n", done, total)
	for _, t := range p.Tasks {
		mark := "[ ]"
		switch t.Status {
		case StatusRunning:
			mark = "[>]"
		case StatusDone:
			mark = "[x]"
		case StatusFailed:
			mark = "[!]"
		case StatusSkipped:
			mark = "[-]"
		}
		deps := ""
		if len(t.DependsOn) > 0 {
			depStrs := make([]string, len(t.DependsOn))
			for i, d := range t.DependsOn {
				depStrs[i] = fmt.Sprintf("#%d", d)
			}
			deps = fmt.Sprintf(" (依赖: %s)", strings.Join(depStrs, ", "))
		}
		fmt.Fprintf(&sb, "  %s #%d %s%s\n", mark, t.ID, t.Title, deps)
		if t.Description != "" && t.Status == StatusPending {
			fmt.Fprintf(&sb, "      %s\n", t.Description)
		}
		if t.Result != "" && (t.Status == StatusDone || t.Status == StatusFailed) {
			fmt.Fprintf(&sb, "      → %s\n", t.Result)
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// LLMClient 是规划器需要的 LLM 接口。
type LLMClient interface {
	Complete(ctx context.Context, messages []ChatMessage) (string, error)
}

// ChatMessage 聊天消息（复用 llm 包的通用类型）。
type ChatMessage = llm.ChatMessage
