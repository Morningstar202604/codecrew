package verify

import (
	"fmt"
	"strings"
)

// RepairRound 一轮修复的结果。
type RepairRound struct {
	Round    int    `json:"round"`
	Fixed    bool   `json:"fixed"`
	Summary  string `json:"summary"`
	ErrorOut string `json:"error_output,omitempty"`
}

// RepairResult 修复循环的最终结果。
type RepairResult struct {
	Fixed       bool          `json:"fixed"`
	Rounds      []RepairRound `json:"rounds"`
	FinalResult *Result       `json:"final_result,omitempty"`
	MaxRounds   int           `json:"max_rounds"`
}

// Summary 返回修复结果摘要。
func (r RepairResult) Summary() string {
	if r.Fixed {
		return fmt.Sprintf("✓ 经过 %d 轮修复，全部验证通过", len(r.Rounds))
	}
	return fmt.Sprintf("✗ 经过 %d 轮修复仍未通过（最大 %d 轮）", len(r.Rounds), r.MaxRounds)
}

// BuildRepairPrompt 构造修复 prompt，包含错误信息和修复指导。
func BuildRepairPrompt(errors string, round int, maxRounds int) string {
	var sb strings.Builder
	sb.WriteString("## 代码验证失败，需要修复\n\n")
	sb.WriteString("以下是验证失败的错误输出：\n\n")
	sb.WriteString("```\n")
	sb.WriteString(errors)
	sb.WriteString("\n```\n\n")
	sb.WriteString(fmt.Sprintf("这是第 %d/%d 轮修复。请分析错误原因，使用 write 或 edit 工具修复代码。\n", round, maxRounds))
	sb.WriteString("修复完成后，请简要说明修改了什么。\n")
	sb.WriteString("注意：只修复验证失败相关的问题，不要做无关的重构。")
	return sb.String()
}

// BuildVerifyPrompt 构造验证提示，让模型知道验证结果。
func BuildVerifyPrompt(result Result) string {
	if result.Passed {
		return fmt.Sprintf("## 验证通过\n\n%s", result.Summary())
	}
	var sb strings.Builder
	sb.WriteString("## 验证失败\n\n")
	sb.WriteString(result.Summary())
	sb.WriteString("\n\n失败详情：\n\n")
	for _, c := range result.Commands {
		if !c.Passed {
			fmt.Fprintf(&sb, "### %s\n\n```\n%s\n```\n\n", c.Command, c.Output)
		}
	}
	return sb.String()
}

// ExtractErrorSummary 从错误输出中提取关键错误摘要（前 N 行）。
func ExtractErrorSummary(output string, maxLines int) string {
	lines := strings.Split(output, "\n")
	if len(lines) <= maxLines {
		return output
	}
	return strings.Join(lines[:maxLines], "\n") + fmt.Sprintf("\n... (共 %d 行，已截断)", len(lines))
}
