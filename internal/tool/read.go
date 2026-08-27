package tool

import (
	"context"
	"fmt"
	"os"
	"strings"
)

type ReadTool struct{ base string }

func NewReadTool(base string) *ReadTool { return &ReadTool{base: base} }

func (t *ReadTool) Name() string        { return "read" }
func (t *ReadTool) Description() string { return "读取文件内容" }

func (t *ReadTool) Schema() map[string]any {
	return objectSchema(map[string]any{
		"path":   stringSchema("文件路径，相对路径或绝对路径"),
		"offset": map[string]any{"type": "integer", "description": "起始行号（从 0 开始）"},
		"limit":  map[string]any{"type": "integer", "description": "读取行数，0 表示全部"},
	}, []string{"path"})
}

func (t *ReadTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	path, err := getString(args, "path")
	if err != nil {
		return "", err
	}
	absPath, err := safePath(t.base, path)
	if err != nil {
		return "", err
	}

	offset := 0
	if v, ok := args["offset"]; ok {
		if f, ok := v.(float64); ok {
			offset = int(f)
		}
	}
	limit := 0
	if v, ok := args["limit"]; ok {
		if f, ok := v.(float64); ok {
			limit = int(f)
		}
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("读取失败: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	start := offset
	if start < 0 {
		start = 0
	}
	if start >= len(lines) {
		return "", fmt.Errorf("偏移量超出文件长度")
	}
	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}

	var sb strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&sb, "%4d | %s\n", i+1, lines[i])
	}
	return sb.String(), nil
}
