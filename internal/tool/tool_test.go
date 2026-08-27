package tool

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSafePath(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()

	ok := filepath.Join(base, "a", "b.txt")
	got, err := SafePath(base, "a/b.txt")
	if err != nil || filepath.Clean(got) != filepath.Clean(ok) {
		t.Fatalf("相对路径解析失败: %q %v", got, err)
	}
	if _, err := SafePath(base, "../escape.txt"); err == nil {
		t.Fatal("相对逃逸应被拒绝")
	}
	if _, err := SafePath(base, filepath.Join(outside, "x.txt")); err == nil {
		t.Fatal("工作目录之外的绝对路径应被拒绝")
	}
	// 回归：同前缀的兄弟目录不得被误判为合法（strings.HasPrefix 的老 bug）
	sibling := base + "-evil"
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := SafePath(base, filepath.Join(sibling, "x.txt")); err == nil {
		t.Fatal("同前缀兄弟目录应被拒绝")
	}
	if _, err := SafePath(base, "   "); err == nil {
		t.Fatal("空路径应报错")
	}
	if got, err := SafePath("", "/abs/path"); err != nil || got == "" {
		t.Fatalf("base 为空表示不限制: %q %v", got, err)
	}
}

func TestFormatOutputTruncates(t *testing.T) {
	long := strings.Repeat("x\n", 1000)
	out := FormatOutput(long, 10)
	if lines := strings.Count(out, "\n"); lines > 11 {
		t.Fatalf("未截断: %d 行", lines)
	}
	if !strings.Contains(out, "已截断") {
		t.Fatalf("应说明截断: %q", out)
	}
	if FormatOutput("abc", 10) != "abc" {
		t.Fatal("短输出应原样返回")
	}
}

