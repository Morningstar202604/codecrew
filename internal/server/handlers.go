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
		"version": version,
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
