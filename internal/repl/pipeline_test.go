package repl

import (
	"strings"
	"testing"

	"codecrew/internal/config"
)

func TestRunPipeline_Basic(t *testing.T) {
	// 四个阶段各返回一段文本，无工具调用
	mock := newMockLLM(t,
		sseText("架构师输出：任务拆解为 3 步"),
		sseText("开发输出：已完成实现"),
		sseText("审查输出：通过审查，无问题"),
		sseText("测试输出：全部测试通过"),
	)
	app, out, _ := newTestREPL(t, mock.server.URL, nil)

	err := app.RunPipeline("实现用户登录功能")
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}

	output := out.String()
	// 验证四个阶段都执行了
	if !strings.Contains(output, "架构师拆解任务") {
		t.Error("output should mention architect stage")
	}
	if !strings.Contains(output, "开发工程师实现") {
		t.Error("output should mention developer stage")
	}
	if !strings.Contains(output, "代码审查") {
		t.Error("output should mention reviewer stage")
	}
	if !strings.Contains(output, "测试验证") {
		t.Error("output should mention tester stage")
	}
	// 验证结果写入了主对话历史
	history := app.History()
	found := false
	for _, m := range history {
		if m.Role == "assistant" && strings.Contains(m.Content, "流水线执行结果") {
			found = true
			if !strings.Contains(m.Content, "架构师输出") {
				t.Error("pipeline result should contain architect output")
			}
			if !strings.Contains(m.Content, "全部测试通过") {
				t.Error("pipeline result should contain tester output")
			}
		}
	}
	if !found {
		t.Error("pipeline result should be in history")
	}
	// 验证 mock 被调用了 4 次（每个阶段一次）
	reqs := mock.Requests()
	if len(reqs) != 4 {
		t.Errorf("expected 4 LLM calls (one per stage), got %d", len(reqs))
	}
}

func TestRunPipeline_EmptyTask(t *testing.T) {
	mock := newMockLLM(t, sseText("x"))
	app, _, _ := newTestREPL(t, mock.server.URL, nil)

	err := app.RunPipeline("   ")
	if err == nil {
		t.Error("expected error for empty task")
	}
}

func TestRunPipeline_NoModel(t *testing.T) {
	app, _, _ := newTestREPL(t, "http://localhost:1", func(cfg *config.Config) {
		cfg.Model = ""
		cfg.Providers = nil
	})
	app.client = nil

	err := app.RunPipeline("test")
	if err == nil {
		t.Error("expected error when no model configured")
	}
}

func TestRunPipeline_MissingRole(t *testing.T) {
	// 用一个没有 architect 角色的配置（通过自定义 roles 目录）
	// 这里简化：直接验证角色检查逻辑
	mock := newMockLLM(t, sseText("x"))
	app, _, _ := newTestREPL(t, mock.server.URL, nil)

	// 临时移除 architect 角色
	originalRoles := app.roles
	app.roles = nil
	defer func() { app.roles = originalRoles }()

	err := app.RunPipeline("test")
	if err == nil {
		t.Error("expected error when required role is missing")
	}
	if !strings.Contains(err.Error(), "architect") {
		t.Errorf("error should mention missing architect role, got %v", err)
	}
}

func TestParseRoundtableArgs(t *testing.T) {
	cases := []struct {
		input      string
		wantTopic  string
		wantRounds int
	}{
		{"", "", 0},
		{"讨论微服务架构", "讨论微服务架构", 0},
		{"讨论微服务架构 3", "讨论微服务架构", 3},
		{"  话题  5  ", "话题", 5},    // 多余空格被 Fields 规范化
		{"话题 不是数字", "话题 不是数字", 0}, // 末尾不是数字
	}
	for _, c := range cases {
		topic, rounds := parseRoundtableArgs(c.input)
		if topic != c.wantTopic || rounds != c.wantRounds {
			t.Errorf("parseRoundtableArgs(%q) = (%q, %d), want (%q, %d)",
				c.input, topic, rounds, c.wantTopic, c.wantRounds)
		}
	}
}
