package tool

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// skipDirs 是 glob / grep 默认跳过的目录名。
var skipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, "node_modules": true,
	"vendor": true, "dist": true, "build": true, "target": true,
	"__pycache__": true, ".venv": true, "venv": true, ".idea": true,
	".vscode": true, ".next": true, ".cache": true,
}

// GlobTool 按通配模式列文件。
type GlobTool struct{ base string }

func NewGlobTool(base string) *GlobTool { return &GlobTool{base: base} }

func (t *GlobTool) Name() string { return "glob" }
func (t *GlobTool) Description() string {
	return "按通配模式查找文件，例如 **/*.go 或 internal/**/tool.go"
}

func (t *GlobTool) Schema() map[string]any {
	return objectSchema(map[string]any{
		"pattern": stringSchema("通配模式，支持 * ? ** 与 {a,b}"),
		"root":    stringSchema("搜索根目录，默认为工作目录"),
		"limit":   integerSchema("最多返回多少个文件，默认 200"),
	}, []string{"pattern"})
}

func (t *GlobTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	pattern, err := getString(args, "pattern")
	if err != nil {
		return "", err
	}
	re, err := CompileGlob(pattern)
	if err != nil {
		return "", fmt.Errorf("通配模式无效: %w", err)
	}
	root := t.base
	if s, ok := args["root"].(string); ok && strings.TrimSpace(s) != "" {
		if root, err = SafePath(t.base, s); err != nil {
			return "", err
		}
	}
	limit := getInt(args, "limit", 200)
	if limit <= 0 || limit > 2000 {
		limit = 200
	}

	var (
		hits []globHit
		scan int
	)
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		scan++
		if scan > 200000 {
			return filepath.SkipAll
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !re.MatchString(rel) && !re.MatchString(filepath.Base(rel)) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		hits = append(hits, globHit{path: rel, size: info.Size()})
		if len(hits) >= limit {
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil && walkErr != filepath.SkipAll {
		return "", fmt.Errorf("遍历目录失败: %w", walkErr)
	}
	if len(hits) == 0 {
		return fmt.Sprintf("没有文件匹配 %s（根目录 %s）", pattern, root), nil
	}
	sortStrings(hits)

	var sb strings.Builder
	fmt.Fprintf(&sb, "匹配 %d 个文件（模式 %s）:\n", len(hits), pattern)
	for _, h := range hits {
		fmt.Fprintf(&sb, "%8d  %s\n", h.size, h.path)
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// GrepTool 在文件内容中做正则搜索，带上下文行。
type GrepTool struct{ base string }

func NewGrepTool(base string) *GrepTool { return &GrepTool{base: base} }

func (t *GrepTool) Name() string { return "grep" }
func (t *GrepTool) Description() string {
	return "在工作目录内按正则搜索文件内容，返回 文件:行号:内容 及上下文"
}

func (t *GrepTool) Schema() map[string]any {
	return objectSchema(map[string]any{
		"pattern":          stringSchema("正则表达式（Go regexp 语法）"),
		"path":             stringSchema("只搜索该文件或目录，默认为工作目录"),
		"glob":             stringSchema("文件名过滤，例如 *.go"),
		"case_insensitive": map[string]any{"type": "boolean", "description": "是否忽略大小写，默认 false"},
		"context":          integerSchema("每个命中前后各展示多少行，默认 2，最大 10"),
		"max_results":      integerSchema("最多返回多少个命中，默认 100"),
	}, []string{"pattern"})
}

func (t *GrepTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	pattern, err := getString(args, "pattern")
	if err != nil {
		return "", err
	}
	re, err := compilePattern(pattern, args)
	if err != nil {
		return "", fmt.Errorf("正则无效: %w", err)
	}
	root := t.base
	if s, ok := args["path"].(string); ok && strings.TrimSpace(s) != "" {
		if root, err = SafePath(t.base, s); err != nil {
			return "", err
		}
	}
	var globRe *regexp.Regexp
	if g, ok := args["glob"].(string); ok && strings.TrimSpace(g) != "" {
		if globRe, err = CompileGlob(g); err != nil {
			return "", fmt.Errorf("glob 无效: %w", err)
		}
	}
	ctxLines := getInt(args, "context", 2)
	if ctxLines < 0 {
		ctxLines = 0
	}
	if ctxLines > 10 {
		ctxLines = 10
	}
	maxResults := getInt(args, "max_results", 100)
	if maxResults <= 0 || maxResults > 1000 {
		maxResults = 100
	}

	type result struct {
		file  string
		line  int
		block []string
	}
	var (
		results   []result
		info      []string
		fileCount int
	)

	walk := func(path string, d fs.DirEntry, err error) error {
		if err != nil || ctx.Err() != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return filepath.SkipAll
		default:
		}
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if globRe != nil && !globRe.MatchString(rel) && !globRe.MatchString(filepath.Base(rel)) {
			return nil
		}
		if stat, err := d.Info(); err != nil || stat.Size() > 2*1024*1024 {
			return nil
		}
		lines, ok := readLines(path)
		if !ok {
			return nil
		}
		fileCount++
		for i, line := range lines {
			if !re.MatchString(line) {
				continue
			}
			from, to := i-ctxLines, i+ctxLines
			if from < 0 {
				from = 0
			}
			if to >= len(lines) {
				to = len(lines) - 1
			}
			block := make([]string, 0, to-from+1)
			for n := from; n <= to; n++ {
				marker := "  "
				if n == i {
					marker = "->"
				}
				block = append(block, fmt.Sprintf("%s %4d | %s", marker, n+1, lines[n]))
			}
			results = append(results, result{file: rel, line: i + 1, block: block})
			if len(results) >= maxResults {
				info = append(info, fmt.Sprintf("已达 %d 条上限，仍有更多命中未展示（可缩小 glob 或提高 max_results）", maxResults))
				return filepath.SkipAll
			}
		}
		return nil
	}

	if stat, err := os.Stat(root); err != nil {
		return "", fmt.Errorf("搜索路径不存在: %s", root)
	} else if !stat.IsDir() {
		lines, ok := readLines(root)
		if !ok {
			return "", fmt.Errorf("无法读取文件: %s", root)
		}
		for i, line := range lines {
			if re.MatchString(line) {
				results = append(results, result{file: filepath.Base(root), line: i + 1, block: []string{fmt.Sprintf("-> %4d | %s", i+1, line)}})
			}
		}
		fileCount = 1
	} else if err := filepath.WalkDir(root, walk); err != nil && err != filepath.SkipAll {
		return "", fmt.Errorf("遍历失败: %w", err)
	}

	if len(results) == 0 {
		return fmt.Sprintf("没有命中 %q（已扫描 %d 个文件）", pattern, fileCount), nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%q 命中 %d 处（扫描 %d 个文件）:\n", pattern, len(results), fileCount)
	for _, r := range results {
		fmt.Fprintf(&sb, "\n%s:%d\n", r.file, r.line)
		sb.WriteString(strings.Join(r.block, "\n"))
	}
	for _, note := range info {
		fmt.Fprintf(&sb, "\n\n%s", note)
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

func compilePattern(pattern string, args map[string]any) (*regexp.Regexp, error) {
	if ci, ok := args["case_insensitive"].(bool); ok && ci {
		pattern = "(?i)" + pattern
	}
	return regexp.Compile(pattern)
}

func readLines(path string) ([]string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	if len(data) == 0 || strings.ContainsRune(string(data[:min(len(data), 4096)]), 0) {
		return nil, false // 空文件或二进制
	}
	return strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n"), true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type globHit struct {
	path string
	size int64
}

func sortStrings(hits []globHit) {
	sort.Slice(hits, func(i, j int) bool { return hits[i].path < hits[j].path })
}

// CompileGlob 把 shell 风格通配翻译成正则：** 跨目录，* 不跨目录，? 单字符，{a,b} 多选。
func CompileGlob(pattern string) (*regexp.Regexp, error) {
	var sb strings.Builder
	sb.WriteString("^")
	runes := []rune(pattern)
	for i := 0; i < len(runes); i++ {
		switch c := runes[i]; c {
		case '*':
			if i+1 < len(runes) && runes[i+1] == '*' {
				i++
				if i+1 < len(runes) && runes[i+1] == '/' {
					i++
					sb.WriteString("(?:.*/)?")
					continue
				}
				sb.WriteString(".*")
				continue
			}
			sb.WriteString("[^/]*")
		case '?':
			sb.WriteString("[^/]")
		case '{':
			end := i
			for end < len(runes) && runes[end] != '}' {
				end++
			}
			if end == len(runes) {
				sb.WriteString(`\{`)
				continue
			}
			options := strings.Split(string(runes[i+1:end]), ",")
			for k, o := range options {
				options[k] = regexp.QuoteMeta(strings.TrimSpace(o))
			}
			sb.WriteString("(?:" + strings.Join(options, "|") + ")")
			i = end
		case '.':
			sb.WriteString(`\.`)
		default:
			sb.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	sb.WriteString("$")
	return regexp.Compile(sb.String())
}
