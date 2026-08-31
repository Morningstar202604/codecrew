package tool

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadTool 读取文件内容，带行号，支持 offset/limit 分片。
type ReadTool struct{ base string }

func NewReadTool(base string) *ReadTool { return &ReadTool{base: base} }

func (t *ReadTool) Name() string { return "read" }
func (t *ReadTool) Description() string {
	return "读取文件内容（带行号）。大文件请用 offset/limit 分片读取"
}

func (t *ReadTool) Schema() map[string]any {
	return objectSchema(map[string]any{
		"path":   stringSchema("文件路径，相对工作目录或绝对路径"),
		"offset": integerSchema("起始行号（从 1 开始），默认 1"),
		"limit":  integerSchema("读取行数，0 表示全部"),
	}, []string{"path"})
}

func (t *ReadTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	path, err := getString(args, "path")
	if err != nil {
		return "", err
	}
	absPath, err := SafePath(t.base, path)
	if err != nil {
		return "", err
	}
	f, err := os.Open(absPath)
	if err != nil {
		return "", fmt.Errorf("读取失败: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s 是目录，请改用 glob 工具", path)
	}

	offset := getInt(args, "offset", 1)
	if offset < 1 {
		offset = 1
	}
	limit := getInt(args, "limit", 0)

	var (
		sb      strings.Builder
		scanner = bufio.NewScanner(f)
		lineNo  int
		shown   int
	)
	scanner.Buffer(make([]byte, 256*1024), 4*1024*1024)
	for scanner.Scan() {
		lineNo++
		if lineNo < offset {
			continue
		}
		if limit > 0 && shown >= limit {
			fmt.Fprintf(&sb, "... 后续还有 %d 行，可用 offset=%d 继续读取\n", lineNo-offset+1, lineNo)
			break
		}
		fmt.Fprintf(&sb, "%4d | %s\n", lineNo, scanner.Text())
		shown++
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("读取失败: %w", err)
	}
	if shown == 0 {
		return "", fmt.Errorf("文件 %s 只有 %d 行，offset=%d 已越过末尾", path, lineNo, offset)
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// WriteTool 新建或覆盖文件。
type WriteTool struct{ base string }

func NewWriteTool(base string) *WriteTool { return &WriteTool{base: base} }

func (t *WriteTool) Name() string { return "write" }
func (t *WriteTool) Description() string {
	return "写入文件（新建或整体覆盖）。修改已有文件请优先用 edit"
}

func (t *WriteTool) Schema() map[string]any {
	return objectSchema(map[string]any{
		"path":    stringSchema("文件路径"),
		"content": stringSchema("文件完整内容"),
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
	absPath, err := SafePath(t.base, path)
	if err != nil {
		return "", err
	}
	existed := PathExists(absPath)

	mode := os.FileMode(0o644)
	if info, err := os.Stat(absPath); err == nil {
		mode = info.Mode()
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return "", fmt.Errorf("创建目录失败: %w", err)
	}
	if err := os.WriteFile(absPath, []byte(content), mode); err != nil {
		return "", fmt.Errorf("写入失败: %w", err)
	}
	verb := "已创建"
	if existed {
		verb = "已覆盖"
	}
	return fmt.Sprintf("%s %s（%d 字节 / %d 行）", verb, RelativePath(t.base, absPath), len(content), strings.Count(content, "\n")+1), nil
}

// EditTool 做唯一匹配的字符串替换。
type EditTool struct{ base string }

func NewEditTool(base string) *EditTool { return &EditTool{base: base} }

func (t *EditTool) Name() string { return "edit" }
func (t *EditTool) Description() string {
	return "编辑文件：把 old_text 精确替换为 new_text，old_text 必须在文件中唯一匹配"
}

func (t *EditTool) Schema() map[string]any {
	return objectSchema(map[string]any{
		"path":     stringSchema("文件路径"),
		"old_text": stringSchema("要替换的原文本，需包含足够上下文以唯一定位"),
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
	if oldText == "" {
		return "", fmt.Errorf("old_text 不能为空")
	}
	if oldText == newText {
		return "", fmt.Errorf("新旧文本相同，无需修改")
	}

	absPath, err := SafePath(t.base, path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("读取失败: %w", err)
	}
	content := string(data)
	switch count := strings.Count(content, oldText); {
	case count == 0:
		return "", fmt.Errorf("未找到 old_text，请先用 read 确认文件当前内容")
	case count > 1:
		return "", fmt.Errorf("old_text 匹配到 %d 处，请扩大上下文使其唯一", count)
	}
	updated := strings.Replace(content, oldText, newText, 1)
	// 保留原文件权限
	mode := os.FileMode(0o644)
	if info, err := os.Stat(absPath); err == nil {
		mode = info.Mode()
	}
	if err := os.WriteFile(absPath, []byte(updated), mode); err != nil {
		return "", fmt.Errorf("写入失败: %w", err)
	}
	return fmt.Sprintf("已修改 %s（1 处替换，%d → %d 字节）", RelativePath(t.base, absPath), len(content), len(updated)), nil
}
