package tool

import (
	"context"
	"fmt"
	"os"
	"strings"
)

type EditTool struct{ base string }

func NewEditTool(base string) *EditTool { return &EditTool{base: base} }

func (t *EditTool) Name() string        { return "edit" }
func (t *EditTool) Description() string { return "编辑文件（字符串替换）" }

func (t *EditTool) Schema() map[string]any {
	return objectSchema(map[string]any{
		"path":     stringSchema("文件路径"),
		"old_text": stringSchema("要替换的原文本（需唯一匹配）"),
		"new_text": stringSchema("替换后的新文本"),
	}, []string{"path", "old_text", "new_text"})
}

func (t *EditTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	path, err := getString(args, "path")
	if err != nil {
		return "", err
	}
	oldText, err := getString(args, "old_text")
	if err != nil {
		return "", err
	}
	newText, err := getString(args, "new_text")
	if err != nil {
		return "", err
	}

	if oldText == newText {
		return "", fmt.Errorf("新旧文本相同，无需修改")
	}
	if oldText == "" {
		return "", fmt.Errorf("old_text 不能为空")
	}

	absPath, err := safePath(t.base, path)
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("读取失败: %w", err)
	}

	content := string(data)
	count := strings.Count(content, oldText)
	if count == 0 {
		return "", fmt.Errorf("未找到匹配的 old_text（请确保包含足够上下文以唯一定位）")
	}
	if count > 1 {
		return "", fmt.Errorf("找到 %d 处匹配，请提供更多上下文以唯一定位", count)
	}

	newContent := strings.Replace(content, oldText, newText, 1)
	if err := os.WriteFile(absPath, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("写入失败: %w", err)
	}
	return fmt.Sprintf("已修改 %s（1 处替换）", path), nil
}
