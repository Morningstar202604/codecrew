package knowledge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// EpisodicStore 情景记忆存储。
type EpisodicStore struct {
	mu       sync.RWMutex
	memories []EpisodicMemory
	nextID   int
	storeDir string
}

// NewEpisodicStore 创建情景记忆存储。
func NewEpisodicStore() *EpisodicStore {
	home, _ := os.UserHomeDir()
	storeDir := filepath.Join(home, ".codecrew", "episodic")
	os.MkdirAll(storeDir, 0o755)

	store := &EpisodicStore{
		storeDir: storeDir,
		nextID:   1,
	}
	store.load()
	return store
}

// Add 添加一条情景记忆。
func (s *EpisodicStore) Add(task, result string, success bool, files []string, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	mem := EpisodicMemory{
		ID:        s.nextID,
		Task:      task,
		Result:    result,
		Files:     files,
		SessionID: sessionID,
		Timestamp: time.Now(),
		Success:   success,
	}
	s.memories = append(s.memories, mem)
	s.nextID++

	// 只保留最近 100 条
	if len(s.memories) > 100 {
		s.memories = s.memories[len(s.memories)-100:]
	}

	s.save()
}

// List 返回所有情景记忆（最新在前）。
func (s *EpisodicStore) List() []EpisodicMemory {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]EpisodicMemory, len(s.memories))
	copy(out, s.memories)
	// 最新在前
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp.After(out[j].Timestamp)
	})
	return out
}

// Recent 返回最近 N 条记忆。
func (s *EpisodicStore) Recent(n int) []EpisodicMemory {
	all := s.List()
	if n > len(all) {
		n = len(all)
	}
	return all[:n]
}

// Search 搜索包含关键词的记忆。
func (s *EpisodicStore) Search(keyword string, limit int) []EpisodicMemory {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keyword = strings.ToLower(keyword)
	var results []EpisodicMemory
	for i := len(s.memories) - 1; i >= 0; i-- {
		mem := s.memories[i]
		if strings.Contains(strings.ToLower(mem.Task), keyword) ||
			strings.Contains(strings.ToLower(mem.Result), keyword) {
			results = append(results, mem)
			if len(results) >= limit {
				break
			}
		}
	}
	return results
}

// Clear 清空所有记忆。
func (s *EpisodicStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.memories = nil
	s.nextID = 1
	s.save()
}

// Count 返回记忆数量。
func (s *EpisodicStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.memories)
}

// InjectPrompt 生成情景记忆注入 prompt（最近 N 条任务摘要）。
func (s *EpisodicStore) InjectPrompt(n int) string {
	recent := s.Recent(n)
	if len(recent) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n## 最近执行的任务（情景记忆）\n\n")
	for _, mem := range recent {
		status := "✓"
		if !mem.Success {
			status = "✗"
		}
		files := ""
		if len(mem.Files) > 0 {
			files = fmt.Sprintf(" (涉及文件: %s)", strings.Join(mem.Files, ", "))
		}
		fmt.Fprintf(&sb, "- [%s] %s%s: %s\n",
			status,
			mem.Task,
			files,
			truncate(mem.Result, 100),
		)
	}
	sb.WriteString("\n参考以上历史任务，避免重复工作，借鉴成功经验。")
	return sb.String()
}

// load 从磁盘加载记忆。
func (s *EpisodicStore) load() {
	data, err := os.ReadFile(filepath.Join(s.storeDir, "episodic.json"))
	if err != nil {
		return
	}
	var saved struct {
		Memories []EpisodicMemory `json:"memories"`
		NextID   int              `json:"next_id"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		return
	}
	s.memories = saved.Memories
	s.nextID = saved.NextID
	if s.nextID <= 0 {
		s.nextID = 1
	}
}

// save 持久化记忆到磁盘。
func (s *EpisodicStore) save() {
	data := struct {
		Memories []EpisodicMemory `json:"memories"`
		NextID   int              `json:"next_id"`
	}{
		Memories: s.memories,
		NextID:   s.nextID,
	}
	if content, err := json.MarshalIndent(data, "", "  "); err == nil {
		os.WriteFile(filepath.Join(s.storeDir, "episodic.json"), content, 0o644)
	}
}

// truncate 截断字符串。
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
