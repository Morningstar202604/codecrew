// Package server 提供 CodeCrew 的 HTTP/Web 服务层，把终端 REPL 能力暴露为
// REST API + SSE 流式接口，并托管前端静态文件。
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"codecrew/internal/config"
	"codecrew/internal/repl"
)

// Server 是 HTTP 服务主体，管理多个 Web 会话。
type Server struct {
	cfg      *config.Config
	baseDir  string
	mu       sync.RWMutex
	sessions map[string]*WebSession
	once     sync.Once
}

// WebSession 是一个 Web 端会话，持有独立的 REPL 实例与输出通道。
type WebSession struct {
	ID         string
	REPL       *repl.REPL
	out        *SwitchableWriter
	mu         sync.Mutex
	busy       bool
	createdAt  time.Time
	lastActive time.Time
}

// sessionTTL 是 Web 会话的最大空闲时间，超过后会被清理。
const sessionTTL = 2 * time.Hour

// SwitchableWriter 是可切换目标的 io.Writer，REPL 创建时注入，
// 每次请求时切换到当前的 channel writer，实现输出流式推送。
type SwitchableWriter struct {
	mu     sync.Mutex
	target io.Writer
}

func (w *SwitchableWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.target != nil {
		return w.target.Write(p)
	}
	return len(p), nil
}

func (w *SwitchableWriter) SetTarget(t io.Writer) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.target = t
}

// channelWriter 把输出写到 string channel，供 SSE 推送。
// 非阻塞写入：channel 满时丢弃溢出内容，避免 REPL 因慢客户端而卡住。
type channelWriter struct {
	ch chan<- string
}

func (w *channelWriter) Write(p []byte) (int, error) {
	select {
	case w.ch <- stripANSI(string(p)):
	default:
		// channel 满，丢弃此块输出（客户端太慢）
	}
	return len(p), nil
}

// ansiRegex 匹配 ANSI 转义序列。
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripANSI 移除 ANSI 颜色码，供 Web 端纯文本展示。
func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// NewServer 创建 HTTP 服务。
func NewServer(cfg *config.Config, baseDir string) *Server {
	return &Server{
		cfg:      cfg,
		baseDir:  baseDir,
		sessions: make(map[string]*WebSession),
	}
}

// Handler 返回挂载了所有路由的 http.Handler。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/chat", s.handleChat)
	mux.HandleFunc("/api/roles", s.handleRoles)
	mux.HandleFunc("/api/roles/switch", s.handleRoleSwitch)
	mux.HandleFunc("/api/model", s.handleModel)
	mux.HandleFunc("/api/model/switch", s.handleModelSwitch)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/config/reload", s.handleConfigReload)
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/sessions/new", s.handleSessionNew)
	mux.HandleFunc("/api/sessions/resume", s.handleSessionResume)
	mux.HandleFunc("/api/memory", s.handleMemory)
	mux.HandleFunc("/api/memory/add", s.handleMemoryAdd)
	mux.HandleFunc("/api/memory/clear", s.handleMemoryClear)
	mux.HandleFunc("/api/plan", s.handlePlan)
	mux.HandleFunc("/api/context", s.handleContext)
	mux.HandleFunc("/api/context/compact", s.handleContextCompact)
	mux.HandleFunc("/api/cost", s.handleCost)
	mux.HandleFunc("/api/tools", s.handleTools)
	mux.HandleFunc("/api/permissions", s.handlePermissions)
	mux.HandleFunc("/api/permissions/allow", s.handlePermissionAllow)
	mux.HandleFunc("/api/history/clear", s.handleHistoryClear)
	mux.HandleFunc("/api/history/undo", s.handleHistoryUndo)
	mux.HandleFunc("/", s.handleFrontend)

	return mux
}

// ListenAndServe 启动 HTTP 服务。
func (s *Server) ListenAndServe(addr string) error {
	fmt.Printf("CodeCrew Web 服务已启动: http://%s\n", addr)
	srv := &http.Server{
		Addr:         addr,
		Handler:      s.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // SSE 长连接需要无限写入超时
		IdleTimeout:  120 * time.Second,
	}
	return srv.ListenAndServe()
}

