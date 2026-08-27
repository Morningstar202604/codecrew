package tool

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	Execute(ctx context.Context, args map[string]any) (string, error)
}

type Result struct {
	Output string
	Error  string
}

func Execute(ctx context.Context, t Tool, args map[string]any) Result {
	output, err := t.Execute(ctx, args)
	if err != nil {
		return Result{Error: err.Error()}
	}
	return Result{Output: output}
}

func jsonSchema(typ string, properties map[string]any, required []string) map[string]any {
	return map[string]any{
		"type":       typ,
		"properties": properties,
		"required":   required,
	}
}

func objectSchema(props map[string]any, req []string) map[string]any {
	return jsonSchema("object", props, req)
}

func stringSchema(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func getString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("缺少参数: %s", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("参数 %s 必须是字符串", key)
	}
	return s, nil
}

func getBool(args map[string]any, key string) (bool, error) {
	v, ok := args[key]
	if !ok {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("参数 %s 必须是布尔值", key)
	}
	return b, nil
}

func safePath(base, p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("路径不能为空")
	}
	abs, err := filepath.Abs(filepath.Join(base, p))
	if err != nil {
		return "", err
	}
	absBase, _ := filepath.Abs(base)
	if !strings.HasPrefix(abs, absBase) {
		return "", fmt.Errorf("路径超出允许范围: %s", p)
	}
	return abs, nil
}

func formatOutput(output string, maxLines int) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) > maxLines {
		return strings.Join(lines[:maxLines], "\n") + fmt.Sprintf("\n... (共 %d 行，已截断)", len(lines))
	}
	return output
}

var DefaultTimeout = 30 * time.Second
