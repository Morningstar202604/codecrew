// Package eval 实现 Agent 能力评估框架。
//
// 支持定义测试用例、运行评估、生成报告，用于量化 Agent 的能力和改进效果。
package eval

import (
	"codecrew/internal/llm"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TestCase 测试用例。
type TestCase struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Category    string   `json:"category"` // code_generation / debugging / refactoring / planning / conversation
	Description string   `json:"description"`
	Input       string   `json:"input"`
	Expected    string   `json:"expected,omitempty"`     // 期望输出（用于精确匹配）
	Keywords    []string `json:"keywords,omitempty"`     // 期望包含的关键词
	MaxScore    int      `json:"max_score"`              // 满分
	TimeoutSecs int      `json:"timeout_secs,omitempty"` // 超时时间
}

// TestResult 测试结果。
type TestResult struct {
	TestCaseID string        `json:"test_case_id"`
	Name       string        `json:"name"`
	Category   string        `json:"category"`
	Passed     bool          `json:"passed"`
	Score      int           `json:"score"`
	MaxScore   int           `json:"max_score"`
	Output     string        `json:"output,omitempty"`
	Error      string        `json:"error,omitempty"`
	Duration   time.Duration `json:"duration"`
	StartedAt  time.Time     `json:"started_at"`
	FinishedAt time.Time     `json:"finished_at"`
}

// EvalReport 评估报告。
type EvalReport struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	StartedAt  time.Time     `json:"started_at"`
	FinishedAt time.Time     `json:"finished_at"`
	Duration   time.Duration `json:"duration"`
	Total      int           `json:"total"`
	Passed     int           `json:"passed"`
	Failed     int           `json:"failed"`
	Errors     int           `json:"errors"`
	PassRate   float64       `json:"pass_rate"`
	TotalScore int           `json:"total_score"`
	MaxScore   int           `json:"max_score"`
	AvgScore   float64       `json:"avg_score"`
	Results    []TestResult  `json:"results"`
}

// LLMClient 评估需要的 LLM 接口。
type LLMClient interface {
	Complete(ctx context.Context, messages []ChatMessage) (string, error)
}

// ChatMessage 聊天消息（复用 llm 包的通用类型）。
type ChatMessage = llm.ChatMessage

// Harness 评估框架。
type Harness struct {
	client   LLMClient
	storeDir string
}

// NewHarness 创建评估框架。
func NewHarness(client LLMClient) *Harness {
	home, _ := os.UserHomeDir()
	storeDir := filepath.Join(home, ".codecrew", "eval")
	os.MkdirAll(storeDir, 0o755)
	return &Harness{client: client, storeDir: storeDir}
}

// DefaultTestCases 返回默认测试用例集。
func DefaultTestCases() []TestCase {
	return []TestCase{
		{
			ID: "cg_001", Name: "简单函数生成", Category: "code_generation",
			Description: "生成一个计算斐波那契数列的函数",
			Input:       "用 Go 写一个计算斐波那契数列第 n 项的函数",
			Keywords:    []string{"func", "fib", "n"},
			MaxScore:    10, TimeoutSecs: 60,
		},
		{
			ID: "cg_002", Name: "数据结构实现", Category: "code_generation",
			Description: "实现一个简单的栈数据结构",
			Input:       "用 Go 实现一个泛型栈，支持 Push/Pop/Peek/IsEmpty",
			Keywords:    []string{"Stack", "Push", "Pop", "Peek"},
			MaxScore:    10, TimeoutSecs: 60,
		},
		{
			ID: "db_001", Name: "简单 Bug 修复", Category: "debugging",
			Description: "修复一个空指针引用的 Bug",
			Input:       "下面的代码有什么问题？如何修复？\n\nfunc getName(u *User) string {\n    return u.Name\n}",
			Keywords:    []string{"nil", "检查", "修复"},
			MaxScore:    10, TimeoutSecs: 60,
		},
		{
			ID: "pl_001", Name: "任务拆解", Category: "planning",
			Description: "将复杂任务拆解为子任务",
			Input:       "如何为一个 Web 应用添加用户认证功能？请列出步骤",
			Keywords:    []string{"步骤", "注册", "登录", "验证"},
			MaxScore:    10, TimeoutSecs: 60,
		},
		{
			ID: "cv_001", Name: "代码审查", Category: "conversation",
			Description: "对代码进行审查并给出建议",
			Input:       "请审查这段代码并给出改进建议：\n\nfunc sum(nums []int) int {\n    total := 0\n    for i := 0; i < len(nums); i++ {\n        total += nums[i]\n    }\n    return total\n}",
			Keywords:    []string{"建议", "改进", "range"},
			MaxScore:    10, TimeoutSecs: 60,
		},
	}
}

