package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Tool 是所有可被模型调用的能力的最小契约。
type Tool interface {
	Name() string
	Description() string
	// Schema 返回 JSON Schema 形式的参数定义（object 类型），由注册表包装成 OpenAI 工具格式。
	Schema() map[string]any
	Execute(ctx context.Context, args map[string]any) (string, error)
}

// MaxOutputLines 限制回填给模型的输出行数，避免单次工具结果吃光上下文。
const MaxOutputLines = 400

func jsonSchema(typ string, properties map[string]any, required []string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	if required == nil {
		required = []string{}
	}
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

func integerSchema(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
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

func getInt(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return def
	}
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

// SafePath 把模型给的路径解析到 base 之下，拒绝越界与相对逃逸。
// base 为空表示不限制目录（由调用方自行承担风险，bash 的 cwd 即属此类）。
// 会解析符号链接用于越界检查，但返回原始绝对路径（保持路径语义一致）。
func SafePath(base, p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("路径不能为空")
	}
	if base == "" {
		return filepath.Abs(p)
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	target := p
	if !filepath.IsAbs(target) {
		// 相对路径以工作目录为基准，而不是进程当前目录
		target = filepath.Join(absBase, target)
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	// 解析符号链接用于越界检查（防止通过 symlink 逃逸）
	// 解析 base 的符号链接
	resolvedBase := absBase
	if rb, err := filepath.EvalSymlinks(absBase); err == nil {
		resolvedBase = rb
	}
	// 解析目标路径的符号链接（文件存在时）
	resolvedAbs := abs
	if ra, err := filepath.EvalSymlinks(abs); err == nil {
		resolvedAbs = ra
	} else {
		// 文件不存在时，解析父目录的符号链接
		parent := filepath.Dir(abs)
		if rp, err := filepath.EvalSymlinks(parent); err == nil {
			resolvedAbs = filepath.Join(rp, filepath.Base(abs))
		}
	}
	// 用解析后的路径做越界检查
	rel, err := filepath.Rel(resolvedBase, resolvedAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("路径超出工作目录范围: %s（工作目录 %s）", p, absBase)
	}
	// 返回原始绝对路径（未解析符号链接），保持路径语义一致
	return abs, nil
}

// RelativePath 给出用于展示的相对路径，失败时退回原值。
func RelativePath(base, abs string) string {
	if rel, err := filepath.Rel(base, abs); err == nil && len(rel) < len(abs) {
		return filepath.ToSlash(rel)
	}
	return abs
}

// FormatOutput 截断过长输出，保留头部并说明丢了多少行。
func FormatOutput(output string, maxLines int) string {
	if maxLines <= 0 {
		maxLines = MaxOutputLines
	}
	trimmed := strings.TrimRight(output, "\n")
	lines := strings.Split(trimmed, "\n")
	if len(lines) <= maxLines {
		return trimmed
	}
	kept := strings.Join(lines[:maxLines], "\n")
	return fmt.Sprintf("%s\n... 输出过长，已截断 %d/%d 行", kept, len(lines)-maxLines, len(lines))
}

// PathExists 判断路径是否存在。
func PathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
