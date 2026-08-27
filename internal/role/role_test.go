package role

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	src := "---\nname: dev\ndescription: 描述\n---\n\n你是工程师。\n- 要点\n"
	r, err := Parse("dev.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "dev" || r.Description != "描述" {
		t.Fatalf("解析结果 %+v", r)
	}
	if !strings.Contains(r.Prompt, "你是工程师") {
		t.Fatalf("prompt = %q", r.Prompt)
	}
}

func TestParseToolsList(t *testing.T) {
	cases := map[string]int{
		"tools: [read, write]":      2,
		"tools: [ read , glob ]":    2,
		"tools: [\"read\", 'glob']": 2,
		"tools: []":                 0,
	}
	for line, want := range cases {
		src := "---\nname: x\n" + line + "\n---\n正文\n"
		r, err := Parse("x.md", []byte(src))
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		if len(r.Tools) != want {
			t.Fatalf("%s -> %v, want %d 项", line, r.Tools, want)
		}
	}
}

func TestParseCRLFAndMissingFields(t *testing.T) {
	r, err := Parse("custom.md", []byte("---\r\ndescription: 只有描述\r\n---\r\n正文在此\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "custom" {
		t.Fatalf("缺 name 时应回落文件名，got %q", r.Name)
	}

	for _, bad := range []string{"没有 frontmatter", "---\nname: a\n没有结束标记", "---\nname: a\n---\n\n"} {
		if _, err := Parse("bad.md", []byte(bad)); err == nil {
			t.Fatalf("应报错: %q", bad)
		}
	}
}

func TestLoadBuiltinsOverrideAndMerge(t *testing.T) {
	builtins, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, r := range builtins {
		names[r.Name] = true
	}
	for _, want := range []string{"developer", "reviewer", "architect", "tester", "docs"} {
		if !names[want] {
			t.Fatalf("内置角色缺失 %s，got %v", want, names)
		}
	}

	dir := t.TempDir()
	// 覆盖内置 developer
	if err := os.WriteFile(filepath.Join(dir, "developer.md"), []byte("---\nname: developer\ndescription: 我的版本\n---\n我的提示词\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 新增自定义角色
	if err := os.WriteFile(filepath.Join(dir, "security.md"), []byte("---\nname: security\ndescription: 安全审计\ntools: [read, grep]\n---\n你是安全审计工程师\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	roles, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	dev, ok := Get(roles, "developer")
	if !ok || dev.Description != "我的版本" || dev.Builtin {
		t.Fatalf("磁盘角色应覆盖内置: %+v", dev)
	}
	if _, ok := Get(roles, "security"); !ok {
		t.Fatal("自定义角色应被加载")
	}
	if _, ok := Get(roles, "tester"); !ok {
		t.Fatal("未被覆盖的内置角色应保留")
	}
	for i := 1; i < len(roles); i++ {
		if roles[i-1].Name > roles[i].Name {
			t.Fatalf("角色未按名称排序: %v", roles)
		}
	}
}

func TestLoadIgnoresMissingDirButRejectsBrokenFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Fatalf("目录不存在应回落到内置角色: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.md"), []byte("不是合法角色文件"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("坏的角色文件应报错，而不是静默跳过")
	}
}
