package repl

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"codecrew/internal/config"
	"codecrew/internal/llm"
)

// mockLLMComplete 是一个同时支持流式和非流式的假 LLM 服务，用于圆桌测试。
type mockLLMComplete struct {
	mu       sync.Mutex
	server   *httptest.Server
	replies  []string
	calls    int
	requests []recordedRequest
}

func newMockLLMComplete(t *testing.T, replies ...string) *mockLLMComplete {
	t.Helper()
	m := &mockLLMComplete{replies: replies}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req recordedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("mock 收到坏请求: %v", err)
			w.WriteHeader(400)
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		m.requests = append(m.requests, req)
		idx := m.calls
		if idx >= len(m.replies) {
			idx = len(m.replies) - 1
		}
		m.calls++
		reply := m.replies[idx]

		if req.Stream {
			// 流式：返回 SSE
			w.Header().Set("Content-Type", "text/event-stream")
			frames := []string{
				`{"choices":[{"delta":{"role":"assistant","content":""}}]}`,
				fmt.Sprintf(`{"choices":[{"delta":{"content":%q}}]}`, reply),
				`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
				`{"usage":{"prompt_tokens":10,"completion_tokens":5,"total":15}}`,
			}
			for _, f := range frames {
				fmt.Fprintf(w, "data: %s\n\n", f)
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
		} else {
			// 非流式：返回标准 JSON
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]string{
							"role":    "assistant",
							"content": reply,
						},
						"finish_reason": "stop",
					},
				},
				"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	t.Cleanup(m.server.Close)
	return m
}

func (m *mockLLMComplete) Requests() []recordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]recordedRequest, len(m.requests))
	copy(out, m.requests)
	return out
}

func TestRunRoundtable_Basic(t *testing.T) {
	// 圆桌有 2 轮，每轮 3 个角色发言 = 6 次 Complete 调用
	// 最后主持人总结 = 1 次 Complete 调用
	// 共 7 次非流式调用
	mock := newMockLLMComplete(t,
		"架构师观点：应该用微服务",
		"开发者观点：微服务太复杂，单体更好",
		"审查者观点：需要考虑团队规模",
		"架构师回应：模块化单体是折中",
		"开发者回应：同意模块化单体",
		"审查者回应：模块化单体可行",
		"共识：采用模块化单体。分歧：无。建议：先做模块化单体，未来按需拆分。",
	)
	app, out, _ := newTestREPL(t, mock.server.URL, nil)

	err := app.RunRoundtable("讨论系统架构选择", 2)
	if err != nil {
		t.Fatalf("roundtable failed: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "圆桌讨论") {
		t.Error("output should mention roundtable")
	}
	if !strings.Contains(output, "第 1/2 轮") {
		t.Error("output should show round 1")
	}
	if !strings.Contains(output, "第 2/2 轮") {
		t.Error("output should show round 2")
	}
	if !strings.Contains(output, "主持人总结") {
		t.Error("output should show moderator summary")
	}

	// 验证结果写入历史
	history := app.History()
	found := false
	for _, m := range history {
		if m.Role == "assistant" && strings.Contains(m.Content, "圆桌讨论结果") {
			found = true
			if !strings.Contains(m.Content, "共识") {
				t.Error("result should contain consensus")
			}
		}
	}
	if !found {
		t.Error("roundtable result should be in history")
	}

	// 验证调用次数：2轮 × 3角色 + 1总结 = 7
	reqs := mock.Requests()
	if len(reqs) != 7 {
		t.Errorf("expected 7 LLM calls, got %d", len(reqs))
	}
}

func TestRunRoundtable_DefaultRounds(t *testing.T) {
	// 默认 2 轮
	mock := newMockLLMComplete(t,
		"a1", "d1", "r1",
		"a2", "d2", "r2",
		"summary",
	)
	app, _, _ := newTestREPL(t, mock.server.URL, nil)

	err := app.RunRoundtable("话题", 0) // 0 表示默认
	if err != nil {
		t.Fatal(err)
	}
	reqs := mock.Requests()
	if len(reqs) != 7 {
		t.Errorf("expected 7 calls for default 2 rounds, got %d", len(reqs))
	}
}

func TestRunRoundtable_MaxRounds(t *testing.T) {
	// 超过 5 轮应被截断为 5
	mock := newMockLLMComplete(t, "x")
	app, _, _ := newTestREPL(t, mock.server.URL, nil)

	err := app.RunRoundtable("话题", 10)
	if err != nil {
		t.Fatal(err)
	}
	// 5轮 × 3角色 + 1总结 = 16
	reqs := mock.Requests()
	if len(reqs) != 16 {
		t.Errorf("expected 16 calls for capped 5 rounds, got %d", len(reqs))
	}
}

func TestRunRoundtable_EmptyTopic(t *testing.T) {
	mock := newMockLLMComplete(t, "x")
	app, _, _ := newTestREPL(t, mock.server.URL, nil)

	err := app.RunRoundtable("   ", 2)
	if err == nil {
		t.Error("expected error for empty topic")
	}
}

func TestRunRoundtable_NoModel(t *testing.T) {
	app, _, _ := newTestREPL(t, "http://localhost:1", func(cfg *config.Config) {
		cfg.Model = ""
		cfg.Providers = nil
	})
	app.client = nil

	err := app.RunRoundtable("test", 1)
	if err == nil {
		t.Error("expected error when no model")
	}
}

func TestCompleteRoundtableTurn(t *testing.T) {
	mock := newMockLLMComplete(t, "hello world")
	app, _, _ := newTestREPL(t, mock.server.URL, nil)

	history := []llm.Message{
		llm.TextMessage("system", "你是测试"),
		llm.TextMessage("user", "说 hello"),
	}
	text, err := app.completeRoundtableTurn(history)
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello world" {
		t.Errorf("expected 'hello world', got %q", text)
	}
}
