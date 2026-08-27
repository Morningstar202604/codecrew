package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// handler 按顺序把给定帧以 SSE 形式吐回去，并记录收到的请求体。
func handler(t *testing.T, frames []string, captured *[]map[string]any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Errorf("缺少 Authorization 头")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("请求体不是合法 JSON: %v", err)
		}
		if captured != nil {
			*captured = append(*captured, body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, f := range frames {
			fmt.Fprintln(w, "data: "+f)
			fmt.Fprintln(w)
			if flusher != nil {
				flusher.Flush()
			}
		}
		fmt.Fprintln(w, "data: [DONE]")
		fmt.Fprintln(w)
	}
}

func chunkContent(text string) string {
	return fmt.Sprintf(`{"choices":[{"delta":{"content":%q}}]}`, text)
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func newTestClient(url string) *Client {
	c := New(url, "sk-test", "unit-model")
	return c
}

func TestStreamContent(t *testing.T) {
	var got []map[string]any
	srv := httptest.NewServer(handler(t, []string{chunkContent("你好"), chunkContent("，世界")}, &got))
	defer srv.Close()

	var streamed strings.Builder
	text, calls, _, err := newTestClient(srv.URL).Chat(context.Background(), []Message{TextMessage("user", "hi")}, nil, func(d string) { streamed.WriteString(d) })
	if err != nil {
		t.Fatal(err)
	}
	if text != "你好，世界" || streamed.String() != "你好，世界" {
		t.Fatalf("text = %q streamed = %q", text, streamed.String())
	}
	if len(calls) != 0 {
		t.Fatalf("不该有工具调用: %+v", calls)
	}
	sent := got[0]
	if sent["stream"] != true {
		t.Fatalf("应使用流式: %+v", sent)
	}
	if _, ok := sent["tools"]; ok {
		t.Fatal("无工具时不应带 tools 字段")
	}
}

func TestStreamAccumulatesToolCallArguments(t *testing.T) {
	frames := []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":\""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"a.go\"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}
	var got []map[string]any
	srv := httptest.NewServer(handler(t, frames, &got))
	defer srv.Close()

	text, calls, _, err := newTestClient(srv.URL).Chat(context.Background(), []Message{TextMessage("user", "hi")}, nil, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].ID != "call_1" || calls[0].Function.Name != "read" {
		t.Fatalf("调用头信息丢失: %+v", calls[0])
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("arguments 拼接错误: %q %v", calls[0].Function.Arguments, err)
	}
	if args["path"] != "a.go" {
		t.Fatalf("args = %+v", args)
	}
	if text != "" {
		t.Fatalf("不该有正文: %q", text)
	}
}

// 回归：并行工具调用的 arguments 必须按 index 分槽，不得串到最后一个调用上
func TestStreamParallelToolCallsDoNotMixUp(t *testing.T) {
	frames := []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","type":"function","function":{"name":"read","arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"b","type":"function","function":{"name":"glob","arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":\"x\"}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"pattern\":\"*.go\"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}
	srv := httptest.NewServer(handler(t, frames, nil))
	defer srv.Close()

	_, calls, _, err := newTestClient(srv.URL).Chat(context.Background(), []Message{TextMessage("user", "hi")}, nil, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].Function.Name != "read" || calls[0].Function.Arguments != `{"path":"x"}` {
		t.Fatalf("第一个调用被污染: %+v", calls[0])
	}
	if calls[1].Function.Name != "glob" || calls[1].Function.Arguments != `{"pattern":"*.go"}` {
		t.Fatalf("第二个调用被污染: %+v", calls[1])
	}
}

// 兼容不带 index 字段的供应商：有 ID 即新调用
func TestStreamToolCallsWithoutIndex(t *testing.T) {
	frames := []string{
		`{"choices":[{"delta":{"tool_calls":[{"id":"a","type":"function","function":{"name":"read","arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"{\"path\":\"1\"}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"id":"b","type":"function","function":{"name":"grep","arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"{\"pattern\":\"x\"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}
	srv := httptest.NewServer(handler(t, frames, nil))
	defer srv.Close()

	_, calls, _, err := newTestClient(srv.URL).Chat(context.Background(), []Message{TextMessage("user", "hi")}, nil, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].Function.Arguments != `{"path":"1"}` || calls[1].Function.Arguments != `{"pattern":"x"}` {
		t.Fatalf("无 index 时归并错误: %+v", calls)
	}
}

func TestStreamIgnoresCommentsAndKeepsUsage(t *testing.T) {
	frames := []string{
		": this is a ping",
		chunkContent("ok"),
		`{"choices":[{"delta":{"content":""}}],"usage":{"prompt_tokens":11,"completion_tokens":5,"total_tokens":16}}`,
	}
	srv := httptest.NewServer(handler(t, frames, nil))
	defer srv.Close()

	_, _, usage, err := newTestClient(srv.URL).Chat(context.Background(), []Message{TextMessage("user", "hi")}, nil, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if usage == nil || usage.PromptTokens != 11 || usage.CompletionTokens != 5 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestStreamErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"invalid api key"}}`)
	}))
	defer srv.Close()

	_, _, _, err := newTestClient(srv.URL).Chat(context.Background(), []Message{TextMessage("user", "hi")}, nil, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("错误应带上游状态与原因: %v", err)
	}
}

func TestClientValidation(t *testing.T) {
	c := &Client{}
	if _, _, _, err := c.Chat(context.Background(), nil, nil, func(string) {}); err == nil {
		t.Fatal("缺少 base_url 应报错")
	}
	c = New("https://x", "k", "")
	if _, err := c.Complete(context.Background(), nil); err == nil {
		t.Fatal("缺少模型名应报错")
	}
	if New("https://x.example/v1/", "k", "m").URL() != "https://x.example/v1/chat/completions" {
		t.Fatalf("URL 拼接错误: %s", New("https://x.example/v1/", "k", "m").URL())
	}
	if New("localhost:11434/v1", "k", "m").URL() != "http://localhost:11434/v1/chat/completions" {
		t.Fatalf("省略协议的本地地址应补 http://: %s", New("localhost:11434/v1", "k", "m").URL())
	}
}

func TestCompleteNonStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body chatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Stream {
			t.Error("Complete 应为非流式")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "摘要结果"}}},
		})
	}))
	defer srv.Close()

	out, err := newTestClient(srv.URL).Complete(context.Background(), []Message{TextMessage("user", "总结")})
	if err != nil || out != "摘要结果" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestMessagesJSONShape(t *testing.T) {
	toolMsg, _ := json.Marshal(Message{Role: "tool", Content: "结果", ToolCallID: "call_1", Name: "read"})
	if !strings.Contains(string(toolMsg), `"tool_call_id":"call_1"`) {
		t.Fatalf("工具结果消息缺少 tool_call_id: %s", toolMsg)
	}
	if strings.Contains(string(toolMsg), "tool_calls") {
		t.Fatal("不应出现空 tool_calls 字段")
	}
	assistant, _ := json.Marshal(Message{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "read", Arguments: "{}"}}}})
	if !strings.Contains(string(assistant), `"tool_calls":[{"id":"c1","type":"function"`) {
		t.Fatalf("assistant 工具调用序列化异常: %s", assistant)
	}
}
