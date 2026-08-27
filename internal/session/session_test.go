package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codecrew/internal/llm"
)

func TestAppendAndReloadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.New(Meta{Role: "developer", Model: "mock/m", WorkDir: "/tmp/proj"})
	if err != nil {
		t.Fatal(err)
	}
	sess.Append(llm.TextMessage("user", "看一下 main.go"))
	sess.Append(llm.Message{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.FunctionCall{Name: "read", Arguments: `{"path":"main.go"}`}}}})
	sess.Append(llm.Message{Role: "tool", Content: "1 | package main", ToolCallID: "c1", Name: "read"})
	sess.Append(llm.TextMessage("assistant", "入口在 package main"))
	sess.Append(llm.Message{}) // 空消息应被忽略
	sess.Close()

	meta, messages, err := store.Load(filepath.Base(strings.TrimSuffix(sess.Path(), ".jsonl")))
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 {
		t.Fatalf("消息数 = %d, want 4: %+v", len(messages), messages)
	}
	if messages[1].ToolCalls[0].Function.Name != "read" {
		t.Fatalf("tool_calls 未持久化: %+v", messages[1])
	}
	if messages[2].ToolCallID != "c1" || messages[2].Name != "read" {
		t.Fatalf("工具结果关联信息丢失: %+v", messages[2])
	}
	if meta.Role != "developer" || meta.Model != "mock/m" || meta.WorkDir != "/tmp/proj" {
		t.Fatalf("元信息丢失: %+v", meta)
	}

	// 会话文件首行必须是元信息对象（不含 role），否则读回时会被误当成消息
	data, _ := os.ReadFile(sess.Path())
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var probe map[string]any
	_ = json.Unmarshal([]byte(lines[0]), &probe)
	if _, ok := probe["content"]; ok {
		t.Fatalf("首行是元信息，不应带消息字段: %s", lines[0])
	}
	if _, ok := probe["created_at"]; !ok {
		t.Fatalf("首行应有 created_at: %s", lines[0])
	}
}

func TestOpenAppendsWithoutTruncating(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)
	sess, err := store.New(Meta{ID: "fixed-id", Role: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	sess.Append(llm.TextMessage("user", "第一轮"))
	sess.Close()

	again, err := store.Open("fixed-id")
	if err != nil {
		t.Fatal(err)
	}
	again.Append(llm.TextMessage("user", "第二轮"))
	again.Close()

	_, messages, err := store.Load("fixed-id")
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[1].Content != "第二轮" {
		t.Fatalf("Open 应追加而非覆盖: %+v", messages)
	}
	if again.Meta().Role != "tester" {
		t.Fatalf("元信息未读回: %+v", again.Meta())
	}
}

func TestListSortedAndPreview(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)
	for _, id := range []string{"20240101-000000", "20240102-000000", "20240103-000000"} {
		sess, err := store.New(Meta{ID: id, Role: "developer"})
		if err != nil {
			t.Fatal(err)
		}
		sess.Append(llm.TextMessage("user", "会话 "+id+" 的首个问题"))
		sess.Close()
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].ID != "20240103-000000" {
		t.Fatalf("列表应按时间倒序: %+v", list)
	}
	if list[0].Preview == "" {
		t.Fatal("预览应取首条用户消息")
	}
	if list[0].Messages != 1 {
		t.Fatalf("消息数 = %d", list[0].Messages)
	}
}

func TestLoadByNumberOrSubstring(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)
	sess, _ := store.New(Meta{ID: "20240101-120000", Role: "docs"})
	sess.Append(llm.TextMessage("user", "写 README"))
	sess.Close()

	if _, _, err := store.Load("120000"); err != nil {
		t.Fatalf("应按子串匹配会话: %v", err)
	}
	if _, _, err := store.Load("不存在"); err == nil {
		t.Fatal("找不到时应报错")
	}
}
