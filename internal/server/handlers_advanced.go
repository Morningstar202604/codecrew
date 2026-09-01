package server

import (
	"net/http"
	"strconv"
	"strings"
)

// --------------------------------------------------------------- 推理范式

func (s *Server) handleReasoning(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	ws, err := s.getOrCreateSession(session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Mode string `json:"mode"`
		}
		if err := readBody(r, &req); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		ws.REPL.HandleReasoningAPI(req.Mode)
	}

	mode, showThoughts, autoReflect, depth := ws.REPL.ReasoningStatus()
	writeJSON(w, 200, map[string]any{
		"mode":          mode,
		"show_thoughts": showThoughts,
		"auto_reflect":  autoReflect,
		"depth":         depth,
	})
}

func (s *Server) handleFailures(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	ws, err := s.getOrCreateSession(session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Action string `json:"action"`
		}
		if err := readBody(r, &req); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if req.Action == "clear" {
			ws.REPL.ClearFailures()
		}
	}

	failures := ws.REPL.ListFailures()
	writeJSON(w, 200, map[string]any{
		"failures": failures,
		"count":    len(failures),
	})
}

// --------------------------------------------------------------- 验证与自愈

func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	ws, err := s.getOrCreateSession(session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Repair bool `json:"repair"`
		}
		if err := readBody(r, &req); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		result := ws.REPL.RunVerifyAPI(req.Repair)
		writeJSON(w, 200, result)
		return
	}

	writeJSON(w, 200, map[string]any{
		"enabled": ws.REPL.VerifyEnabled(),
	})
}

// --------------------------------------------------------------- 代码库索引

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	ws, err := s.getOrCreateSession(session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	status := ws.REPL.IndexStatus()
	writeJSON(w, 200, status)
}

func (s *Server) handleIndexBuild(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	ws, err := s.getOrCreateSession(session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	if r.Method != http.MethodPost {
		writeError(w, 405, "仅支持 POST")
		return
	}

	ws.REPL.BuildIndex()
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleIndexSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	limitStr := r.URL.Query().Get("limit")
	limit := 10
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil {
			limit = n
		}
	}

	session := r.URL.Query().Get("session")
	ws, err := s.getOrCreateSession(session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	results := ws.REPL.SearchCodeAPI(query, limit)
	writeJSON(w, 200, map[string]any{
		"query":   query,
		"results": results,
		"count":   len(results),
	})
}

// --------------------------------------------------------------- Supervisor 编排

func (s *Server) handleSupervisor(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	ws, err := s.getOrCreateSession(session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Action string `json:"action"`
			Worker string `json:"worker"`
			Task   string `json:"task"`
			ID     int    `json:"id"`
			Result string `json:"result"`
		}
		if err := readBody(r, &req); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		ws.REPL.SupervisorAPI(req.Action, req.Worker, req.Task, req.ID, req.Result)
	}

	status := ws.REPL.SupervisorStatus()
	writeJSON(w, 200, status)
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	ws, err := s.getOrCreateSession(session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	if r.Method != http.MethodPost {
		writeError(w, 405, "仅支持 POST")
		return
	}

	var req struct {
		ID int `json:"id"`
	}
	if err := readBody(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	ws.REPL.ApproveAPI(req.ID)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleDeny(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	ws, err := s.getOrCreateSession(session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	if r.Method != http.MethodPost {
		writeError(w, 405, "仅支持 POST")
		return
	}

	var req struct {
		ID int `json:"id"`
	}
	if err := readBody(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	ws.REPL.DenyAPI(req.ID)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// --------------------------------------------------------------- 评估框架

func (s *Server) handleEval(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	ws, err := s.getOrCreateSession(session)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Action string `json:"action"`
		}
		if err := readBody(r, &req); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if req.Action == "run" {
			ws.REPL.RunEvalAPI()
		}
	}

	reports := ws.REPL.ListEvalReports()
	writeJSON(w, 200, map[string]any{
		"reports": reports,
		"count":   len(reports),
	})
}

// 辅助：获取 session 参数（避免重复代码）
func getSessionParam(r *http.Request) string {
	return strings.TrimSpace(r.URL.Query().Get("session"))
}
