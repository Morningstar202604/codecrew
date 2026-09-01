// Package mcp 实现 Model Context Protocol (MCP) 客户端，
// 让 CodeCrew 可以复用 MCP 生态的工具（文件系统、数据库、API 等）。
//
// 当前支持 stdio 传输（启动子进程，通过 stdin/stdout 通信）。
// 协议基于 JSON-RPC 2.0，参考 https://modelcontextprotocol.io/
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// Tool 描述一个 MCP 工具。
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// CallResult 是工具调用的结果。
type CallResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

// Text 返回调用结果的纯文本（拼接所有 text 类型的 content）。
func (r *CallResult) Text() string {
	var out string
	for _, c := range r.Content {
		if c.Type == "text" {
			out += c.Text
		}
	}
	return out
}

// Client 是一个 MCP 客户端，通过 stdio 与 MCP 服务器通信。
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	mu      sync.Mutex
	pending map[int64]chan json.RawMessage
	nextID  atomic.Int64
	closed  bool

	serverName    string
	serverVersion string
}

// NewClient 启动一个 MCP 服务器子进程并完成初始化握手。
// command 是可执行文件路径，args 是启动参数。
func NewClient(command string, args ...string) (*Client, error) {
	cmd := exec.Command(command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 stdin 管道失败: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 stdout 管道失败: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 stderr 管道失败: %w", err)
	}

	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderr,
		pending: make(map[int64]chan json.RawMessage),
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 MCP 服务器失败: %w", err)
	}

	// 读取服务器 stderr 日志（异步，丢弃或记录）
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			// MCP 服务器的 stderr 通常是日志，静默丢弃
			_ = scanner.Text()
		}
	}()

	// 读取服务器 stdout，分发响应
	go c.readLoop()

	// 初始化握手
	if err := c.initialize(); err != nil {
		c.Close()
		return nil, fmt.Errorf("MCP 初始化失败: %w", err)
	}

	return c, nil
}

// readLoop 持续读取服务器输出，分发响应到对应的 pending channel。
func (c *Client) readLoop() {
	scanner := bufio.NewScanner(c.stdout)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg struct {
			ID     int64           `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		// 忽略通知（没有 id 的消息，如 notifications/initialized）
		if msg.ID == 0 && msg.Method != "" {
			continue
		}
		c.mu.Lock()
		ch, ok := c.pending[msg.ID]
		if ok {
			delete(c.pending, msg.ID)
		}
		c.mu.Unlock()
		if ok {
			if msg.Error != nil {
				ch <- json.RawMessage(fmt.Sprintf(`{"error":{"code":%d,"message":%q}}`, msg.Error.Code, msg.Error.Message))
			} else {
				ch <- msg.Result
			}
		}
	}
}

// call 发送一个 JSON-RPC 请求并等待响应。
func (c *Client) call(method string, params any) (json.RawMessage, error) {
	if c.closed {
		return nil, fmt.Errorf("MCP 客户端已关闭")
	}
	id := c.nextID.Add(1)
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	ch := make(chan json.RawMessage, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	// 写入请求
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("写入 MCP 请求失败: %w", err)
	}

	// 等待响应（超时 60 秒）
	select {
	case result := <-ch:
		// 检查是否是错误响应
		var errResp struct {
			Error *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(result, &errResp); err == nil && errResp.Error != nil {
			return nil, fmt.Errorf("MCP 错误 %d: %s", errResp.Error.Code, errResp.Error.Message)
		}
		return result, nil
	case <-time.After(60 * time.Second):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("MCP 请求超时: %s", method)
	}
}

// initialize 完成 MCP 初始化握手。
func (c *Client) initialize() error {
	result, err := c.call("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "codecrew",
			"version": "0.2.0",
		},
	})
	if err != nil {
		return err
	}
	var info struct {
		ServerInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(result, &info); err != nil {
		return fmt.Errorf("解析 initialize 响应失败: %w", err)
	}
	c.serverName = info.ServerInfo.Name
	c.serverVersion = info.ServerInfo.Version

	// 发送 initialized 通知（不需要响应）
	notif, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	c.stdin.Write(append(notif, '\n'))

	return nil
}

// ListTools 获取服务器提供的工具列表。
func (c *Client) ListTools() ([]Tool, error) {
	result, err := c.call("tools/list", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("解析 tools/list 响应失败: %w", err)
	}
	return resp.Tools, nil
}

// CallTool 调用一个 MCP 工具。
func (c *Client) CallTool(name string, args map[string]any) (*CallResult, error) {
	params := map[string]any{
		"name":      name,
		"arguments": args,
	}
	result, err := c.call("tools/call", params)
	if err != nil {
		return nil, err
	}
	var resp CallResult
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("解析 tools/call 响应失败: %w", err)
	}
	if resp.IsError {
		return &resp, fmt.Errorf("工具 %s 执行失败: %s", name, resp.Text())
	}
	return &resp, nil
}

// ServerName 返回服务器名称。
func (c *Client) ServerName() string { return c.serverName }

// ServerVersion 返回服务器版本。
func (c *Client) ServerVersion() string { return c.serverVersion }

// Close 关闭 MCP 客户端，终止子进程。
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	// 关闭所有 pending channel
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()

	c.stdin.Close()
	if c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	c.cmd.Wait()
	return nil
}
