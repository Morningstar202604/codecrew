package server

import (
	"net/http"
)

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
