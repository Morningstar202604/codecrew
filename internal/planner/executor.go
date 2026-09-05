package planner

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// ParseTasks 解析 LLM 返回的任务列表文本。
// 格式: "- 任务标题 | 详细描述 | 依赖编号1,编号2"
func ParseTasks(text string) []Task {
	var tasks []Task
	lines := strings.Split(text, "\n")
	id := 1
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "-") {
			continue
		}
		// 去掉开头的 "- "
		line = strings.TrimPrefix(line, "-")
		line = strings.TrimSpace(line)

		parts := strings.Split(line, "|")
		if len(parts) < 1 {
			continue
		}

		title := strings.TrimSpace(parts[0])
		if title == "" {
			continue
		}

		description := ""
		if len(parts) >= 2 {
			description = strings.TrimSpace(parts[1])
		}

		dependsOn := []int{}
		if len(parts) >= 3 {
			depStr := strings.TrimSpace(parts[2])
			if depStr != "" {
				for _, d := range strings.Split(depStr, ",") {
					d = strings.TrimSpace(d)
					if depID, err := strconv.Atoi(d); err == nil {
						dependsOn = append(dependsOn, depID)
					}
				}
			}
		}

		tasks = append(tasks, Task{
			ID:          id,
			Title:       title,
			Description: description,
			Status:      StatusPending,
			DependsOn:   dependsOn,
		})
		id++
	}
	return tasks
}

// Decomposer 任务分解器，调用 LLM 将目标分解为子任务。
type Decomposer struct {
	client LLMClient
}

// NewDecomposer 创建任务分解器。
func NewDecomposer(client LLMClient) *Decomposer {
	return &Decomposer{client: client}
}

// Decompose 将目标分解为子任务计划。
func (d *Decomposer) Decompose(ctx context.Context, goal string, context string) (*Plan, error) {
	prompt := DecomposePrompt(goal, context)
	messages := []ChatMessage{
		{Role: "system", Content: "你是一个任务规划专家，擅长将复杂目标分解为可执行的子任务。"},
		{Role: "user", Content: prompt},
	}

	result, err := d.client.Complete(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("任务分解失败: %w", err)
	}

	tasks := ParseTasks(result)
	if len(tasks) == 0 {
		// 如果解析失败，创建一个默认任务
		tasks = []Task{
			{ID: 1, Title: goal, Description: "完成目标任务", Status: StatusPending},
		}
	}

	plan := NewPlan(goal, tasks)
	return plan, nil
}

// AdjustPlan 根据失败情况调整计划。
func (d *Decomposer) AdjustPlan(ctx context.Context, plan *Plan, failedTask *Task, errorMsg string) (*Plan, error) {
	prompt := AdjustPrompt(plan, failedTask, errorMsg)
	messages := []ChatMessage{
		{Role: "system", Content: "你是一个任务规划专家，擅长根据执行情况调整计划。"},
		{Role: "user", Content: prompt},
	}

	result, err := d.client.Complete(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("计划调整失败: %w", err)
	}

	tasks := ParseTasks(result)
	if len(tasks) == 0 {
		return plan, nil // 调整失败，保持原计划
	}

	newPlan := NewPlan(plan.Goal, tasks)
	return newPlan, nil
}
