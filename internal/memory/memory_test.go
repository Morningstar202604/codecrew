package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStore_AppendAndLoad(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	// 空角色加载应为空
	mem, err := s.Load("developer")
	if err != nil {
		t.Fatal(err)
	}
	if mem != "" {
		t.Errorf("expected empty memory, got %q", mem)
	}

	// 追加第一条
	if err := s.Append("developer", "记住：这个项目用 Go 1.22"); err != nil {
		t.Fatal(err)
	}
	mem, _ = s.Load("developer")
	if !strings.Contains(mem, "这个项目用 Go 1.22") {
		t.Errorf("memory should contain the note, got %q", mem)
	}
	if !strings.Contains(mem, "# developer 的长期记忆") {
		t.Errorf("memory should have header, got %q", mem)
	}

	// 追加第二条
	if err := s.Append("developer", "另一条：测试用 httptest"); err != nil {
		t.Fatal(err)
	}
	mem, _ = s.Load("developer")
	if !strings.Contains(mem, "测试用 httptest") {
		t.Errorf("memory should contain second note, got %q", mem)
	}
	if !strings.Contains(mem, "这个项目用 Go 1.22") {
		t.Errorf("memory should still contain first note, got %q", mem)
	}
}

func TestStore_AppendEmpty(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	if err := s.Append("developer", "   "); err == nil {
		t.Error("expected error for empty note")
	}
}

func TestStore_Clear(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	s.Append("tester", "一些记忆")
	if err := s.Clear("tester"); err != nil {
		t.Fatal(err)
	}
	mem, _ := s.Load("tester")
	if mem != "" {
		t.Errorf("expected empty after clear, got %q", mem)
	}
	// 清空不存在的角色不应报错
	if err := s.Clear("nonexistent"); err != nil {
		t.Errorf("clear nonexistent should not error, got %v", err)
	}
}

func TestStore_List(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	s.Append("architect", "a")
	s.Append("developer", "b")
	s.Append("tester", "c")

	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Errorf("expected 3 roles, got %d", len(list))
	}
	// 应按字母排序
	if list[0] != "architect" || list[1] != "developer" || list[2] != "tester" {
		t.Errorf("expected sorted list, got %v", list)
	}
}

func TestStore_Path(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	p := s.Path("developer")
	if !strings.HasSuffix(p, "developer.md") {
		t.Errorf("path should end with developer.md, got %s", p)
	}
	if !strings.HasPrefix(p, dir) {
		t.Errorf("path should be under store dir, got %s", p)
	}
}

func TestInjectPrompt(t *testing.T) {
	// 记忆为空时返回原 prompt
	prompt := "你是开发者"
	result := InjectPrompt(prompt, "")
	if result != prompt {
		t.Errorf("empty memory should return prompt unchanged, got %q", result)
	}

	// 记忆非空时追加
	mem := "- 一些记忆"
	result = InjectPrompt(prompt, mem)
	if !strings.HasPrefix(result, prompt) {
		t.Error("result should start with original prompt")
	}
	if !strings.Contains(result, "长期记忆") {
		t.Error("result should contain memory section header")
	}
	if !strings.Contains(result, "一些记忆") {
		t.Error("result should contain memory content")
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"developer": "developer",
		"my role":   "my_role",
		"a/b\\c":    "a_b_c",
		"role-123":  "role-123",
		"":          "unnamed",
		"中文角色":      "中文角色",    // Unicode 字母保留
		"...!!!":    "unnamed", // 纯符号被清理后为空
	}
	for input, expected := range cases {
		if got := sanitize(input); got != expected {
			t.Errorf("sanitize(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestDefaultStore(t *testing.T) {
	// DefaultStore 应该能创建并返回非 nil
	s, err := DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("expected non-nil store")
	}
	// 确认目录存在
	if _, err := os.Stat(s.Dir()); err != nil {
		t.Errorf("store dir should exist: %v", err)
	}
	// 确认路径在 ~/.codecrew/memory 下
	if !strings.Contains(s.Dir(), filepath.Join(".codecrew", "memory")) {
		t.Errorf("unexpected store dir: %s", s.Dir())
	}
}
