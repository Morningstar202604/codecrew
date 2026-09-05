package reasoning

import (
	"testing"
)

func TestParseMode(t *testing.T) {
	tests := []struct {
		input string
		want  Mode
	}{
		{"standard", ModeStandard},
		{"react", ModeReAct},
		{"reflexion", ModeReflexion},
		{"invalid", ModeStandard},
		{"", ModeStandard},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := ParseMode(tt.input); got != tt.want {
				t.Errorf("ParseMode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestModeString(t *testing.T) {
	if ModeStandard.String() != "standard" {
		t.Error("ModeStandard.String() 应为 standard")
	}
	if ModeReAct.String() != "react" {
		t.Error("ModeReAct.String() 应为 react")
	}
}

func TestConfigValidate(t *testing.T) {
	c := Config{Mode: "invalid"}
	c.Validate()
	if c.Mode != "standard" {
		t.Errorf("Validate() 后 Mode = %q, want standard", c.Mode)
	}

	c2 := Config{Mode: "reflexion", ReflectionDepth: 5}
	c2.Validate()
	if c2.ReflectionDepth > 3 {
		t.Errorf("Validate() 后 ReflectionDepth = %d, 应 <= 3", c2.ReflectionDepth)
	}
}

func TestFailureStore(t *testing.T) {
	dir := t.TempDir()
	store := NewFailureStore(dir)

	f := Failure{
		Task:       "修复登录 bug",
		Error:      "空指针异常",
		Reflection: "需要检查 nil 检查",
	}
	if err := store.Add("developer", f); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	list, err := store.List("developer")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 {
		t.Errorf("List() len = %d, want 1", len(list))
	}

	summary := store.RecentSummary("developer", 5)
	if summary == "" {
		t.Error("RecentSummary() 不应为空")
	}

	if err := store.Clear("developer"); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	list, _ = store.List("developer")
	if len(list) != 0 {
		t.Errorf("Clear() 后 len = %d, want 0", len(list))
	}
}

func TestFailureStorePersistence(t *testing.T) {
	dir := t.TempDir()
	store1 := NewFailureStore(dir)
	f := Failure{Task: "测试", Error: "错误"}
	store1.Add("tester", f)

	store2 := NewFailureStore(dir)
	list, err := store2.List("tester")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 {
		t.Errorf("持久化失败，len = %d, want 1", len(list))
	}
}

func TestPrompts(t *testing.T) {
	if p := ReActSystemPrompt(); p == "" {
		t.Error("ReActSystemPrompt() 不应为空")
	}
	if p := ReflexionPrompt("任务", "摘要", false, 1); p == "" {
		t.Error("ReflexionPrompt() 不应为空")
	}
	if p := FailureAnalysisPrompt("任务", "write", "错误"); p == "" {
		t.Error("FailureAnalysisPrompt() 不应为空")
	}
}
