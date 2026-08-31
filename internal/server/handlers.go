package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// --------------------------------------------------------------- 健康检查

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"status":  "ok",
		"version": "v0.2.0",
		"time":    time.Now().Format(time.RFC3339),
	})
}

// --------------------------------------------------------------- 对话（SSE）

type chatRequest struct {
	Session string `json:"session"`
	Message string `json:"message"`
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "仅支持 POST")
		return
	}
	var req chatRequest
	if err := readBody(r, &req); err != nil {
		writeError(w, 400, "请求体解析失败: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		writeError(w, 400, "消息不能为空")
		return
	}

	ws, err := s.getOrCreateSession(req.Session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	outCh, doneCh, err := ws.runInput(req.Message)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}

	// SSE 响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "服务器不支持流式响应")
		return
	}

	// 发送会话 ID
	fmt.Fprintf(w, "data: {\"type\":\"session\",\"id\":\"%s\"}\n\n", ws.ID)
	flusher.Flush()

	// 流式推送输出
	for {
		select {
		case chunk, ok := <-outCh:
			if !ok {
				// 输出通道关闭，等待 done
				<-doneCh
				fmt.Fprintf(w, "data: {\"type\":\"done\"}\n\n")
				flusher.Flush()
				return
			}
			// JSON 编码输出内容
			encoded, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: {\"type\":\"output\",\"content\":%s}\n\n", encoded)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// --------------------------------------------------------------- 角色

func (s *Server) handleRoles(w http.ResponseWriter, r *http.Request) {
	ws, err := s.getOrCreateSession(r.URL.Query().Get("session"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	roles := ws.REPL.Roles()
	type roleInfo struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Tools       []string `json:"tools"`
		Builtin     bool     `json:"builtin"`
		Current     bool     `json:"current"`
	}
	current := ws.REPL.CurrentRoleName()
	out := make([]roleInfo, 0, len(roles))
	for _, rr := range roles {
		out = append(out, roleInfo{
			Name:        rr.Name,
			Description: rr.Description,
			Tools:       rr.Tools,
			Builtin:     rr.Builtin,
			Current:     rr.Name == current,
		})
	}
	writeJSON(w, 200, map[string]any{"roles": out, "current": current})
}

type roleSwitchRequest struct {
	Session string `json:"session"`
	Role    string `json:"role"`
}

func (s *Server) handleRoleSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "仅支持 POST")
		return
	}
	var req roleSwitchRequest
	if err := readBody(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ws, err := s.getOrCreateSession(req.Session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	// 用 Send 执行 /role 命令，复用现有逻辑
	ws.REPL.Send("/role " + req.Role)
	writeJSON(w, 200, map[string]any{
		"ok":      true,
		"current": ws.REPL.CurrentRoleName(),
	})
}

// --------------------------------------------------------------- 模型

func (s *Server) handleModel(w http.ResponseWriter, r *http.Request) {
	ws, err := s.getOrCreateSession(r.URL.Query().Get("session"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	cfg := ws.REPL.Config()
	type providerInfo struct {
		Name    string   `json:"name"`
		BaseURL string   `json:"base_url"`
		Models  []string `json:"models"`
		HasKey  bool     `json:"has_key"`
	}
	providers := make([]providerInfo, 0)
	for _, name := range cfg.ProviderNames() {
		p := cfg.Providers[name]
		providers = append(providers, providerInfo{
			Name:    name,
			BaseURL: p.BaseURL,
			Models:  p.Models,
			HasKey:  p.APIKey != "",
		})
	}
	writeJSON(w, 200, map[string]any{
		"current":   cfg.Model,
		"providers": providers,
	})
}

type modelSwitchRequest struct {
	Session string `json:"session"`
	Model   string `json:"model"`
}

func (s *Server) handleModelSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "仅支持 POST")
		return
	}
	var req modelSwitchRequest
	if err := readBody(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ws, err := s.getOrCreateSession(req.Session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	ws.REPL.Send("/model " + req.Model)
	writeJSON(w, 200, map[string]any{
		"ok":      true,
		"current": ws.REPL.CurrentModel(),
	})
}

// --------------------------------------------------------------- 配置

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	ws, err := s.getOrCreateSession(r.URL.Query().Get("session"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	cfg := ws.REPL.Config()
	// 脱敏供应商信息，绝不返回 api_key
	type providerSafe struct {
		Name    string   `json:"name"`
		BaseURL string   `json:"base_url"`
		Models  []string `json:"models"`
		HasKey  bool     `json:"has_key"`
	}
	providers := make([]providerSafe, 0)
	for _, name := range cfg.ProviderNames() {
		p := cfg.Providers[name]
		providers = append(providers, providerSafe{
			Name: name, BaseURL: p.BaseURL, Models: p.Models, HasKey: p.APIKey != "",
		})
	}
	writeJSON(w, 200, map[string]any{
		"model":              cfg.Model,
		"working_dir":        cfg.WorkDir(),
		"max_context_tokens": cfg.MaxContextTokens,
		"max_tool_rounds":    cfg.MaxToolRounds,
		"permissions":        cfg.Permissions,
		"source":             cfg.Source,
		"providers":          providers,
	})
}

type configReloadRequest struct {
	Session string `json:"session"`
}

func (s *Server) handleConfigReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "仅支持 POST")
		return
	}
	var req configReloadRequest
	if err := readBody(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ws, err := s.getOrCreateSession(req.Session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	ws.REPL.Send("/reload")
	writeJSON(w, 200, map[string]any{"ok": true})
}

// --------------------------------------------------------------- 会话

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	ws, err := s.getOrCreateSession(r.URL.Query().Get("session"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	store := ws.REPL.SessionStore()
	if store == nil {
		writeJSON(w, 200, map[string]any{"sessions": []any{}})
		return
	}
	list, err := store.List()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	type sessMeta struct {
		ID        string `json:"id"`
		Role      string `json:"role"`
		Model     string `json:"model"`
		Messages  int    `json:"messages"`
		Preview   string `json:"preview"`
		CreatedAt string `json:"created_at"`
	}
	out := make([]sessMeta, 0, len(list))
	for _, m := range list {
		out = append(out, sessMeta{
			ID: m.ID, Role: m.Role, Model: m.Model, Messages: m.Messages,
			Preview: m.Preview, CreatedAt: m.CreatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, 200, map[string]any{"sessions": out})
}

type sessionNewRequest struct {
	Session string `json:"session"`
}

func (s *Server) handleSessionNew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "仅支持 POST")
		return
	}
	var req sessionNewRequest
	if err := readBody(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ws, err := s.getOrCreateSession(req.Session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	ws.REPL.Send("/new")
	writeJSON(w, 200, map[string]any{"ok": true})
}

type sessionResumeRequest struct {
	Session string `json:"session"`
	ID      string `json:"id"`
}

func (s *Server) handleSessionResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "仅支持 POST")
		return
	}
	var req sessionResumeRequest
	if err := readBody(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ws, err := s.getOrCreateSession(req.Session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	ws.REPL.Send("/resume " + req.ID)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// --------------------------------------------------------------- 记忆

func (s *Server) handleMemory(w http.ResponseWriter, r *http.Request) {
	ws, err := s.getOrCreateSession(r.URL.Query().Get("session"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	roleName := r.URL.Query().Get("role")
	if roleName == "" {
		roleName = ws.REPL.CurrentRoleName()
	}
	store := ws.REPL.MemoryStore()
	if store == nil {
		writeJSON(w, 200, map[string]any{"role": roleName, "content": "", "path": ""})
		return
	}
	content, err := store.Load(roleName)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	all, _ := store.List()
	writeJSON(w, 200, map[string]any{
		"role":    roleName,
		"content": content,
		"path":    store.Path(roleName),
		"all":     all,
	})
}

type memoryAddRequest struct {
	Session string `json:"session"`
	Role    string `json:"role"`
	Note    string `json:"note"`
}

func (s *Server) handleMemoryAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "仅支持 POST")
		return
	}
	var req memoryAddRequest
	if err := readBody(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ws, err := s.getOrCreateSession(req.Session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	roleName := req.Role
	if roleName == "" {
		roleName = ws.REPL.CurrentRoleName()
	}
	store := ws.REPL.MemoryStore()
	if store == nil {
		writeError(w, 500, "记忆存储不可用")
		return
	}
	// 通过 REPL 命令添加，自动刷新 system prompt（避免重复添加）
	ws.REPL.Send("/memory add " + req.Note)
	writeJSON(w, 200, map[string]any{"ok": true, "role": roleName})
}

type memoryClearRequest struct {
	Session string `json:"session"`
	Role    string `json:"role"`
}

func (s *Server) handleMemoryClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "仅支持 POST")
		return
	}
	var req memoryClearRequest
	if err := readBody(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ws, err := s.getOrCreateSession(req.Session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	roleName := req.Role
	if roleName == "" {
		roleName = ws.REPL.CurrentRoleName()
	}
	store := ws.REPL.MemoryStore()
	if store == nil {
		writeError(w, 500, "记忆存储不可用")
		return
	}
	// 通过 REPL 命令清空，自动刷新 system prompt
	ws.REPL.Send("/memory clear")
	writeJSON(w, 200, map[string]any{"ok": true, "role": roleName})
}

// --------------------------------------------------------------- 计划

func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	ws, err := s.getOrCreateSession(r.URL.Query().Get("session"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	tasks := ws.REPL.PlanTasks()
	type taskInfo struct {
		ID     int    `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	out := make([]taskInfo, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, taskInfo{ID: t.ID, Title: t.Title, Status: t.Status})
	}
	writeJSON(w, 200, map[string]any{"tasks": out})
}

// --------------------------------------------------------------- 上下文

func (s *Server) handleContext(w http.ResponseWriter, r *http.Request) {
	ws, err := s.getOrCreateSession(r.URL.Query().Get("session"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	used, limit := ws.REPL.ContextStats()
	history := ws.REPL.History()
	writeJSON(w, 200, map[string]any{
		"used_tokens":  used,
		"limit_tokens": limit,
		"messages":     len(history),
		"compactions":  ws.REPL.CompactionCount(),
	})
}

type contextCompactRequest struct {
	Session string `json:"session"`
}

func (s *Server) handleContextCompact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "仅支持 POST")
		return
	}
	var req contextCompactRequest
	if err := readBody(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ws, err := s.getOrCreateSession(req.Session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if err := ws.REPL.CompactNow(); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	used, limit := ws.REPL.ContextStats()
	writeJSON(w, 200, map[string]any{"ok": true, "used_tokens": used, "limit_tokens": limit})
}

// --------------------------------------------------------------- 成本

func (s *Server) handleCost(w http.ResponseWriter, r *http.Request) {
	ws, err := s.getOrCreateSession(r.URL.Query().Get("session"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	turns, prompt, completion, elapsed := ws.REPL.CostStats()
	writeJSON(w, 200, map[string]any{
		"turns":             turns,
		"prompt_tokens":     prompt,
		"completion_tokens": completion,
		"total_tokens":      prompt + completion,
		"elapsed_seconds":   int(elapsed.Seconds()),
	})
}

// --------------------------------------------------------------- 工具与权限

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	ws, err := s.getOrCreateSession(r.URL.Query().Get("session"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	type toolInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Permission  string `json:"permission"`
		Allowed     bool   `json:"allowed"`
	}
	names := ws.REPL.AllToolNames()
	out := make([]toolInfo, 0, len(names))
	for _, name := range names {
		decision := ws.REPL.ToolDecision(name)
		out = append(out, toolInfo{
			Name: name, Description: ws.REPL.ToolDescription(name),
			Permission: decision, Allowed: decision != "deny",
		})
	}
	writeJSON(w, 200, map[string]any{"tools": out})
}

func (s *Server) handlePermissions(w http.ResponseWriter, r *http.Request) {
	ws, err := s.getOrCreateSession(r.URL.Query().Get("session"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	cfg := ws.REPL.Config()
	writeJSON(w, 200, map[string]any{"permissions": cfg.Permissions})
}

type permissionAllowRequest struct {
	Session string `json:"session"`
	Tool    string `json:"tool"`
}

func (s *Server) handlePermissionAllow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "仅支持 POST")
		return
	}
	var req permissionAllowRequest
	if err := readBody(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ws, err := s.getOrCreateSession(req.Session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if err := ws.REPL.AllowTool(req.Tool); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "tool": req.Tool})
}

// --------------------------------------------------------------- 历史

type historyClearRequest struct {
	Session string `json:"session"`
}

func (s *Server) handleHistoryClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "仅支持 POST")
		return
	}
	var req historyClearRequest
	if err := readBody(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ws, err := s.getOrCreateSession(req.Session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	ws.REPL.ClearHistory()
	writeJSON(w, 200, map[string]any{"ok": true})
}

type historyUndoRequest struct {
	Session string `json:"session"`
}

func (s *Server) handleHistoryUndo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "仅支持 POST")
		return
	}
	var req historyUndoRequest
	if err := readBody(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ws, err := s.getOrCreateSession(req.Session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	ws.REPL.Undo()
	writeJSON(w, 200, map[string]any{"ok": true})
}

// --------------------------------------------------------------- 前端静态文件

func (s *Server) handleFrontend(w http.ResponseWriter, r *http.Request) {
	fs := frontendFS()
	if fs == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>CodeCrew</title></head>
<body style="font-family:sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0">
<div style="text-align:center">
<h1>CodeCrew Web</h1>
<p>前端文件未找到。请确保 web/ 目录与可执行文件在同一目录下。</p>
<p style="color:#666;font-size:14px">开发模式：在项目根目录运行 codecrew --serve</p>
</div></body></html>`)
		return
	}
	// SPA 路由：不存在的路径回退到 index.html
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}
	f, err := fs.Open(strings.TrimPrefix(path, "/"))
	if err != nil {
		// 回退到 index.html
		f, err = fs.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, path, stat.ModTime(), f)
}
