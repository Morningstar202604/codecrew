package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Message 是 OpenAI Chat Completions 协议中的一条消息。
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// TextMessage 构造一条普通文本消息。
func TextMessage(role, content string) Message {
	return Message{Role: role, Content: content}
}

type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
	Index    int          `json:"-"`
}

type FunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// Usage 是上游返回的 token 统计，部分供应商在流式最后一帧给出。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type Client struct {
	BaseURL     string
	APIKey      string
	Model       string
	HTTP        *http.Client
	Temperature *float64 // 可选，覆盖默认采样温度
}

func New(baseURL, apiKey, model string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		HTTP: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// URL 返回 chat/completions 端点；省略协议的本地地址自动补 http://。
func (c *Client) URL() string {
	base := strings.TrimRight(c.BaseURL, "/")
	if base != "" && !strings.Contains(base, "://") {
		base = "http://" + base
	}
	return base + "/chat/completions"
}

type chatRequest struct {
	Model       string           `json:"model"`
	Messages    []Message        `json:"messages"`
	Stream      bool             `json:"stream"`
	Tools       []map[string]any `json:"tools,omitempty"`
	Temperature *float64         `json:"temperature,omitempty"`
}

// rawFrame 是一条 SSE 帧的原始结构，tool_calls 保留 RawMessage 以便判断是否显式带 index。
type rawFrame struct {
	Choices []struct {
		Delta struct {
			Content   string            `json:"content"`
			ToolCalls []json.RawMessage `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
}

// callAccumulator 按槽位归并流式 tool_calls。
type callAccumulator struct {
	slots map[int]*ToolCall
	order []int
	// fresh 在帧开头重置：同一帧内没有显式 index 的第二个调用视为新槽位
	fresh bool
}

func newCallAccumulator() *callAccumulator {
	return &callAccumulator{slots: map[int]*ToolCall{}}
}

func (a *callAccumulator) beginFrame() { a.fresh = true }

func (a *callAccumulator) add(raw json.RawMessage) {
	var payload struct {
		Index    *int   `json:"index"`
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	idx := a.nextIndex(payload.Index, payload.ID)
	slot, ok := a.slots[idx]
	if !ok {
		slot = &ToolCall{Index: idx}
		a.slots[idx] = slot
		a.order = append(a.order, idx)
	}
	if payload.ID != "" {
		slot.ID = payload.ID
	}
	if payload.Type != "" {
		slot.Type = payload.Type
	}
	slot.Function.Name += payload.Function.Name
	slot.Function.Arguments += payload.Function.Arguments
}

// nextIndex 决定归并槽位：优先用显式 index；省略时按「一帧一个调用」的常见约定，
// 帧内有 ID 就是新调用，无 ID 则续写最后一个调用。
func (a *callAccumulator) nextIndex(explicit *int, id string) int {
	if explicit != nil {
		a.fresh = false
		return *explicit
	}
	if len(a.order) == 0 {
		a.fresh = false
		return 0
	}
	last := a.order[len(a.order)-1]
	if id != "" && (!a.fresh || a.slots[last].Function.Name != "") {
		a.fresh = false
		return last + 1
	}
	a.fresh = false
	return last
}

func (a *callAccumulator) result() []ToolCall {
	var out []ToolCall
	for _, idx := range a.order {
		call := *a.slots[idx]
		if call.Function.Name == "" {
			continue // 没有函数名的残帧，丢弃
		}
		if call.Type == "" {
			call.Type = "function"
		}
		if call.ID == "" {
			call.ID = fmt.Sprintf("call_%d", idx)
		}
		out = append(out, call)
	}
	return out
}

// Chat 发起流式对话。返回本轮完整文本、归并后的工具调用列表与 token 统计。
func (c *Client) Chat(ctx context.Context, messages []Message, tools []map[string]any, onDelta func(string)) (string, []ToolCall, *Usage, error) {
	payload, err := json.Marshal(chatRequest{Model: c.Model, Messages: messages, Stream: true, Tools: tools, Temperature: c.Temperature})
	if err != nil {
		return "", nil, nil, err
	}
	req, err := c.newRequest(ctx, payload)
	if err != nil {
		return "", nil, nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", nil, nil, fmt.Errorf("模型返回 %s：%s", resp.Status, strings.TrimSpace(string(data)))
	}

	var (
		sb       strings.Builder
		acc      = newCallAccumulator()
		usage    *Usage
		finished string
		scanner  = bufio.NewScanner(resp.Body)
	)
	scanner.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue // 空行与 SSE 注释行
		}
		body, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		body = strings.TrimSpace(body)
		if body == "" || body == "[DONE]" {
			continue
		}
		var frame rawFrame
		if err := json.Unmarshal([]byte(body), &frame); err != nil {
			continue
		}
		if frame.Usage != nil {
			usage = frame.Usage
		}
		for _, choice := range frame.Choices {
			if choice.Delta.Content != "" {
				sb.WriteString(choice.Delta.Content)
				onDelta(choice.Delta.Content)
			}
			acc.beginFrame()
			for _, raw := range choice.Delta.ToolCalls {
				acc.add(raw)
			}
			if choice.FinishReason != "" {
				finished = choice.FinishReason
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return sb.String(), nil, usage, fmt.Errorf("读取模型流失败: %w", err)
	}

	out := acc.result()
	if finished == "" && len(out) == 0 && sb.Len() == 0 {
		return "", nil, usage, fmt.Errorf("模型没有返回任何内容，请检查模型名与供应商配置")
	}
	return sb.String(), out, usage, nil
}

// Complete 发起一次非流式补全，用于摘要等后台任务。
func (c *Client) Complete(ctx context.Context, messages []Message) (string, error) {
	payload, err := json.Marshal(chatRequest{Model: c.Model, Messages: messages, Stream: false, Temperature: c.Temperature})
	if err != nil {
		return "", err
	}
	req, err := c.newRequest(ctx, payload)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("模型返回 %s：%s", resp.Status, strings.TrimSpace(string(data)))
	}
	var decoded struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("解析模型响应失败: %w", err)
	}
	if len(decoded.Choices) > 0 {
		return decoded.Choices[0].Message.Content, nil
	}
	return "", nil
}

func (c *Client) newRequest(ctx context.Context, payload []byte) (*http.Request, error) {
	if c.BaseURL == "" {
		return nil, fmt.Errorf("未配置 base_url")
	}
	if c.Model == "" {
		return nil, fmt.Errorf("未配置模型名")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	return req, nil
}

// WithTimeout 返回带超时的上下文，供后台任务使用。
func WithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// resolveCallIndex 决定一条 tool_calls 增量帧应归入哪个调用槽位。
// 兼容两种上游：显式携带 index 的标准实现，以及省略 index 的兼容端点。
func resolveCallIndex(frame string, tc ToolCall, order []int, calls map[int]*ToolCall) int {
	if jsonHasKey(frame, "index") {
		return tc.Index
	}
	if len(order) == 0 {
		return 0
	}
	last := order[len(order)-1]
	if tc.ID != "" && calls[last].Function.Name != "" {
		return last + 1
	}
	return last
}

// jsonHasKey 在 SSE 帧原文里粗暴查找某个键是否存在（值可能是 0，不能靠反序列化判断）。
func jsonHasKey(frame, key string) bool {
	return strings.Contains(frame, `"`+key+`"`)
}
