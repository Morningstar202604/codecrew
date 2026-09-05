package planner

import "fmt"

// DecomposePrompt 生成任务分解 prompt，让 LLM 将复杂任务分解为子任务。
func DecomposePrompt(goal string, context string) string {
	return fmt.Sprintf(`## 任务分解

请将以下目标分解为可执行的子任务列表。每个子任务应该是一个独立、可验证的步骤。

### 目标
%s

### 上下文
%s

### 输出格式
请按以下格式输出，每个子任务一行：

- 任务标题 | 详细描述 | 依赖的任务编号（用逗号分隔，无依赖则留空）

### 要求
1. 子任务数量控制在 3-8 个之间
2. 每个子任务应该是原子的，可独立完成和验证
3. 合理设置依赖关系，确保执行顺序正确
4. 第一个任务不应该有依赖
5. 任务描述要具体，包含验收标准

### 示例
- 读取项目结构 | 查看 README 和目录结构，了解项目概况 | 
- 分析需求 | 根据用户需求确定需要修改的文件 | 1
- 实现功能 | 修改代码实现需求 | 2
- 运行测试 | 执行测试验证功能正确 | 3
`, goal, context)
}

// AdjustPrompt 生成计划调整 prompt，让 LLM 根据执行结果调整计划。
func AdjustPrompt(plan *Plan, failedTask *Task, errorMsg string) string {
	return fmt.Sprintf(`## 计划调整

执行计划时遇到问题，需要调整计划。

### 原始目标
%s

### 当前计划
%s

### 失败的任务
- #%d %s
- 错误信息: %s

### 要求
请分析失败原因，并给出调整后的计划。可以：
1. 将失败的任务拆分为更小的子任务
2. 添加新的前置任务来解决依赖问题
3. 跳过不必要的任务
4. 调整任务顺序

请按以下格式输出调整后的完整任务列表：
- 任务标题 | 详细描述 | 依赖的任务编号（用逗号分隔，无依赖则留空）
`, plan.Goal, plan.Render(), failedTask.ID, failedTask.Title, errorMsg)
}

// ExecuteTaskPrompt 生成执行单个任务的 prompt。
func ExecuteTaskPrompt(task *Task, plan *Plan) string {
	context := ""
	// 收集已完成任务的结果作为上下文
	for _, t := range plan.Tasks {
		if t.Status == StatusDone && t.Result != "" {
			context += fmt.Sprintf("- #%d %s: %s\n", t.ID, t.Title, t.Result)
		}
	}

	return fmt.Sprintf(`## 执行任务 #%d: %s

### 任务描述
%s

### 已完成任务的上下文
%s

### 要求
1. 专注完成当前任务，不要做任务范围外的事情
2. 使用工具完成任务（读取文件、修改代码、运行命令等）
3. 完成后简要说明做了什么，以及如何验证
4. 如果遇到无法解决的问题，说明原因并停止
`, task.ID, task.Title, task.Description, context)
}

// SummaryPrompt 生成计划完成总结 prompt。
func SummaryPrompt(plan *Plan) string {
	results := ""
	for _, t := range plan.Tasks {
		if t.Result != "" {
			results += fmt.Sprintf("- #%d %s (%s): %s\n", t.ID, t.Title, t.Status, t.Result)
		} else {
			results += fmt.Sprintf("- #%d %s (%s)\n", t.ID, t.Title, t.Status)
		}
	}

	return fmt.Sprintf(`## 计划完成总结

### 目标
%s

### 任务执行情况
%s

### 要求
请总结整个计划的执行情况：
1. 目标是否达成
2. 做了哪些主要工作
3. 遇到了什么问题，如何解决的
4. 还有什么遗留问题或后续建议
`, plan.Goal, results)
}