// Run 运行评估。
func (h *Harness) Run(ctx context.Context, name string, testCases []TestCase) *EvalReport {
	if len(testCases) == 0 {
		testCases = DefaultTestCases()
	}

	report := &EvalReport{
		ID:        fmt.Sprintf("eval-%d", time.Now().Unix()),
		Name:      name,
		StartedAt: time.Now(),
		Total:     len(testCases),
		Results:   make([]TestResult, 0, len(testCases)),
	}

	for _, tc := range testCases {
		result := h.runTestCase(ctx, tc)
		report.Results = append(report.Results, result)
		report.MaxScore += tc.MaxScore
		report.TotalScore += result.Score
		if result.Passed {
			report.Passed++
		} else if result.Error != "" {
			report.Errors++
		} else {
			report.Failed++
		}
	}

	report.FinishedAt = time.Now()
	report.Duration = report.FinishedAt.Sub(report.StartedAt)
	if report.Total > 0 {
		report.PassRate = float64(report.Passed) / float64(report.Total) * 100
		report.AvgScore = float64(report.TotalScore) / float64(report.Total)
	}

	// 保存报告
	h.saveReport(report)

	return report
}

// runTestCase 运行单个测试用例。
func (h *Harness) runTestCase(ctx context.Context, tc TestCase) TestResult {
	result := TestResult{
		TestCaseID: tc.ID,
		Name:       tc.Name,
		Category:   tc.Category,
		MaxScore:   tc.MaxScore,
		StartedAt:  time.Now(),
	}

	timeout := time.Duration(tc.TimeoutSecs) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	messages := []ChatMessage{
		{Role: "system", Content: "你是一个专业的编程助手，请准确回答用户的问题。"},
		{Role: "user", Content: tc.Input},
	}

	output, err := h.client.Complete(ctx, messages)
	result.FinishedAt = time.Now()
	result.Duration = result.FinishedAt.Sub(result.StartedAt)

	if err != nil {
		result.Error = err.Error()
		result.Score = 0
		return result
	}

	result.Output = output

	// 评分
	score, passed := h.scoreResult(tc, output)
	result.Score = score
	result.Passed = passed

	return result
}

// scoreResult 对测试结果进行评分。
func (h *Harness) scoreResult(tc TestCase, output string) (int, bool) {
	if output == "" {
		return 0, false
	}

	score := 0
	outputLower := strings.ToLower(output)

	// 关键词匹配
	keywordHits := 0
	for _, kw := range tc.Keywords {
		if strings.Contains(outputLower, strings.ToLower(kw)) {
			keywordHits++
		}
	}
	if len(tc.Keywords) > 0 {
		keywordScore := keywordHits * tc.MaxScore / len(tc.Keywords)
		score += keywordScore
	}

	// 精确匹配（如果有期望输出）
	if tc.Expected != "" && strings.Contains(outputLower, strings.ToLower(tc.Expected)) {
		score = tc.MaxScore
	}

	// 输出长度加分（有实质内容）
	if len(output) > 50 {
		score += 1
	}
	if score > tc.MaxScore {
		score = tc.MaxScore
	}

	passed := score >= tc.MaxScore/2 // 达到一半分数算通过
	return score, passed
}

// saveReport 保存评估报告。
func (h *Harness) saveReport(report *EvalReport) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(filepath.Join(h.storeDir, report.ID+".json"), data, 0o644)
}

// ListReports 列出历史评估报告。
func (h *Harness) ListReports() ([]EvalReport, error) {
	files, err := os.ReadDir(h.storeDir)
	if err != nil {
		return nil, err
	}

	var reports []EvalReport
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(h.storeDir, f.Name()))
		if err != nil {
			continue
		}
		var report EvalReport
		if err := json.Unmarshal(data, &report); err == nil {
			reports = append(reports, report)
		}
	}

	// 按时间倒序
	for i := 0; i < len(reports); i++ {
		for j := i + 1; j < len(reports); j++ {
			if reports[j].StartedAt.After(reports[i].StartedAt) {
				reports[i], reports[j] = reports[j], reports[i]
			}
		}
	}

	return reports, nil
}

// RenderReport 格式化评估报告。
func RenderReport(report *EvalReport) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "📊 评估报告: %s\n", report.Name)
	fmt.Fprintf(&sb, "   时间: %s\n", report.StartedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&sb, "   耗时: %s\n\n", report.Duration.Round(time.Millisecond))

	fmt.Fprintf(&sb, "总用例: %d | 通过: %d | 失败: %d | 错误: %d\n",
		report.Total, report.Passed, report.Failed, report.Errors)
	fmt.Fprintf(&sb, "通过率: %.1f%% | 得分: %d/%d (%.1f)\n\n",
		report.PassRate, report.TotalScore, report.MaxScore, report.AvgScore)

	fmt.Fprintln(&sb, "详细结果:")
	for _, r := range report.Results {
		status := "✓"
		if !r.Passed {
			status = "✗"
		}
		if r.Error != "" {
			status = "⚠"
		}
		fmt.Fprintf(&sb, "  %s [%s] %s: %d/%d (%s)\n",
			status, r.Category, r.Name, r.Score, r.MaxScore, r.Duration.Round(time.Millisecond))
		if r.Error != "" {
			fmt.Fprintf(&sb, "      错误: %s\n", truncate(r.Error, 100))
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
