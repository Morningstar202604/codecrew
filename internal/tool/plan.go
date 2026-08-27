package tool

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Task 是一条计划条目。
type Task struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"` // pending / doing / done / blocked
}

// PlanTool 让模型把大任务拆成可跟踪的条目，纯内存状态，供 REPL 侧展示。
type PlanTool struct {
	mu       sync.Mutex
	tasks    []Task
	nextID   int
	Listener func([]Task) // 计划变化后回调，用于终端渲染
}

func NewPlanTool() *PlanTool { return &PlanTool{nextID: 1} }

func (t *PlanTool) Name() string { return "plan" }
func (t *PlanTool) Description() string {
	return "维护任务计划：action=add/update/list/done/clear。复杂任务先拆计划，再逐项推进"
}

func (t *PlanTool) Schema() map[string]any {
	return objectSchema(map[string]any{
		"action": map[string]any{
			"type":        "string",
			"enum":        []string{"add", "update", "list", "done", "clear"},
			"description": "add 新增条目；update 改状态；done 标记完成；list 查看；clear 清空",
		},
		"title":  stringSchema("action=add 时的任务标题"),
		"id":     integerSchema("action=update/done 时的条目 ID"),
		"status": map[string]any{"type": "string", "enum": []string{"pending", "doing", "done", "blocked"}, "description": "action=update 时的新状态"},
		"note":   stringSchema("可选补充说明，会附在条目后"),
	}, []string{"action"})
}

func (t *PlanTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	action, err := getString(args, "action")
	if err != nil {
		return "", err
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	switch action {
	case "add":
		title, err := getString(args, "title")
		if err != nil {
			return "", err
		}
		if note, ok := args["note"].(string); ok && note != "" {
			title += " —— " + note
		}
		t.tasks = append(t.tasks, Task{ID: t.nextID, Title: title, Status: "pending"})
		added := fmt.Sprintf("已加入计划 #%d: %s", t.nextID, title)
		t.nextID++
		t.notify()
		return added, nil
	case "update":
		id := getInt(args, "id", 0)
		status, err := getString(args, "status")
		if err != nil {
			return "", err
		}
		for i := range t.tasks {
			if t.tasks[i].ID == id {
				t.tasks[i].Status = status
				t.notify()
				return fmt.Sprintf("#%d → %s", id, status), nil
			}
		}
		return "", fmt.Errorf("找不到计划条目 #%d", id)
	case "done":
		id := getInt(args, "id", 0)
		for i := range t.tasks {
			if t.tasks[i].ID == id {
				t.tasks[i].Status = "done"
				t.notify()
				return fmt.Sprintf("#%d 已完成: %s", id, t.tasks[i].Title), nil
			}
		}
		return "", fmt.Errorf("找不到计划条目 #%d", id)
	case "clear":
		t.tasks = nil
		t.notify()
		return "计划已清空", nil
	case "list", "":
		return t.render(), nil
	default:
		return "", fmt.Errorf("未知 action %q，可用: add/update/list/done/clear", action)
	}
}

// Tasks 返回当前计划快照。
func (t *PlanTool) Tasks() []Task {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Task, len(t.tasks))
	copy(out, t.tasks)
	return out
}

// SetTasks 用于会话恢复时回填计划。
func (t *PlanTool) SetTasks(tasks []Task) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tasks = tasks
	for _, task := range tasks {
		if task.ID >= t.nextID {
			t.nextID = task.ID + 1
		}
	}
	t.notify()
}

func (t *PlanTool) notify() {
	if t.Listener != nil {
		t.Listener(t.tasks)
	}
}

func (t *PlanTool) render() string {
	if len(t.tasks) == 0 {
		return "当前没有计划条目"
	}
	var done int
	var sb strings.Builder
	sb.WriteString("任务计划:\n")
	for _, task := range t.tasks {
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
		fmt.Fprintf(&sb, "%s #%d %s", mark, task.ID, task.Title)
		if task.Status != "pending" && task.Status != "done" {
			fmt.Fprintf(&sb, " (%s)", task.Status)
		}
		sb.WriteString("\n")
	}
	fmt.Fprintf(&sb, "进度 %d/%d", done, len(t.tasks))
	return strings.TrimRight(sb.String(), "\n")
}
