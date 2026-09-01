// Package knowledge 实现代码库索引、语义检索和情景记忆。
//
// 让 Agent 能够理解整个项目，通过 RAG 检索相关代码片段，
// 并记住之前的任务执行情况，避免重复工作。
package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config 知识系统配置。
type Config struct {
	// Enabled 是否启用知识系统，默认 true。
	Enabled bool `json:"enabled,omitempty"`
	// AutoIndex 是否自动索引项目，默认 true。
	AutoIndex bool `json:"auto_index,omitempty"`
	// IndexInterval 索引更新间隔（小时），默认 24。
	IndexInterval int `json:"index_interval,omitempty"`
	// MaxResults 搜索最大返回结果数，默认 10。
	MaxResults int `json:"max_results,omitempty"`
	// ContextLines 搜索结果上下文行数，默认 3。
	ContextLines int `json:"context_lines,omitempty"`
	// ExcludeDirs 排除的目录列表。
	ExcludeDirs []string `json:"exclude_dirs,omitempty"`
	// ExcludeExts 排除的文件扩展名列表。
	ExcludeExts []string `json:"exclude_exts,omitempty"`
	// MaxFileSize 最大索引文件大小（字节），默认 1MB。
	MaxFileSize int64 `json:"max_file_size,omitempty"`
}

// DefaultConfig 返回默认配置。
func DefaultConfig() Config {
	return Config{
		Enabled:       true,
		AutoIndex:     true,
		IndexInterval: 24,
		MaxResults:    10,
		ContextLines:  3,
		ExcludeDirs:   []string{".git", "node_modules", "vendor", "dist", "build", ".idea", ".vscode", "__pycache__"},
		ExcludeExts:   []string{".exe", ".dll", ".so", ".dylib", ".bin", ".obj", ".class", ".jar", ".war", ".zip", ".tar", ".gz", ".png", ".jpg", ".jpeg", ".gif", ".ico", ".svg", ".woff", ".woff2", ".ttf", ".eot", ".mp3", ".mp4", ".avi", ".mov", ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx"},
		MaxFileSize:   1024 * 1024, // 1MB
	}
}

// GetMaxResults 返回最大返回结果数。
func (c Config) GetMaxResults() int {
	if c.MaxResults <= 0 {
		return 10
	}
	return c.MaxResults
}

// GetContextLines 返回上下文行数。
func (c Config) GetContextLines() int {
	if c.ContextLines <= 0 {
		return 3
	}
	return c.ContextLines
}

// GetMaxFileSize 返回最大文件大小。
func (c Config) GetMaxFileSize() int64 {
	if c.MaxFileSize <= 0 {
		return 1024 * 1024
	}
	return c.MaxFileSize
}

// shouldExclude 判断文件是否应该被排除。
func (c Config) shouldExclude(path string, info os.FileInfo) bool {
	// 检查目录
	for _, dir := range c.ExcludeDirs {
		if strings.Contains(path, string(filepath.Separator)+dir+string(filepath.Separator)) ||
			strings.HasPrefix(filepath.Base(path), dir) {
			return true
		}
	}
	// 检查扩展名
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range c.ExcludeExts {
		if ext == e {
			return true
		}
	}
	// 检查文件大小
	if info.Size() > c.GetMaxFileSize() {
		return true
	}
	return false
}

// FileInfo 索引中的文件信息。
type FileInfo struct {
	Path       string    `json:"path"`
	Language   string    `json:"language"`
	Size       int64     `json:"size"`
	Lines      int       `json:"lines"`
	ModifiedAt time.Time `json:"modified_at"`
	IndexedAt  time.Time `json:"indexed_at"`
	Symbols    []Symbol  `json:"symbols,omitempty"`
}

// Symbol 代码符号（函数、类型、常量等）。
type Symbol struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"` // func, type, const, var, import, class, method
	Line     int    `json:"line"`
	Receiver string `json:"receiver,omitempty"` // Go 方法的接收者
}

// SearchResult 搜索结果。
type SearchResult struct {
	File       string   `json:"file"`
	Score      float64  `json:"score"`
	Line       int      `json:"line"`
	Content    string   `json:"content"`
	Context    []string `json:"context,omitempty"`
	SymbolName string   `json:"symbol_name,omitempty"`
}

// EpisodicMemory 情景记忆条目。
type EpisodicMemory struct {
	ID        int       `json:"id"`
	Task      string    `json:"task"`
	Result    string    `json:"result"`
	Files     []string  `json:"files,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Success   bool      `json:"success"`
}
