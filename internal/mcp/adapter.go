package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"codecrew/internal/tool"
)

// ToolAdapter 将一个 MCP 工具包装成 CodeCrew 的 tool.Tool 接口。
type ToolAdapter struct {
	client *Client
	tool   Tool
}

// NewToolAdapter 创建一个 MCP 工具适配器。
func NewToolAdapter(client *Client, t Tool) *ToolAdapter {
	return &ToolAdapter{client: client, tool: t}
}

// Name 返回工具名。为避免与内置工具冲突，MCP 工具统一加 "mcp_" 前缀。
func (a *ToolAdapter) Name() string {
	return "mcp_" + a.tool.Name
}

// Description 返回工具描述，前缀标注来源 MCP 服务器。
func (a *ToolAdapter) Description() string {
	return fmt.Sprintf("[MCP:%s] %s", a.client.ServerName(), a.tool.Description)
}

// Schema 返回工具的参数 schema（从 MCP 的 inputSchema 转换）。
func (a *ToolAdapter) Schema() map[string]any {
	var schema map[string]any
	if err := json.Unmarshal(a.tool.InputSchema, &schema); err != nil {
		// 解析失败时返回空 schema
		return map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}
	return schema
}

// Execute 调用 MCP 工具并返回结果文本。
func (a *ToolAdapter) Execute(ctx context.Context, args map[string]any) (string, error) {
	// 去掉前缀，得到原始工具名
	origName := a.tool.Name
	result, err := a.client.CallTool(origName, args)
	if err != nil {
		return "", err
	}
	text := result.Text()
	if text == "" {
		return "(工具执行成功，无文本输出)", nil
	}
	// 限制输出长度，避免吃光上下文
	return tool.FormatOutput(text, tool.MaxOutputLines), nil
}

// 编译期检查：ToolAdapter 实现了 tool.Tool 接口
var _ tool.Tool = (*ToolAdapter)(nil)
