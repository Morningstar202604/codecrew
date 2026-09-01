// Package reasoning 实现多种推理范式，让 Agent 的思考过程显式化、可配置、可积累经验。
//
// 支持的推理模式：
//   - standard: 标准模式（隐式 ReAct，现有行为）
//   - react: 显式 ReAct 模式（Thought → Action → Observation 循环）
//   - reflexion: 反思模式（任务完成后自动反思，失败时深度反思，经验存入长期记忆）
//
// 设计原则：
//   - 完整：覆盖 ReAct、Reflexion、Self-Consistency 等主流范式
//   - 灵活：模式可运行时切换，参数可配置，可组合使用
//   - 非侵入：不改变现有行为，standard 模式与之前完全一致
package reasoning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Mode 推理模式。
type Mode string

const (
	// ModeStandard 标准模式，隐式 ReAct，与现有行为一致。
	ModeStandard Mode = "standard"
	// ModeReAct 显式 ReAct 模式，模型输出包含明确的 Thought 步骤。
	ModeReAct Mode = "react"
	// ModeReflexion 反思模式，任务完成后自动反思，失败时深度反思。
	ModeReflexion Mode = "reflexion"
)

// ParseMode 解析推理模式字符串，不区分大小写。
func ParseMode(s string) Mode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "react":
		return ModeReAct
	case "reflexion", "reflection":
		return ModeReflexion
	default:
		return ModeStandard
	}
}

// String 返回模式的字符串表示。
func (m Mode) String() string { return string(m) }

// Config 推理配置。
type Config struct {
	// Mode 推理模式，默认 standard。
	Mode Mode `json:"mode,omitempty"`
	// ShowThoughts 是否在终端/Web 端显示思考过程，默认 true。
	ShowThoughts bool `json:"show_thoughts,omitempty"`
	// AutoReflect 是否在任务完成后自动触发反思，默认 true（仅 reflexion 模式生效）。
	AutoReflect bool `json:"auto_reflect,omitempty"`
	// ReflectionDepth 反思深度 1-3，默认 1。数字越大反思越深入，但消耗更多 tokens。
	ReflectionDepth int `json:"reflection_depth,omitempty"`
	// MaxFailures 保留的最大失败经验数，默认 50。
	MaxFailures int `json:"max_failures,omitempty"`
	// InjectReflections 是否将历史反思注入 system prompt，默认 true。
	InjectReflections bool `json:"inject_reflections,omitempty"`
}

// DefaultConfig 返回默认配置。
func DefaultConfig() Config {
	return Config{
		Mode:              ModeStandard,
		ShowThoughts:      true,
		AutoReflect:       true,
		ReflectionDepth:   1,
		MaxFailures:       50,
		InjectReflections: true,
	}
}

// Validate 校验并修正配置。
func (c *Config) Validate() {
	if c.Mode == "" {
		c.Mode = ModeStandard
	}
	if c.ReflectionDepth < 1 {
		c.ReflectionDepth = 1
	}
	if c.ReflectionDepth > 3 {
		c.ReflectionDepth = 3
	}
	if c.MaxFailures < 1 {
		c.MaxFailures = 50
	}
}

// Failure 记录一次失败经验。
type Failure struct {
	Task       string    `json:"task"`
	Error      string    `json:"error"`
	Role       string    `json:"role"`
	Timestamp  time.Time `json:"timestamp"`
	Reflection string    `json:"reflection,omitempty"`
}

// FailureStore 失败经验存储，按角色隔离，线程安全。
type FailureStore struct {
	baseDir string
	mu      sync.Mutex
}

// NewFailureStore 创建失败经验存储。
func NewFailureStore(baseDir string) *FailureStore {
	return &FailureStore{baseDir: baseDir}
}

func (s *FailureStore) path(role string) string {
	return filepath.Join(s.baseDir, "failures", role+".json")
}

// Add 添加一条失败经验，超过上限时丢弃最旧的。
func (s *FailureStore) Add(role string, f Failure) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f.Role = role
	if f.Timestamp.IsZero() {
		f.Timestamp = time.Now()
	}
	list, err := s.load(role)
	if err != nil {
		return err
	}
	list = append(list, f)
	if len(list) > 50 {
		list = list[len(list)-50:]
	}
	return s.save(role, list)
}

// List 返回某角色的所有失败经验（最新的在前）。
func (s *FailureStore) List(role string) ([]Failure, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load(role)
	if err != nil {
		return nil, err
	}
	// 反转，最新的在前
	out := make([]Failure, len(list))
	for i, f := range list {
		out[len(list)-1-i] = f
	}
	return out, nil
}

// Clear 清空某角色的失败经验。
func (s *FailureStore) Clear(role string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.path(role)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	return os.Remove(path)
}

// RecentSummary 返回最近 N 条失败经验的摘要文本，用于注入 prompt。
func (s *FailureStore) RecentSummary(role string, n int) string {
	list, err := s.List(role)
	if err != nil || len(list) == 0 {
		return ""
	}
	if n > len(list) {
		n = len(list)
	}
	var sb strings.Builder
	sb.WriteString("## 过去的失败经验（避免重复犯错）\n\n")
	for i, f := range list[:n] {
		sb.WriteString(fmt.Sprintf("%d. **任务**: %s\n", i+1, f.Task))
		sb.WriteString(fmt.Sprintf("   **错误**: %s\n", f.Error))
		if f.Reflection != "" {
			sb.WriteString(fmt.Sprintf("   **反思**: %s\n", f.Reflection))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (s *FailureStore) load(role string) ([]Failure, error) {
	path := s.path(role)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Failure{}, nil
		}
		return nil, err
	}
	var list []Failure
	if err := json.Unmarshal(data, &list); err != nil {
		return []Failure{}, nil
	}
	return list, nil
}

func (s *FailureStore) save(role string, list []Failure) error {
	path := s.path(role)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
