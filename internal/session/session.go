package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"codecrew/internal/llm"
)

// Meta 描述一个会话的基本信息。
type Meta struct {
	ID        string    `json:"id"`
	Path      string    `json:"-"`
	Role      string    `json:"role"`
	Model     string    `json:"model"`
	WorkDir   string    `json:"work_dir"`
	CreatedAt time.Time `json:"created_at"`
	Messages  int       `json:"messages"`
	Preview   string    `json:"preview"`
}

// Store 负责会话的落盘与读取（JSONL，一行一条消息）。
type Store struct{ dir string }

// DefaultStore 返回 ~/.codecrew/sessions。
func DefaultStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("无法定位用户目录: %w", err)
	}
	return NewStore(filepath.Join(home, ".codecrew", "sessions"))
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建会话目录失败: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Dir 返回会话存储目录。
func (s *Store) Dir() string { return s.dir }

// New 开一个新会话文件。
func (s *Store) New(meta Meta) (*Session, error) {
	if meta.ID == "" {
		now := time.Now()
		// 带上纳秒后缀，避免同一秒内新建的会话互相覆盖
		meta.ID = now.Format("20060102-150405") + fmt.Sprintf("%06d", now.Nanosecond()/1000)
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now()
	}
	path := filepath.Join(s.dir, meta.ID+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("创建会话文件失败: %w", err)
	}
	meta.Messages = 0
	head, _ := json.Marshal(meta)
	if _, err := f.Write(append(head, '\n')); err != nil {
		f.Close()
		return nil, err
	}
	return &Session{meta: meta, path: path, f: f}, nil
}

// Open 以追加方式打开已有会话（续聊用），不截断原文件。
func (s *Store) Open(id string) (*Session, error) {
	path := filepath.Join(s.dir, id+".jsonl")
	meta, messages, err := s.read(path)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("打开会话文件失败: %w", err)
	}
	meta.Messages = len(messages)
	return &Session{meta: meta, path: path, f: f}, nil
}

// Session 是一个打开的会话，支持追加写入。
type Session struct {
	meta Meta
	path string
	f    *os.File
}

// Meta 返回会话元信息副本。
func (s *Session) Meta() Meta { return s.meta }

// Path 返回会话文件路径。
func (s *Session) Path() string { return s.path }

// Append 追加一条消息（忽略空消息）。返回写入错误，调用方应检查。
func (s *Session) Append(m llm.Message) error {
	if s == nil || s.f == nil {
		return nil
	}
	if m.Role == "" || (m.Content == "" && len(m.ToolCalls) == 0) {
		return nil
	}
	data, err := json.Marshal(newRecord(m))
	if err != nil {
		return fmt.Errorf("序列化消息失败: %w", err)
	}
	if _, err := s.f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("写入会话失败: %w", err)
	}
	s.meta.Messages++
	if s.meta.Preview == "" && m.Role == "user" {
		s.meta.Preview = truncate(m.Content, 60)
	}
	return nil
}

// Flush 落盘。
func (s *Session) Flush() {
	if s != nil && s.f != nil {
		_ = s.f.Sync()
	}
}

// Close 关闭文件。
func (s *Session) Close() {
	if s != nil && s.f != nil {
		_ = s.f.Sync()
		_ = s.f.Close()
		s.f = nil
	}
}

// record 是会话文件里的一行。不能用内嵌结构体：Go 会把匿名字段当成嵌套对象序列化。
type record struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []llm.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
	At         time.Time      `json:"at"`
}

func newRecord(m llm.Message) record {
	return record{Role: m.Role, Content: m.Content, ToolCalls: m.ToolCalls, ToolCallID: m.ToolCallID, Name: m.Name, At: time.Now()}
}

func (r record) message() llm.Message {
	return llm.Message{Role: r.Role, Content: r.Content, ToolCalls: r.ToolCalls, ToolCallID: r.ToolCallID, Name: r.Name}
}

// List 返回全部会话，按时间倒序。
func (s *Store) List() ([]Meta, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var out []Meta
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		meta, _, err := s.read(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Load 按 ID 读取会话消息。
func (s *Store) Load(id string) (Meta, []llm.Message, error) {
	path := filepath.Join(s.dir, id+".jsonl")
	if _, err := os.Stat(path); err != nil {
		candidates, _ := s.List()
		for _, c := range candidates {
			if strings.Contains(c.ID, id) || c.Preview == id {
				path = filepath.Join(s.dir, c.ID+".jsonl")
				break
			}
		}
	}
	return s.read(path)
}

func (s *Store) read(path string) (Meta, []llm.Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return Meta{}, nil, err
	}
	defer f.Close()

	var meta Meta
	var messages []llm.Message
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if len(messages) == 0 && strings.Contains(line, "\"created_at\"") {
			_ = json.Unmarshal([]byte(line), &meta)
			continue
		}
		var rec record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		messages = append(messages, rec.message())
	}
	if err := scanner.Err(); err != nil {
		return meta, messages, err
	}
	if meta.ID == "" {
		meta.ID = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	}
	meta.Path = path
	if meta.Messages == 0 {
		meta.Messages = len(messages)
	}
	if meta.Preview == "" {
		for _, m := range messages {
			if m.Role == "user" {
				meta.Preview = truncate(m.Content, 60)
				break
			}
		}
	}
	if meta.CreatedAt.IsZero() {
		if info, err := os.Stat(path); err == nil {
			meta.CreatedAt = info.ModTime()
		}
	}
	return meta, messages, nil
}

func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
