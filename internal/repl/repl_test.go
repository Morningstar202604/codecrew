package repl

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"codecrew/internal/config"
	"codecrew/internal/llm"
)

type recordedRequest struct {
	Messages []llm.Message     `json:"messages"`
	Tools    []json.RawMessage `json:"tools"`
	Model    string            `json:"model"`
	Stream   bool              `json:"stream"`
}

// mockLLM 是一个假的 OpenAI 兼容服务，按脚本依次返回响应，并记录每次收到的请求。
type mockLLM struct {
	mu       sync.Mutex
	server   *httptest.Server
	resplies []string
	calls    int
	requests []recordedRequest
}

func newMockLLM(t *testing.T, replies ...string) *mockLLM {
	t.Helper()
	m := &mockLLM{resplies: replies}
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
		if idx >= len(m.resplies) {
			idx = len(m.resplies) - 1
		}
		m.calls++
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, m.resplies[idx])
	}))
	t.Cleanup(m.server.Close)
	return m
}

func (m *mockLLM) Requests() []recordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]recordedRequest, len(m.requests))
	copy(out, m.requests)
	return out
}

func sseText(text string) string {
	frames := []string{
		`{"choices":[{"delta":{"role":"assistant","content":""}}]}`,
		fmt.Sprintf(`{"choices":[{"delta":{"content":%q}}]}`, text),
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`{"usage":{"prompt_tokens":20,"completion_tokens":5,"total_tokens":25}}`,
	}
	var sb strings.Builder
	for _, f := range frames {
		fmt.Fprintf(&sb, "data: %s\n\n", f)
	}
	sb.WriteString("data: [DONE]\n\n")
	return sb.String()
}

func sseToolCall(id, name string, args map[string]any) string {
	raw, _ := json.Marshal(args)
	inner, _ := json.Marshal(string(raw))
	frames := []string{
		fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":%q,"type":"function","function":{"name":%q,"arguments":""}}]}}]}`, id, name),
		fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":%s}}]}}]}`, inner),
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}
	var sb strings.Builder
	for _, f := range frames {
		fmt.Fprintf(&sb, "data: %s\n\n", f)
	}
	sb.WriteString("data: [DONE]\n\n")
	return sb.String()
}

func newTestREPL(t *testing.T, url string, mutate func(*config.Config)) (*REPL, *bytesBuffer, string) {
	app, out, work, _ := newTestREPLWithHome(t, url, "", mutate)
	return app, out, work
}

// resumeApp 在不自动建会话的前提下构造一个 REPL，用于测试 --session 续聊。
func resumeApp(t *testing.T, url, home, sessionID string) (*REPL, *bytesBuffer) {
	t.Helper()
	app, out, _, _ := newTestREPLWithHomeRaw(t, url, home, sessionID, nil)
	return app, out
}

// newTestREPLWithHome 允许两个 REPL 共享同一个 HOME（用于测试会话续聊）。
func newTestREPLWithHome(t *testing.T, url, home string, mutate func(*config.Config)) (*REPL, *bytesBuffer, string, string) {
	return newTestREPLWithHomeRaw(t, url, home, "", mutate)
}