// getOrCreateSession 获取或创建 Web 会话。
func (s *Server) getOrCreateSession(sessionID string) (*WebSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sessionID != "" {
		if ws, ok := s.sessions[sessionID]; ok {
			ws.mu.Lock()
			ws.lastActive = time.Now()
			ws.mu.Unlock()
			return ws, nil
		}
	}

	if sessionID == "" {
		sessionID = fmt.Sprintf("web-%d", time.Now().UnixNano())
	}

	out := &SwitchableWriter{}
	sessCfg := *s.cfg
	// 深拷贝 map，避免会话间共享底层 map
	if s.cfg.Permissions != nil {
		sessCfg.Permissions = make(map[string]string, len(s.cfg.Permissions))
		for k, v := range s.cfg.Permissions {
			sessCfg.Permissions[k] = v
		}
	}
	if s.cfg.Providers != nil {
		sessCfg.Providers = make(map[string]config.Provider, len(s.cfg.Providers))
		for k, v := range s.cfg.Providers {
			sessCfg.Providers[k] = v
		}
	}
	r, err := repl.New(&sessCfg, repl.Options{
		BaseDir: s.baseDir,
		Stdin:   strings.NewReader(""),
		Stdout:  out,
		AutoYes: true,
	})
	if err != nil {
		return nil, fmt.Errorf("创建会话失败: %w", err)
	}

	ws := &WebSession{
		ID:         sessionID,
		REPL:       r,
		out:        out,
		createdAt:  time.Now(),
		lastActive: time.Now(),
	}
	s.sessions[sessionID] = ws
	// 启动后台清理（仅首次）
	s.once.Do(s.startCleanup)
	return ws, nil
}

// startCleanup 启动后台 goroutine，定期清理过期会话。
func (s *Server) startCleanup() {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			s.cleanupExpiredSessions()
		}
	}()
}

// cleanupExpiredSessions 清理超过 sessionTTL 未活动的会话。
func (s *Server) cleanupExpiredSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, ws := range s.sessions {
		ws.mu.Lock()
		busy := ws.busy
		lastActive := ws.lastActive
		ws.mu.Unlock()
		if busy {
			continue
		}
		if now.Sub(lastActive) > sessionTTL {
			ws.REPL.ClearHistory()
			delete(s.sessions, id)
		}
	}
}

// runInput 在 Web 会话中执行一条输入，输出通过返回的 channel 流式推送。
// 返回 outCh（输出内容）和 doneCh（完成信号）。调用方应读取 outCh 直到 doneCh 关闭。
func (ws *WebSession) runInput(input string) (<-chan string, <-chan struct{}, error) {
	ws.mu.Lock()
	if ws.busy {
		ws.mu.Unlock()
		return nil, nil, fmt.Errorf("会话正忙，请等待当前请求完成")
	}
	ws.busy = true
	outCh := make(chan string, 256)
	doneCh := make(chan struct{})
	ws.out.SetTarget(&channelWriter{ch: outCh})
	ws.mu.Unlock()

	go func() {
		defer func() {
			ws.mu.Lock()
			ws.out.SetTarget(nil)
			close(outCh)
			close(doneCh)
			ws.busy = false
			ws.mu.Unlock()
		}()
		ws.REPL.Send(input)
	}()

	return outCh, doneCh, nil
}

// writeJSON 写 JSON 响应。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError 写错误响应。
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// readBody 读取并解析 JSON 请求体。
func readBody(r *http.Request, v any) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}

// Shutdown 清理所有会话。
func (s *Server) Shutdown(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = make(map[string]*WebSession)
	return nil
}

// frontendFS 返回前端静态文件的文件系统。
func frontendFS() http.FileSystem {
	if _, err := os.Stat("web/index.html"); err == nil {
		return http.FS(os.DirFS("web"))
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(dir, "web/index.html")); err == nil {
			return http.FS(os.DirFS(dir))
		}
	}
	return nil
}
