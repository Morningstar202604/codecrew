package server

import (
	"net/http"
	"time"
)

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
