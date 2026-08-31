// Package memory 提供角色长期记忆：每个角色一份 Markdown 笔记，持久化到磁盘，
// 启动时自动注入到该角色的 system prompt 末尾。架构师记决策、测试员记坑点，等等。
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Store 负责角色记忆的读写。记忆以纯 Markdown 文本存储，便于人工编辑与 grep。
type Store struct{ dir string }

// DefaultStore 返回 ~/.codecrew/memory。目录不存在时自动创建。
func DefaultStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("无法定位用户目录: %w", err)
	}
	return NewStore(filepath.Join(home, ".codecrew", "memory"))
}

// NewStore 在指定目录创建记忆存储。
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建记忆目录失败: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Dir 返回记忆存储目录。
func (s *Store) Dir() string { return s.dir }

// path 返回某角色的记忆文件路径。
func (s *Store) path(role string) string {
	return filepath.Join(s.dir, sanitize(role)+".md")
}

// Path 暴露角色记忆文件路径，供展示用。
func (s *Store) Path(role string) string { return s.path(role) }

// Load 读取某角色的全部记忆文本。文件不存在时返回空字符串（不算错误）。
func (s *Store) Load(role string) (string, error) {
	data, err := os.ReadFile(s.path(role))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("读取记忆失败: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// Append 追加一条笔记。自动带时间戳前缀，空笔记忽略。
func (s *Store) Append(role, note string) error {
	note = strings.TrimSpace(note)
	if note == "" {
		return fmt.Errorf("笔记内容不能为空")
	}
	existing, err := s.Load(role)
	if err != nil {
		return err
	}
	stamp := time.Now().Format("2006-01-02 15:04")
	entry := fmt.Sprintf("- [%s] %s", stamp, note)
	var content string
	if existing == "" {
		content = "# " + role + " 的长期记忆\n\n" + entry + "\n"
	} else {
		content = existing + "\n" + entry + "\n"
	}
	if err := os.WriteFile(s.path(role), []byte(content), 0o644); err != nil {
		return fmt.Errorf("写入记忆失败: %w", err)
	}
	return nil
}

// Clear 清空某角色的全部记忆（删除文件）。
func (s *Store) Clear(role string) error {
	p := s.path(role)
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("清空记忆失败: %w", err)
	}
	return nil
}

// List 返回有记忆文件的角色名列表（按文件名排序）。
func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		out = append(out, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(out)
	return out, nil
}

// InjectPrompt 把记忆注入到 system prompt 末尾。记忆为空时返回原 prompt。
// 注入格式：在 prompt 后追加一段「以下是你在本项目中积累的长期记忆…」。
func InjectPrompt(prompt, memory string) string {
	memory = strings.TrimSpace(memory)
	if memory == "" {
		return prompt
	}
	return prompt + "\n\n---\n## 长期记忆（本角色在过往项目中积累的笔记，务必参考）\n\n" + memory + "\n"
}

// sanitize 清理角色名中不适合做文件名的字符。允许 Unicode 字母和数字，
// 只替换路径分隔符、控制字符等真正危险的字符。
func sanitize(name string) string {
	name = strings.TrimSpace(name)
	var sb strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			sb.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			sb.WriteRune(r)
		default:
			sb.WriteRune('_')
		}
	}
	out := sb.String()
	// 去掉首尾的点和下划线（避免隐藏文件或空文件名）
	out = strings.Trim(out, "._")
	if out == "" {
		out = "unnamed"
	}
	return out
}