func newTestREPLWithHomeRaw(t *testing.T, url, home, sessionID string, mutate func(*config.Config)) (*REPL, *bytesBuffer, string, string) {
	t.Helper()
	// HOME 指向独立临时目录（不交给 t.TempDir 清理，Windows 下会话文件句柄可能仍被占用）
	if home == "" {
		var err error
		home, err = os.MkdirTemp("", "codecrew-home-*")
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODECREW_COLOR", "0")

	work := t.TempDir()
	cfg := &config.Config{
		Model:     "mock/test-model",
		Providers: map[string]config.Provider{"mock": {BaseURL: url, APIKey: "sk-test-key-123456", Models: []string{"test-model"}}},
	}
	cfg.WorkingDir = work
	cfg.MaxContextTokens = 24000
	cfg.MaxToolRounds = 6
	cfg.Permissions = map[string]string{"*": "allow"}
	if mutate != nil {
		mutate(cfg)
	}

	app, err := New(cfg, Options{BaseDir: work, Stdin: strings.NewReader(""), Stdout: &bytesBuffer{}, SessionID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.closeSession() })
	out := app.out.(*bytesBuffer)
	return app, out, work, home
}

func TestAgentLoopFeedsToolResultBack(t *testing.T) {
	mock := newMockLLM(t,
		sseToolCall("call_1", "read", map[string]any{"path": "hello.txt"}),
		sseText("文件里写的是 hello from disk"),
	)
	app, out, work := newTestREPL(t, mock.server.URL, nil)
	if err := os.WriteFile(filepath.Join(work, "hello.txt"), []byte("hello from disk\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	app.handleInput("读一下 hello.txt")

	if app.usage.turns != 2 {
		t.Fatalf("模型应被调用两次，got %d\n输出:\n%s", app.usage.turns, out.String())
	}
	hist := app.History()
	roles := []string{}
	for _, m := range hist {
		roles = append(roles, m.Role)
	}
	want := []string{"system", "user", "assistant", "tool", "assistant"}
	if strings.Join(roles, ",") != strings.Join(want, ",") {
		t.Fatalf("历史结构错误: %v", roles)
	}
	toolMsg := hist[3]
	if toolMsg.ToolCallID != "call_1" || toolMsg.Name != "read" {
		t.Fatalf("工具结果缺少关联字段: %+v", toolMsg)
	}
	if !strings.Contains(toolMsg.Content, "hello from disk") {
		t.Fatalf("工具结果未回填: %q", toolMsg.Content)
	}
	if hist[2].Content == "" && len(hist[2].ToolCalls) == 0 {
		t.Fatal("assistant 消息丢失")
	}

	// 第二次请求必须带上工具结果，否则模型无法继续推理
	reqs := mock.Requests()
	if len(reqs) != 2 {
		t.Fatalf("mock 收到 %d 次请求", len(reqs))
	}
	last := reqs[1].Messages
	if len(last) != 4 {
		t.Fatalf("第二轮请求消息数 = %d, want 4", len(last))
	}
	if last[3].Role != "tool" || !strings.Contains(last[3].Content, "hello from disk") {
		t.Fatalf("第二轮请求未包含工具结果: %+v", last[3])
	}
	if !strings.Contains(out.String(), "hello from disk") {
		t.Fatalf("终端应打印模型回答:\n%s", out.String())
	}
}

func TestConversationKeepsGrowingHistory(t *testing.T) {
	mock := newMockLLM(t, sseText("第一轮答复"), sseText("第二轮答复"))
	app, _, _ := newTestREPL(t, mock.server.URL, nil)

	app.handleInput("第一句")
	app.handleInput("第二句")

	if got := len(app.History()); got != 5 {
		t.Fatalf("历史 = %d 条, want 5（system + 2×(user+assistant)）", got)
	}
	reqs := mock.Requests()
	if len(reqs[1].Messages) <= len(reqs[0].Messages) {
		t.Fatal("第二轮请求必须包含第一轮内容（回归：历史从未回填的 bug）")
	}
	if !strings.Contains(stringify(reqs[1].Messages), "第一句") {
		t.Fatal("上下文里应能看到上一轮用户输入")
	}
	if app.usage.prompt != 40 {
		t.Fatalf("token 统计 = %d", app.usage.prompt)
	}
}

func TestUnknownToolBecomesModelErrorNotCrash(t *testing.T) {
	mock := newMockLLM(t,
		sseToolCall("call_x", "no_such_tool", map[string]any{}),
		sseText("我换个工具"),
	)
	app, _, _ := newTestREPL(t, mock.server.URL, nil)
	app.handleInput("试试看")

	hist := app.History()
	if len(hist) < 4 {
		t.Fatalf("历史过短: %+v", hist)
	}
	if hist[3].Role != "tool" || !strings.Contains(hist[3].Content, "未知工具") {
		t.Fatalf("工具错误应回填给模型: %+v", hist[3])
	}
}

func TestDeniedToolIsReportedToModel(t *testing.T) {
	mock := newMockLLM(t,
		sseToolCall("c1", "write", map[string]any{"path": "x.txt", "content": "hi"}),
		sseText("好的，我不写了"),
	)
	app, _, work := newTestREPL(t, mock.server.URL, func(c *config.Config) {
		c.Permissions = map[string]string{"write": "deny"}
	})
	app.handleInput("创建 x.txt")

	if _, err := os.Stat(filepath.Join(work, "x.txt")); !os.IsNotExist(err) {
		t.Fatal("deny 的工具绝不能落盘")
	}
	hist := app.History()
	if !strings.Contains(hist[3].Content, "deny") {
		t.Fatalf("应把拒绝原因回传给模型: %+v", hist[3])
	}
}

func TestPermissionAskUsesApprover(t *testing.T) {
	mock := newMockLLM(t,
		sseToolCall("c1", "write", map[string]any{"path": "y.txt", "content": "ok"}),
		sseText("已按你的要求处理"),
	)
	app, _, work := newTestREPL(t, mock.server.URL, func(c *config.Config) {
		c.Permissions = map[string]string{"*": "allow", "write": "ask"}
	})
	// 用户回答 a=always
	app.opt.Stdin = strings.NewReader("a\n")
	app.scanner = newScanner(app.opt.Stdin)

	app.handleInput("写 y.txt")
	data, err := os.ReadFile(filepath.Join(work, "y.txt"))
	if err != nil || string(data) != "ok" {
		t.Fatalf("确认后应写入: %q %v", data, err)
	}
	if !strings.Contains(app.out.(*bytesBuffer).String(), "请求执行 write") {
		t.Fatal("应提示待确认的动作")
	}
}

func TestRoleSwitchReplacesSystemPromptAndTools(t *testing.T) {
	mock := newMockLLM(t, sseText("收到"))
	app, _, _ := newTestREPL(t, mock.server.URL, nil)

	app.handleInput("/role reviewer")
	app.handleInput("看一下代码")

	hist := app.History()
	if hist[0].Role != "system" || len(hist) != 3 {
		t.Fatalf("历史 = %+v", hist)
	}
	if strings.Count(strings.Join([]string{hist[0].Content}, "|"), "审查员") != 1 {
		t.Fatalf("system 提示词应被替换而非追加:\n%s", hist[0].Content)
	}
	req := mock.Requests()[0]
	for _, raw := range req.Tools {
		if strings.Contains(string(raw), `"write"`) || strings.Contains(string(raw), `"bash"`) {
			t.Fatalf("reviewer 不该看到写人工具: %s", raw)
		}
	}
	if !strings.Contains(stringify(req.Tools), "grep") {
		t.Fatal("reviewer 应看到 glob/grep")
	}
}

func TestContextCompactionKeepsRecentTurns(t *testing.T) {
	mock := newMockLLM(t, sseText("压缩后的回答"))
	var app *REPL
	wd := t.TempDir()
	app, _, _ = newTestREPL(t, mock.server.URL, func(c *config.Config) {
		c.MaxContextTokens = 200
	})
	app.history = []llm.Message{llm.TextMessage("system", "你是开发工程师")}
	for i := 0; i < 12; i++ {
		app.history = append(app.history,
			llm.TextMessage("user", fmt.Sprintf("第 %d 个问题，这是一段足够长的中文内容用来占位符呀呀呀", i)),
			llm.TextMessage("assistant", fmt.Sprintf("第 %d 个回答，同样足够长一些内容呀呀呀呀", i)))
	}
	before := len(app.history)
	app.compactIfNeeded()
	after := len(app.History())
	if after >= before {
		t.Fatalf("未压缩: %d -> %d", before, after)
	}
	if app.History()[1].Role != "system" || !strings.Contains(app.History()[1].Content, "前文摘要") {
		t.Fatalf("应插入摘要消息: %+v", app.History()[1])
	}
	// 摘要走的是非流式接口，mock 返回空内容时会退化为机械截断，仍要保留用户问题
	if !strings.Contains(app.History()[1].Content, "用户：") {
		t.Fatalf("兜底摘要应保留用户输入:\n%s", app.History()[1].Content)
	}
	if app.History()[after-1].Content == "" {
		t.Fatal("最近消息不应被丢弃")
	}
	_ = wd
}

func TestReloadRebuildsClient(t *testing.T) {
	mock := newMockLLM(t, sseText("ok"))
	app, out, work := newTestREPL(t, mock.server.URL, nil)

	// 初始无配置
	empty := &config.Config{}
	empty.WorkingDir = work
	app.cfg = empty
	app.client = app.buildClient()
	if app.client != nil {
		t.Fatal("前置条件：无配置时 client 应为 nil")
	}
	app.handleInput("这会提示未配置")
	if !strings.Contains(out.String(), "还没有可用的模型") {
		t.Fatalf("应提示配置:\n%s", out.String())
	}

	// 写入配置文件后 reload
	cfgPath := filepath.Join(work, "codecrew.json")
	raw, _ := json.Marshal(map[string]any{
		"model":     "mock/test-model",
		"providers": map[string]any{"mock": map[string]any{"base_url": mock.server.URL, "api_key": "sk-test", "models": []string{"test-model"}}},
	})
	if err := os.WriteFile(cfgPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	app.opt.ConfigPath = cfgPath
	app.handleInput("/reload")
	if app.client == nil {
		t.Fatalf("reload 后必须重建 client:\n%s", out.String())
	}
	if app.cfg.Model != "mock/test-model" {
		t.Fatal("reload 必须替换全局 cfg")
	}

	app.handleInput("现在可以对话了")
	if app.usage.turns != 1 {
		t.Fatalf("reload 后应能真正调用模型，turns=%d\n%s", app.usage.turns, out.String())
	}
}

func TestModelSwitchTakesEffect(t *testing.T) {
	mock := newMockLLM(t, sseText("ok"))
	app, out, _ := newTestREPL(t, mock.server.URL, func(c *config.Config) {
		c.Providers["second"] = config.Provider{BaseURL: mock.server.URL, APIKey: "k2", Models: []string{"other-model"}}
		c.Model = "mock/test-model"
	})
	app.client = app.buildClient()

	app.handleInput("/model second/other-model")
	if app.client == nil || app.client.Model != "other-model" {
		t.Fatalf("切换模型后 client 未更新:\n%s", out.String())
	}
	app.handleInput("hi")
	reqs := mock.Requests()
	if reqs[len(reqs)-1].Model != "other-model" {
		t.Fatalf("请求里的模型 = %q", reqs[len(reqs)-1].Model)
	}
	app.handleInput("/model nope/x")
	if !strings.Contains(out.String(), "未知供应商") {
		t.Fatal("切换失败应给出原因")
	}
}

func TestChineseAliasesAndUnknownCommand(t *testing.T) {
	mock := newMockLLM(t, sseText("ok"))
	app, out, _ := newTestREPL(t, mock.server.URL, nil)

	app.handleInput("角色")
	if !strings.Contains(out.String(), "可用角色") {
		t.Fatal("中文别名应生效")
	}
	app.handleInput("模型")
	if !strings.Contains(out.String(), "mock/test-model") {
		t.Fatalf("模型列表:\n%s", out.String())
	}
	app.handleInput("/不存在的命令")
	if !strings.Contains(out.String(), "未知命令") {
		t.Fatal("未知命令应提示")
	}
	if app.usage.turns != 0 {
		t.Fatal("命令不应触发模型调用")
	}
}

func TestUndoClearAndCost(t *testing.T) {
	mock := newMockLLM(t, sseText("答复一"), sseText("答复二"))
	app, out, _ := newTestREPL(t, mock.server.URL, nil)

	app.handleInput("问题一")
	app.handleInput("问题二")
	if len(app.History()) != 5 {
		t.Fatalf("历史 = %d", len(app.History()))
	}
	app.handleInput("/undo")
	if len(app.History()) != 3 {
		t.Fatalf("撤销后历史 = %d", len(app.History()))
	}
	app.handleInput("/clear")
	if len(app.History()) != 1 || app.History()[0].Role != "system" {
		t.Fatalf("清空后应保留 system: %+v", app.History())
	}
	app.handleInput("/cost")
	if !strings.Contains(out.String(), "tokens") {
		t.Fatal("应输出 token 统计")
	}
}

func TestSessionPersistenceAndResume(t *testing.T) {
	mock := newMockLLM(t, sseText("已记录"))
	app, _, _, home := newTestREPLWithHome(t, mock.server.URL, "", nil)

	app.handleInput("帮我看下项目结构")
	app.saveSession()
	if app.session == nil {
		t.Fatal("应已开会话")
	}
	path := app.session.Path()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "帮我看下项目结构") || !strings.Contains(string(data), "已记录") {
		t.Fatalf("会话内容不完整:\n%s", data)
	}

	// 新 REPL 通过 --session 语义续聊同一会话（共享 HOME）；先释放前一个句柄
	app.closeSession()
	id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	app2, out2 := resumeApp(t, mock.server.URL, home, id)
	if len(app2.History()) < 3 {
		t.Fatalf("续聊未恢复历史:\n%s", out2.String())
	}
	if !strings.Contains(out2.String(), "续聊会话 "+id) {
		t.Fatalf("应提示续聊成功:\n%s", out2.String())
	}
	app2.handleInput("继续")
	if !strings.Contains(stringify(mock.Requests()[len(mock.Requests())-1].Messages), "帮我看下项目结构") {
		t.Fatalf("续聊后的请求应带上下文:\n%s", out2.String())
	}
}

func TestToolRoundLimitStopsLoop(t *testing.T) {
	// 只给一条脚本回复：mock 会重复返回同一个工具调用，必须靠轮次上限兜住
	mock := newMockLLM(t, sseToolCall("c1", "read", map[string]any{"path": "missing.txt"}))
	app, out, _ := newTestREPL(t, mock.server.URL, func(c *config.Config) { c.MaxToolRounds = 3 })
	app.handleInput("无限循环吧")

	if app.usage.turns > 4 {
		t.Fatalf("未受控循环: %d 轮", app.usage.turns)
	}
	if !strings.Contains(out.String(), "上限") {
		t.Fatalf("应提示达到上限:\n%s", out.String())
	}
}

func TestCommandOutputAndPermissionsDisplay(t *testing.T) {
	mock := newMockLLM(t, sseText("ok"))
	app, out, _ := newTestREPL(t, mock.server.URL, nil)
	for _, cmd := range []string{"/config", "/tools", "/permissions", "/context", "/plan", "/sessions", "/help"} {
		app.handleInput(cmd)
	}
	text := out.String()
	for _, want := range []string{"已配置的供应商", config.MaskKey("sk-test-key-123456"), "bash", "ask", "上下文", "命令列表"} {
		if !strings.Contains(text, want) {
			t.Fatalf("缺少 %q 的输出:\n%s", want, text)
		}
	}
	if strings.Contains(text, "sk-test-key-123456") {
		t.Fatal("密钥不得明文出现在 /config 输出里")
	}
	if !strings.Contains(text, config.MaskKey("sk-test-key-123456")) {
		t.Fatalf("/config 应展示脱敏密钥:\n%s", text)
	}
}

func TestPlanCommandAndAdd(t *testing.T) {
	mock := newMockLLM(t,
		sseToolCall("p1", "plan", map[string]any{"action": "add", "title": "补齐 CI"}),
		sseText("计划已建立"),
	)
	app, out, _ := newTestREPL(t, mock.server.URL, nil)
	app.handleInput("先拆个计划")
	app.handleInput("/plan")
	if !strings.Contains(out.String(), "补齐 CI") {
		t.Fatalf("计划未记录:\n%s", out.String())
	}
	app.handleInput("/plan 手动加一条")
	if !strings.Contains(out.String(), "已加入计划 #2") {
		t.Fatalf("手动新增失败:\n%s", out.String())
	}
}

func TestCustomRoleFromDiskOverridesAndLoads(t *testing.T) {
	mock := newMockLLM(t, sseText("ok"))
	app, out, work := newTestREPL(t, mock.server.URL, nil)
	if err := os.MkdirAll(filepath.Join(work, "roles"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "---\nname: security\ndescription: 安全审计\ntools: [read, grep]\n---\n你是安全审计工程师，只读不写。\n"
	if err := os.WriteFile(filepath.Join(work, "roles", "security.md"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	app.handleInput("/role security")
	if !strings.Contains(out.String(), "已切换到 ") || !strings.Contains(out.String(), "security") {
		t.Fatalf("自定义角色不可用:\n%q", out.String())
	}
	app.handleInput("扫一下漏洞")
	req := mock.Requests()[len(mock.Requests())-1]
	if !strings.Contains(req.Messages[0].Content, "安全审计工程师") {
		t.Fatal("自定义 system 提示词未生效")
	}
	if strings.Contains(stringify(req.Tools), `"write"`) {
		t.Fatal("自定义角色的工具白名单未生效")
	}
}

func stringify(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

// bytesBuffer 是并发安全的输出收集器。
type bytesBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *bytesBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *bytesBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
