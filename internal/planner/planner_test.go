package planner

import (
	"testing"
)

func TestTaskStatusConstants(t *testing.T) {
	if StatusPending != "pending" {
		t.Error("StatusPending 应为 pending")
	}
	if StatusRunning != "running" {
		t.Error("StatusRunning 应为 running")
	}
	if StatusDone != "done" {
		t.Error("StatusDone 应为 done")
	}
	if StatusFailed != "failed" {
		t.Error("StatusFailed 应为 failed")
	}
}

func TestNewPlan(t *testing.T) {
	p := NewPlan("测试目标", nil)
	if p.Goal != "测试目标" {
		t.Error("Goal 应为 测试目标")
	}
	if len(p.Tasks) != 0 {
		t.Error("初始任务数应为 0")
	}
}

func TestPlanAddTask(t *testing.T) {
	p := NewPlan("测试目标", nil)

	id1 := p.AddTask("子任务1", "描述1")
	if id1 != 1 {
		t.Errorf("第一个任务 ID = %d, 应为 1", id1)
	}
	if len(p.Tasks) != 1 {
		t.Errorf("任务数 = %d, 应为 1", len(p.Tasks))
	}

	id2 := p.AddTask("子任务2", "描述2", id1)
	if id2 != 2 {
		t.Errorf("第二个任务 ID = %d, 应为 2", id2)
	}
	if len(p.Tasks[1].DependsOn) != 1 {
		t.Error("子任务2 应有 1 个依赖")
	}
}

func TestPlanNextTask(t *testing.T) {
	p := NewPlan("测试", nil)
	id1 := p.AddTask("任务1", "")
	p.AddTask("任务2", "", id1)

	// 第一个可执行任务应该是任务1
	next := p.NextTask()
	if next == nil || next.ID != id1 {
		t.Error("第一个可执行任务应为任务1")
	}

	// 任务1 运行中，没有可执行任务
	p.UpdateTaskStatus(id1, StatusRunning)
	next = p.NextTask()
	if next != nil {
		t.Error("任务1 运行中不应有其他可执行任务")
	}

	// 任务1 完成后，任务2 可执行
	p.UpdateTaskStatus(id1, StatusDone)
	next = p.NextTask()
	if next == nil || next.ID != 2 {
		t.Error("任务1 完成后可执行任务应为任务2")
	}
}

func TestPlanProgress(t *testing.T) {
	p := NewPlan("测试", nil)
	id1 := p.AddTask("任务1", "")
	id2 := p.AddTask("任务2", "")
	p.AddTask("任务3", "")

	done, total := p.Progress()
	if done != 0 || total != 3 {
		t.Errorf("初始进度 = %d/%d, 应为 0/3", done, total)
	}

	p.UpdateTaskStatus(id1, StatusDone)
	done, total = p.Progress()
	if done != 1 || total != 3 {
		t.Errorf("进度 = %d/%d, 应为 1/3", done, total)
	}

	p.UpdateTaskStatus(id2, StatusDone)
	p.UpdateTaskStatus(3, StatusDone)
	if !p.IsComplete() {
		t.Error("所有任务完成后 IsComplete() 应为 true")
	}
}

func TestParseTasks(t *testing.T) {
	input := `- 任务一 | 描述一
- 任务二 | 描述二
- 任务三 | 描述三`
	tasks := ParseTasks(input)
	if len(tasks) != 3 {
		t.Errorf("解析到 %d 个任务, 应为 3", len(tasks))
	}
	if tasks[0].Title != "任务一" {
		t.Errorf("第一个任务标题 = %q, 应为 任务一", tasks[0].Title)
	}
	if tasks[0].Description != "描述一" {
		t.Errorf("第一个任务描述 = %q, 应为 描述一", tasks[0].Description)
	}
}

func TestPrompts(t *testing.T) {
	p := NewPlan("测试目标", nil)
	task := &Task{ID: 1, Title: "任务1", Description: "描述"}

	if r := DecomposePrompt("测试目标", "上下文"); r == "" {
		t.Error("DecomposePrompt() 不应为空")
	}
	if r := AdjustPrompt(p, task, "错误信息"); r == "" {
		t.Error("AdjustPrompt() 不应为空")
	}
	if r := ExecuteTaskPrompt(task, p); r == "" {
		t.Error("ExecuteTaskPrompt() 不应为空")
	}
	if r := SummaryPrompt(p); r == "" {
		t.Error("SummaryPrompt() 不应为空")
	}
}
