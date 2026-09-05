package llm

import (
	"context"
)

// ChatMessage 通用聊天消息，用于适配各模块的 LLMClient 接口。
type ChatMessage struct {
	Role    string
	Content string
}

// Adapter 通用 LLM 适配器，将 Client 适配为各模块需要的 LLMClient 接口。
// 各模块可以定义自己的 ChatMessage 类型，但通过这个适配器统一转换。
type Adapter struct {
	client *Client
}

// NewAdapter 创建通用 LLM 适配器。
func NewAdapter(client *Client) *Adapter {
	return &Adapter{client: client}
}

// Complete 实现通用的 Complete 方法。
func (a *Adapter) Complete(ctx context.Context, messages []ChatMessage) (string, error) {
	llmMessages := make([]Message, len(messages))
	for i, m := range messages {
		llmMessages[i] = TextMessage(m.Role, m.Content)
	}
	return a.client.Complete(ctx, llmMessages)
}