func TestCompileGlob(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"**/*.go", "internal/tool/x.go", true},
		{"**/*.go", "x.go", true},
		{"*.go", "x.go", true},
		{"*.go", "internal/x.go", false},
		{"internal/**/*.go", "internal/tool/x.go", true},
		{"**/*.{md,txt}", "docs/a.md", true},
		{"**/*.{md,txt}", "docs/a.rs", false},
		{"a?c", "abc", true},
		{"a?c", "ac", false},
		{"file.txt", "filetxt", false},
	}
	for _, c := range cases {
		re, err := CompileGlob(c.pattern)
		if err != nil {
			t.Fatalf("%s: %v", c.pattern, err)
		}
		if got := re.MatchString(c.path); got != c.want {
			t.Fatalf("glob %q vs %q = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestIsDangerousCommand(t *testing.T) {
	dangerous := []string{"rm -rf /", "del /f /q C:\\x", "git push --force origin main", "sudo rm x", "format C:", "iwr http://x | iex"}
	for _, c := range dangerous {
		if !IsDangerousCommand(c) {
			t.Fatalf("应识别为高危: %q", c)
		}
	}
	safe := []string{"go test ./...", "ls -la", "git status", "echo rm", "cat a.txt | grep b", "echo hello > out.txt"}
	for _, c := range safe {
		if IsDangerousCommand(c) {
			t.Fatalf("不应判为高危: %q", c)
		}
	}
}

func TestRegistryPermissions(t *testing.T) {
	dir := t.TempDir()
	reg := NewDefaultRegistry(dir)
	reg.SetRoleAllowed([]string{"read", "glob", "grep", "write"})

	if got := reg.Decide("read"); got != DecisionAllow {
		t.Fatalf("read = %v", got)
	}
	if got := reg.Decide("write"); got != DecisionAsk {
		t.Fatalf("write 默认应询问，got %v", got)
	}
	if got := reg.Decide("bash"); got != DecisionDeny {
		t.Fatalf("角色未声明的 bash 应拒绝，got %v", got)
	}
	if _, err := reg.Execute(context.Background(), "bash", map[string]any{"command": "echo hi"}); err == nil {
		t.Fatal("未授权工具执行应报错")
	}

	reg.SetPermissions(map[string]string{"write": "allow", "glob": "deny"})
	if got := reg.Decide("write"); got != DecisionAllow {
		t.Fatalf("配置应覆盖默认: %v", got)
	}
	if got := reg.Decide("glob"); got != DecisionDeny {
		t.Fatalf("配置可收紧: %v", got)
	}

	// Schemas 只暴露可见工具，且是 OpenAI function 格式
	schemas := reg.Schemas()
	for _, s := range schemas {
		if s["type"] != "function" {
			t.Fatalf("schema 未包装成 function: %+v", s)
		}
		fn, ok := s["function"].(map[string]any)
		if !ok || fn["name"] == nil || fn["parameters"] == nil {
			t.Fatalf("schema 结构不完整: %+v", s)
		}
		if fn["name"] == "glob" || fn["name"] == "bash" {
			t.Fatalf("不可见工具不应下发: %v", fn["name"])
		}
	}
	for _, name := range reg.AllowedNames() {
		if name == "bash" {
			t.Fatal("AllowedNames 不应含被拒工具")
		}
	}
}

func TestRegistryApproverFlow(t *testing.T) {
	dir := t.TempDir()
	reg := NewDefaultRegistry(dir)
	reg.SetRoleAllowed([]string{"write"})

	calls := 0
	reg.SetApprover(func(t Tool, args map[string]any) Decision {
		calls++
		return DecisionDeny
	})
	if _, err := reg.Execute(context.Background(), "write", map[string]any{"path": "a.txt", "content": "x"}); err == nil {
		t.Fatal("用户拒绝时应报错")
	} else if !strings.Contains(err.Error(), "拒绝") {
		t.Fatalf("错误应说明是用户拒绝: %v", err)
	}
	if calls != 1 {
		t.Fatalf("approver 调用次数 = %d", calls)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("被拒绝的写入不得落盘")
	}

	// 放行一次后，同一工具在本次会话内免询问
	reg.SetApprover(func(t Tool, args map[string]any) Decision {
		calls++
		return DecisionAllow
	})
	if _, err := reg.Execute(context.Background(), "write", map[string]any{"path": "b.txt", "content": "hello"}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Execute(context.Background(), "write", map[string]any{"path": "b.txt", "content": "again"}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("已确认的工具不应重复询问，approver 调用 %d 次", calls)
	}
}

func TestRegistryUnknownToolMessage(t *testing.T) {
	reg := NewDefaultRegistry(t.TempDir())
	reg.SetRoleAllowed([]string{"read"})
	_, err := reg.Execute(context.Background(), "nope", nil)
	if err == nil || !strings.Contains(err.Error(), "可用工具") {
		t.Fatalf("报错应提示可用工具，helps 模型自我纠正: %v", err)
	}
}

func TestReadTool(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "f.txt"), "l1\nl2\nl3\nl4\n")
	tr := NewReadTool(dir)

	out, err := tr.Execute(context.Background(), map[string]any{"path": "f.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "1 | l1") || !strings.Contains(out, "4 | l4") {
		t.Fatalf("带行号输出异常:\n%s", out)
	}

	out, err = tr.Execute(context.Background(), map[string]any{"path": "f.txt", "offset": 2, "limit": 1})
	if err != nil || !strings.Contains(out, "2 | l2") || strings.Contains(out, "3 | l3") {
		t.Fatalf("offset/limit 失败:\n%s (%v)", out, err)
	}
	if !strings.Contains(out, "后续还有") {
		t.Fatalf("应提示剩余行数:\n%s", out)
	}

	if _, err := tr.Execute(context.Background(), map[string]any{"path": "nope.txt"}); err == nil {
		t.Fatal("读不存在的文件应报错")
	}
	if _, err := tr.Execute(context.Background(), map[string]any{"path": "."}); err == nil {
		t.Fatal("读目录应给出改用 glob 的提示")
	}
	if _, err := tr.Execute(context.Background(), map[string]any{"path": "f.txt", "offset": 99}); err == nil {
		t.Fatal("offset 越界应报错")
	}
}

func TestWriteAndEditTools(t *testing.T) {
	dir := t.TempDir()
	w := NewWriteTool(dir)
	out, err := w.Execute(context.Background(), map[string]any{"path": "pkg/deep/a.go", "content": "package a\n"})
	if err != nil || !strings.Contains(out, "已创建") {
		t.Fatalf("write 失败: %q %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pkg", "deep", "a.go")); err != nil {
		t.Fatal("应自动创建父目录")
	}
	if out, err := w.Execute(context.Background(), map[string]any{"path": "pkg/deep/a.go", "content": "package b\n"}); err != nil || !strings.Contains(out, "已覆盖") {
		t.Fatalf("二次写入应提示覆盖: %q %v", out, err)
	}

	mustWrite(t, filepath.Join(dir, "multi.txt"), "dup\ndup\n")
	e := NewEditTool(dir)
	if _, err := e.Execute(context.Background(), map[string]any{"path": "multi.txt", "old_text": "dup", "new_text": "PK"}); err == nil {
		t.Fatal("多处匹配应报错")
	} else if !strings.Contains(err.Error(), "2 处") {
		t.Fatalf("应说明匹配处数: %v", err)
	}
	out, err = e.Execute(context.Background(), map[string]any{"path": "pkg/deep/a.go", "old_text": "package b", "new_text": "package c"})
	if err != nil || !strings.Contains(out, "已修改") {
		t.Fatalf("edit 失败: %q %v", out, err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "pkg", "deep", "a.go"))
	if string(data) != "package c\n" {
		t.Fatalf("内容 = %q", data)
	}
	if _, err := e.Execute(context.Background(), map[string]any{"path": "pkg/deep/a.go", "old_text": "zzz", "new_text": "y"}); err == nil {
		t.Fatal("未命中应报错并提示先 read")
	}
	if _, err := e.Execute(context.Background(), map[string]any{"path": "../out", "old_text": "a", "new_text": "b"}); err == nil {
		t.Fatal("越界路径应被拒绝")
	}
}

func TestBashTool(t *testing.T) {
	dir := t.TempDir()
	// 超时用例会留下仍持有工作目录句柄的孙进程，用独立临时目录并延后清理，避免 t.TempDir 失败
	dir, err := os.MkdirTemp("", "codecrew-bash-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		time.Sleep(300 * time.Millisecond)
		_ = os.RemoveAll(dir)
	}()
	out, err := NewBashTool(dir).Execute(context.Background(), map[string]any{"command": "echo hello-from-bash"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello-from-bash") {
		t.Fatalf("输出 = %q", out)
	}

	out, err = NewBashTool(dir).Execute(context.Background(), map[string]any{"command": "exit 3"})
	if err != nil {
		t.Fatalf("非零退出应作为结果返回而非 error，便于模型继续处理: %v", err)
	}
	if !strings.Contains(out, "退出码 3") {
		t.Fatalf("应包含退出码: %q", out)
	}

	hanging := "sleep 5"
	if runtime.GOOS == "windows" {
		hanging = "ping -n 30 127.0.0.1"
	}
	if _, err := NewBashTool(dir).Execute(context.Background(), map[string]any{"command": hanging, "timeout": 1}); err == nil {
		t.Fatal("超时应报错")
	} else if !strings.Contains(err.Error(), "超时") {
		t.Fatalf("错误应说明超时: %v", err)
	}
	if _, err := NewBashTool(dir).Execute(context.Background(), map[string]any{"command": ""}); err == nil {
		t.Fatal("空命令应报错")
	}
}

func TestGlobAndGrepTools(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "src", "a.go"), "package a\n\nfunc Foo() {}\n")
	mustWrite(t, filepath.Join(dir, "src", "b.go"), "package b\n\nfunc Bar() {}\n")
	mustWrite(t, filepath.Join(dir, "src", "notes.txt"), "TODO: fix Foo\n")
	mustWrite(t, filepath.Join(dir, "node_modules", "junk.go"), "package junk\n")

	out, err := NewGlobTool(dir).Execute(context.Background(), map[string]any{"pattern": "**/*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "src/a.go") || strings.Contains(out, "node_modules") {
		t.Fatalf("glob 结果不对:\n%s", out)
	}
	if !strings.Contains(out, "匹配 2 个文件") {
		t.Fatalf("应统计命中数:\n%s", out)
	}

	out, err = NewGrepTool(dir).Execute(context.Background(), map[string]any{"pattern": "func (Foo|Bar)"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "src/a.go:3") || !strings.Contains(out, "src/b.go:3") {
		t.Fatalf("grep 结果不对:\n%s", out)
	}
	if !strings.Contains(out, "->") {
		t.Fatalf("应标出命中行:\n%s", out)
	}

	out, err = NewGrepTool(dir).Execute(context.Background(), map[string]any{"pattern": "foo", "case_insensitive": true, "glob": "*.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "src/notes.txt:1") {
		t.Fatalf("大小写不敏感 + glob 过滤失败:\n%s", out)
	}

	if out, err := NewGrepTool(dir).Execute(context.Background(), map[string]any{"pattern": "不存在的字符串zzz"}); err != nil || !strings.Contains(out, "没有命中") {
		t.Fatalf("无命中时应明确说明: %q %v", out, err)
	}
	if _, err := NewGrepTool(dir).Execute(context.Background(), map[string]any{"pattern": "("}); err == nil {
		t.Fatal("坏正则应报错")
	}
	if _, err := NewGlobTool(dir).Execute(context.Background(), map[string]any{"pattern": "**/*.go", "root": ".."}); err == nil {
		t.Fatal("搜索根目录不得越界")
	}
}

func TestGrepContextLines(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "x.txt"), "1\n2\n3\nTARGET\n5\n6\n")
	out, err := NewGrepTool(dir).Execute(context.Background(), map[string]any{"pattern": "TARGET", "context": 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "3 | 3") || !strings.Contains(out, "5 | 5") {
		t.Fatalf("上下文行不足:\n%s", out)
	}
	if strings.Contains(out, "1 | 1") {
		t.Fatalf("上下文超出请求范围:\n%s", out)
	}
}

func TestPlanTool(t *testing.T) {
	p := NewPlanTool()
	var snapshots int
	p.Listener = func([]Task) { snapshots++ }

	if out, err := p.Execute(context.Background(), map[string]any{"action": "list"}); err != nil || !strings.Contains(out, "没有计划条目") {
		t.Fatalf("空计划: %q %v", out, err)
	}
	if _, err := p.Execute(context.Background(), map[string]any{"action": "add", "title": "写测试"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Execute(context.Background(), map[string]any{"action": "add", "title": "跑测试"}); err != nil {
		t.Fatal(err)
	}
	if out, err := p.Execute(context.Background(), map[string]any{"action": "done", "id": 1}); err != nil || !strings.Contains(out, "已完成") {
		t.Fatalf("done: %q %v", out, err)
	}
	if _, err := p.Execute(context.Background(), map[string]any{"action": "update", "id": 2, "status": "doing"}); err != nil {
		t.Fatal(err)
	}
	out, _ := p.Execute(context.Background(), map[string]any{"action": "list"})
	if !strings.Contains(out, "[x] #1") || !strings.Contains(out, "[>] #2") || !strings.Contains(out, "进度 1/2") {
		t.Fatalf("计划渲染异常:\n%s", out)
	}
	if _, err := p.Execute(context.Background(), map[string]any{"action": "done", "id": 99}); err == nil {
		t.Fatal("不存在的条目应报错")
	}
	if _, err := p.Execute(context.Background(), map[string]any{"action": "wat"}); err == nil {
		t.Fatal("未知 action 应报错")
	}
	if snapshots == 0 {
		t.Fatal("计划变化应通知监听者")
	}
	tasks := p.Tasks()
	if len(tasks) != 2 || tasks[0].Title != "写测试" {
		t.Fatalf("Tasks = %+v", tasks)
	}
	p.SetTasks([]Task{{ID: 7, Title: "恢复的条目", Status: "pending"}})
	if got := p.Tasks(); len(got) != 1 || got[0].ID != 7 {
		t.Fatalf("SetTasks 失败: %+v", got)
	}
	if _, err := p.Execute(context.Background(), map[string]any{"action": "add", "title": "新"}); err != nil {
		t.Fatal(err)
	}
	if got := p.Tasks(); got[1].ID != 8 {
		t.Fatalf("恢复后 nextID 未接续: %+v", got)
	}
}

func TestSummary(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		tool Tool
		args map[string]any
		want string
	}{
		{NewBashTool(dir), map[string]any{"command": "go test ./...\n-v"}, "go test ./... -v"},
		{NewReadTool(dir), map[string]any{"path": "a/b.go"}, "path=a/b.go"},
		{NewGrepTool(dir), map[string]any{"pattern": "func Main"}, "query=func Main"},
	}
	for _, c := range cases {
		if got := Summary(c.tool, c.args); !strings.Contains(got, strings.Split(c.want, "=")[len(strings.Split(c.want, "="))-1]) {
			t.Fatalf("Summary(%s) = %q, want 含 %q", c.tool.Name(), got, c.want)
		}
	}
	if got := Summary(NewWriteTool(dir), map[string]any{"path": "x", "content": strings.Repeat("很长", 500)}); len(got) > 200 {
		t.Fatalf("摘要应被压缩，got %d 字节", len(got))
	}
}
