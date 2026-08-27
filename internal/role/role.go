package role

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Role 是一个可切换的角色定义。
type Role struct {
	Name        string
	Description string
	Tools       []string
	Prompt      string
	Builtin     bool // 来自内置默认集还是磁盘覆盖
}

//go:embed defaults/*.md
var builtinFS embed.FS

// Load 读取角色集合：内置默认为底，再按顺序用各目录中的 .md 覆盖同名角色。
// 目录不存在不算错误——这意味着 go install 之后开箱即用。
func Load(dirs ...string) ([]Role, error) {
	roles := map[string]Role{}

	err := fs.WalkDir(builtinFS, "defaults", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		data, err := builtinFS.ReadFile(path)
		if err != nil {
			return err
		}
		r, err := Parse(filepath.Base(path), data)
		if err != nil {
			return fmt.Errorf("内置角色 %s: %w", path, err)
		}
		r.Builtin = true
		roles[r.Name] = r
		return nil
	})
	if err != nil {
		return nil, err
	}

	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := applyDir(dir, roles); err != nil {
			return nil, err
		}
	}

	out := make([]Role, 0, len(roles))
	for _, r := range roles {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if len(out) == 0 {
		return nil, fmt.Errorf("没有可用角色，请检查 roles/ 目录下的 .md 文件")
	}
	return out, nil
}

// applyDir 把目录下的 .md 角色合入 roles（同名覆盖）。目录不存在时静默跳过。
func applyDir(dir string, roles map[string]Role) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取角色目录 %s 失败: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("角色 %s 读取失败: %w", name, err)
		}
		r, err := Parse(name, data)
		if err != nil {
			return fmt.Errorf("角色 %s: %w", name, err)
		}
		roles[r.Name] = r
	}
	return nil
}

// Get 按名称查找角色。
func Get(roles []Role, name string) (Role, bool) {
	for _, r := range roles {
		if r.Name == name {
			return r, true
		}
	}
	return Role{}, false
}

// Parse 解析 frontmatter + Markdown 角色文件。filename 仅用于报错定位。
func Parse(filename string, data []byte) (Role, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return Role{}, fmt.Errorf("%s 缺少 YAML frontmatter（首行应为 ---）", filename)
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return Role{}, fmt.Errorf("%s 的 frontmatter 没有结束标记 ---", filename)
	}

	r := Role{}
	for _, line := range lines[1:end] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, found := strings.Cut(trimmed, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "name":
			r.Name = strings.Trim(value, `"'`)
		case "description":
			r.Description = strings.Trim(value, `"'`)
		case "tools":
			r.Tools = parseList(value)
		}
	}
	if r.Name == "" {
		r.Name = strings.TrimSuffix(filename, ".md")
	}
	if r.Description == "" {
		r.Description = "未提供角色说明"
	}
	r.Prompt = strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	if r.Prompt == "" {
		return Role{}, fmt.Errorf("%s 的正文为空，至少需要一段系统提示词", filename)
	}
	return r, nil
}

func parseList(value string) []string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.Trim(strings.TrimSpace(item), `"'`); item != "" {
			out = append(out, item)
		}
	}
	return out
}
