package eval

import (
	"context"
	"testing"
)

// mockLLMClient 是测试用的 mock LLM 客户端。
type mockLLMClient struct {
	response string
	err      error
}

func (m *mockLLMClient) Complete(ctx context.Context, messages []ChatMessage) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

func TestDefaultTestCases(t *testing.T) {
	cases := DefaultTestCases()
	if len(cases) == 0 {
		t.Error("默认测试用例不应为空")
	}

	// 检查每个测试用例的必要字段
	for i, tc := range cases {
		if tc.ID == "" {
			t.Errorf("测试用例 %d 的 ID 不应为空", i)
		}
		if tc.Name == "" {
			t.Errorf("测试用例 %d 的 Name 不应为空", i)
		}
		if tc.Input == "" {
			t.Errorf("测试用例 %d 的 Input 不应为空", i)
		}
		if tc.MaxScore <= 0 {
			t.Errorf("测试用例 %d 的 MaxScore 应 > 0", i)
		}
	}
}

func TestTestCaseCategories(t *testing.T) {
	cases := DefaultTestCases()
	categories := make(map[string]bool)
	for _, tc := range cases {
		if tc.Category != "" {
			categories[tc.Category] = true
		}
	}

	// 应该至少包含几个主要分类
	expectedCategories := []string{"code_generation", "debugging", "planning", "conversation"}
	foundCount := 0
	for _, cat := range expectedCategories {
		if categories[cat] {
			foundCount++
		}
	}
	if foundCount < 2 {
		t.Errorf("应至少包含 2 个主要分类，实际找到 %d", foundCount)
	}
}

func TestEvalReportStruct(t *testing.T) {
	report := &EvalReport{
		ID:         "test-report",
		Name:       "测试报告",
		Total:      10,
		Passed:     8,
		Failed:     2,
		TotalScore: 80,
		MaxScore:   100,
	}

	if report.PassRate != 0 {
		// PassRate 应该在 Run 中计算，这里只是检查结构体
	}

	if report.Total != 10 {
		t.Errorf("Total = %d, 应为 10", report.Total)
	}
	if report.Passed != 8 {
		t.Errorf("Passed = %d, 应为 8", report.Passed)
	}
}

func TestRenderReport(t *testing.T) {
	report := &EvalReport{
		ID:         "test-report",
		Name:       "测试评估",
		Total:      3,
		Passed:     2,
		Failed:     1,
		TotalScore: 80,
		MaxScore:   100,
		AvgScore:   26.7,
		Results: []TestResult{
			{TestCaseID: "test1", Name: "测试1", Passed: true, Score: 30, MaxScore: 30},
			{TestCaseID: "test2", Name: "测试2", Passed: true, Score: 30, MaxScore: 40},
			{TestCaseID: "test3", Name: "测试3", Passed: false, Score: 20, MaxScore: 30},
		},
	}

	rendered := RenderReport(report)
	if rendered == "" {
		t.Error("RenderReport() 不应为空")
	}

	// 应该包含关键信息
	contains := []string{"测试评估", "3", "2", "80", "100"}
	for _, s := range contains {
		if !containsStr(rendered, s) {
			t.Errorf("RenderReport() 应包含 %q", s)
		}
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStrHelper(s, substr))
}

func containsStrHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestHarnessRun(t *testing.T) {
	// 创建一个返回包含关键词的 mock 客户端
	mock := &mockLLMClient{
		response: "这是一个包含 func 和 return 的测试输出，用于验证评估功能。",
	}

	harness := NewHarness(mock)

	// 创建简单的测试用例
	testCases := []TestCase{
		{
			ID:       "test1",
			Name:     "简单测试",
			Category: "conversation",
			Input:    "测试输入",
			Keywords: []string{"func", "return"},
			MaxScore: 100,
		},
	}

	report := harness.Run(context.Background(), "测试运行", testCases)
	if report == nil {
		t.Fatal("Run() 不应返回 nil")
	}

	if report.Total != 1 {
		t.Errorf("Total = %d, 应为 1", report.Total)
	}

	// mock 响应包含关键词，应该通过
	if report.Passed != 1 {
		t.Errorf("Passed = %d, 应为 1 (mock 响应包含关键词)", report.Passed)
	}
}

func TestHarnessRunFailure(t *testing.T) {
	// 创建一个不包含关键词的 mock 客户端
	mock := &mockLLMClient{
		response: "这是一个不包含任何关键词的输出。",
	}

	harness := NewHarness(mock)

	testCases := []TestCase{
		{
			ID:       "test1",
			Name:     "关键词测试",
			Category: "conversation",
			Input:    "测试输入",
			Keywords: []string{"不存在的关键词"},
			MaxScore: 100,
		},
	}

	report := harness.Run(context.Background(), "失败测试", testCases)
	if report == nil {
		t.Fatal("Run() 不应返回 nil")
	}

	if report.Failed != 1 {
		t.Errorf("Failed = %d, 应为 1 (mock 响应不包含关键词)", report.Failed)
	}
}

func TestHarnessListReports(t *testing.T) {
	mock := &mockLLMClient{response: "测试"}
	harness := NewHarness(mock)

	// ListReports 可能返回空列表或错误，不应 panic
	reports, err := harness.ListReports()
	_ = reports
	_ = err
	// 只要不 panic 就通过
}
