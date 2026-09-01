package reasoning

import "fmt"

// ReActSystemPrompt 返回显式 ReAct 模式的 system prompt 追加部分。
//
//	instructs the model to output explicit Thought steps before actions.
func ReActSystemPrompt() string {
	return `
## 推理格式（ReAct）

在执行任务时，请遵循以下格式，让思考过程显式化：

1. **Thought**: 先思考当前状态、目标和下一步计划
2. **Action**: 决定调用哪个工具，以及参数
3. **Observation**: 工具返回结果后，观察并分析
4. 重复 1-3，直到任务完成
5. **Final Answer**: 给出最终结果

示例：
Thought: 用户想读取 main.go 的内容，我需要先确认文件是否存在。
Action: read(path="main.go")
Observation: 文件内容是...
Thought: 我已经获取了文件内容，现在可以总结给用户。
Final Answer: main.go 的内容是...

注意：
- Thought 要简洁，聚焦于"为什么做这个动作"
- 不需要每一步都写 Thought，只有关键决策点才写
- 如果直接回答用户问题，不需要 Thought，直接给出答案即可
`
}

// ReflexionPrompt 生成反思提示，让模型回顾任务执行过程并总结经验。
// task 是用户原始请求，summary 是执行过程摘要，failed 表示任务是否失败。
func ReflexionPrompt(task, summary string, failed bool, depth int) string {
	intro := "## 自我反思（Reflexion）\n\n"
	if failed {
		intro += "**本次任务失败了。** 请深入分析失败原因，总结经验教训，避免下次重复同样的错误。\n\n"
	} else {
		intro += "**本次任务已完成。** 请回顾执行过程，总结做得好的地方和可以改进的地方。\n\n"
	}

	questions := []string{
		"1. 任务目标是什么？最终是否达成？",
		"2. 执行过程中做了哪些关键决策？哪些是对的，哪些有问题？",
		"3. 遇到了哪些困难或错误？是如何解决的（或为什么没解决）？",
		"4. 如果重新做一次，你会怎么做 differently？",
		"5. 从这次任务中学到了什么可以复用的经验？",
	}

	if depth >= 2 {
		questions = append(questions,
			"6. 工具使用是否高效？有没有不必要的调用或遗漏的工具？",
			"7. 上下文管理是否合理？有没有信息丢失或冗余？",
		)
	}
	if depth >= 3 {
		questions = append(questions,
			"8. 从架构/设计角度看，这次任务暴露了什么系统性问题？",
			"9. 如何将这次的经验沉淀为可复用的工作流或检查清单？",
		)
	}

	return fmt.Sprintf("%s### 任务\n%s\n\n### 执行摘要\n%s\n\n### 反思问题\n%s\n\n请用简洁的要点回答以上问题，重点是可操作的改进建议。",
		intro, task, summary, joinQuestions(questions))
}

// FailureAnalysisPrompt 生成失败分析提示，用于工具调用失败时的即时反思。
func FailureAnalysisPrompt(task, toolName, errorMsg string) string {
	return fmt.Sprintf(`## 工具调用失败分析

工具 **%s** 调用失败，错误信息：%s

请简要分析：
1. 失败的根本原因是什么？
2. 下一步应该如何修复？
3. 下次遇到类似情况应该如何避免？

（用 2-3 句话回答，然后继续执行任务）`, toolName, errorMsg)
}

// InjectReflectionsPrompt 将历史反思注入 system prompt。
func InjectReflectionsPrompt(reflections string) string {
	if reflections == "" {
		return ""
	}
	return fmt.Sprintf("\n## 历史反思经验\n\n以下是你过去执行任务时的反思总结，请在本次任务中引以为戒：\n\n%s\n", reflections)
}

func joinQuestions(qs []string) string {
	out := ""
	for _, q := range qs {
		out += q + "\n"
	}
	return out
}
