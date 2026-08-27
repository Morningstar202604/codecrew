package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type WriteTool struct{ base string }

func NewWriteTool(base string) *WriteTool { return &WriteTool{base: base} }

func (t *WriteTool) Name() string        { return "write" }
func (t *WriteTool) Description() string { return "写入文件（新建或覆盖）" }

func (t *WriteTool) Schema() map[string]any {
	return objectSchema(map[string]any{
		"path":    stringSchema("文件路径"),
		"content": stringSchema("文件内容"),
	}, []string{"path", "content"})
}

func (t *WriteTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	path, err := getString(args, "path")
	if err != nil {
		return "", err
	}
	content, err := getString(args, "content")
	if err != nil {
		return "", err
	}

	absPath, err := safePath(t.base, path)
	if err != nil {
		return "", err
	}

	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("创建目录失败: %w", err)
	}

	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("写入失败: %w", err)
	}
	return fmt.Sprintf("已写入 %s (%d 字节)", path, len(content)), nil
}
