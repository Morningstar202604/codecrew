package orchestration

import (
	"testing"
)

func TestSupervisorState(t *testing.T) {
	state := NewSupervisorState()

	// 空状态下 IsComplete() 返回 true 是合理的（0/0 完成）

	// 分配任务（参数顺序：task, worker）
	task1 := state.AssignTask("设计系统架构", "架构师")
	if task1.ID != 1 {
		t.Errorf("第一个任务 ID = %d, 应为 1", task1.ID)
	}
	if task1.Task != "设计系统架构" {
		t.Errorf("任务 Task = %q, 应为 设计系统架构", task1.Task)
	}
	if task1.Worker != "架构师" {
		t.Errorf("任务 Worker = %q, 应为 架构师", task1.Worker)
	}
	if task1.Status != "pending" {
		t.Errorf("任务 Status = %q, 应为 pending", task1.Status)
	}

	// 分配第二个任务
	task2 := state.AssignTask("实现功能", "开发者")
	if task2.ID != 2 {
		t.Errorf("第二个任务 ID = %d, 应为 2", task2.ID)
	}

	// 获取下一个待处理任务
	next := state.NextPendingTask()
	if next == nil || next.ID != 1 {
		t.Error("下一个待处理任务应为任务1")
	}

	// 更新任务状态
	state.UpdateTask(task1.ID, "running", "")
	next = state.NextPendingTask()
	if next == nil || next.ID != 2 {
		t.Error("任务1运行中，下一个待处理任务应为任务2")
	}

	// 完成任务
	state.UpdateTask(task1.ID, "done", "架构设计完成")
	state.UpdateTask(task2.ID, "done", "功能实现完成")

	if !state.IsComplete() {
		t.Error("所有任务完成后应返回 true")
	}

	done, total := state.Progress()
	if done != 2 || total != 2 {
		t.Errorf("进度 = %d/%d, 应为 2/2", done, total)
	}
}

func TestSupervisorRender(t *testing.T) {
	state := NewSupervisorState()
	state.AssignTask("任务1", "开发者")
	rendered := state.Render()
	if rendered == "" {
		t.Error("Render() 不应为空")
	}
}

func TestHITLState(t *testing.T) {
	state := NewHITLState()

	if state.HasPending() {
		t.Error("初始状态不应有待审批项")
	}

	// 请求审批
	pending := state.RequestApproval("write", "写入文件 main.go", "path=main.go")
	if pending.ID != 1 {
		t.Errorf("第一个审批 ID = %d, 应为 1", pending.ID)
	}
	if pending.Type != "write" {
		t.Errorf("审批 Type = %q, 应为 write", pending.Type)
	}
	if pending.Status != "pending" {
		t.Errorf("审批 Status = %q, 应为 pending", pending.Status)
	}

	if !state.HasPending() {
		t.Error("请求审批后应返回 true")
	}

	list := state.ListPending()
	if len(list) != 1 {
		t.Errorf("待审批项数 = %d, 应为 1", len(list))
	}

	// 获取待审批项
	got := state.GetPending(1)
	if got == nil {
		t.Fatal("GetPending(1) 不应返回 nil")
	}
	if got.ID != 1 {
		t.Errorf("GetPending(1).ID = %d, 应为 1", got.ID)
	}

	// 批准
	approved := state.Approve(1)
	if !approved {
		t.Error("Approve(1) 应返回 true")
	}

	if state.HasPending() {
		t.Error("批准后不应有待审批项")
	}

	// 拒绝
	pending2 := state.RequestApproval("delete", "删除文件", "")
	denied := state.Deny(pending2.ID)
	if !denied {
		t.Error("Deny() 应返回 true")
	}
}

func TestHITLAutoApprove(t *testing.T) {
	state := NewHITLState()
	state.SetAutoApprove("read", true)

	// 自动批准的类型应该立即被批准
	pending := state.RequestApproval("read", "读取文件", "")
	if pending.Status != "approved" {
		t.Errorf("自动批准类型的 Status = %q, 应为 approved", pending.Status)
	}
}

func TestWorkerRole(t *testing.T) {
	workers := defaultWorkers()
	if len(workers) == 0 {
		t.Error("默认工作者列表不应为空")
	}
}
