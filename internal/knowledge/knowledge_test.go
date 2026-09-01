package knowledge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDefaults(t *testing.T) {
	c := Config{}
	if c.GetMaxResults() != 10 {
		t.Errorf("默认 MaxResults = %d, 应为 10", c.GetMaxResults())
	}
	if c.GetContextLines() != 3 {
		t.Errorf("默认 ContextLines = %d, 应为 3", c.GetContextLines())
	}
	if c.GetMaxFileSize() <= 0 {
		t.Error("默认 MaxFileSize 应 > 0")
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"main.go", "go"},
		{"index.js", "javascript"},
		{"main.py", "python"},
		{"README.md", "markdown"},
		{"config.json", "json"},
	}
	for _, tt := range tests {
		if got := detectLanguage(tt.filename); got != tt.expected {
			t.Errorf("detectLanguage(%q) = %q, 应为 %q", tt.filename, got, tt.expected)
		}
	}
}

func TestTokenize(t *testing.T) {
	tokens := tokenize("hello world func main")
	if len(tokens) < 3 {
		t.Errorf("tokenize() = %d tokens, 至少 3", len(tokens))
	}
}

func TestEpisodicStore(t *testing.T) {
	store := NewEpisodicStore()

	// 添加记忆
	store.Add("修复登录 bug", "已修复空指针", true, []string{"auth.go"}, "test-session")

	// 列表
	list := store.List()
	if len(list) != 1 {
		t.Errorf("List() len = %d, 应为 1", len(list))
	}

	// 最近记忆
	recent := store.Recent(5)
	if len(recent) != 1 {
		t.Errorf("Recent() len = %d, 应为 1", len(recent))
	}

	// 计数
	if count := store.Count(); count != 1 {
		t.Errorf("Count() = %d, 应为 1", count)
	}

	// 注入提示
	prompt := store.InjectPrompt(5)
	if prompt == "" {
		t.Error("InjectPrompt() 不应为空")
	}

	// 搜索
	results := store.Search("登录", 5)
	if len(results) == 0 {
		t.Error("Search('登录') 应返回结果")
	}

	// 清空
	store.Clear()
	if count := store.Count(); count != 0 {
		t.Errorf("Clear() 后 Count() = %d, 应为 0", count)
	}
}

func TestCodebaseIndexBuild(t *testing.T) {
	dir := t.TempDir()

	// 创建测试文件
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "util.go"), []byte("package main\n\nfunc util() {}\n"), 0o644)

	idx := NewCodebaseIndex(DefaultConfig(), dir)
	if err := idx.Build(); err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// 检查元数据
	meta := idx.Meta()
	if meta.FileCount < 2 {
		t.Errorf("FileCount = %d, 应 >= 2", meta.FileCount)
	}

	// 检查文件列表
	files := idx.Files()
	if len(files) < 2 {
		t.Errorf("Files() len = %d, 应 >= 2", len(files))
	}

	// 检查 IsStale
	if idx.IsStale() {
		t.Error("刚构建的索引不应过期")
	}
}

func TestFormatResults(t *testing.T) {
	results := []SearchResult{
		{File: "main.go", Line: 1, Score: 0.9, Content: "func main()"},
	}
	formatted := FormatResults(results)
	if formatted == "" {
		t.Error("FormatResults() 不应为空")
	}
}
