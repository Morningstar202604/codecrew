package knowledge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// CodebaseIndex 代码库索引。
type CodebaseIndex struct {
	mu       sync.RWMutex
	cfg      Config
	rootDir  string
	indexDir string
	files    map[string]*FileInfo
	// 倒排索引：关键词 -> 文件路径 -> 出现次数
	inverted map[string]map[string]int
	// 文档频率：关键词 -> 出现的文件数
	docFreq map[string]int
	// 总文件数
	totalDocs int
	// 索引元信息
	meta IndexMeta
}

// IndexMeta 索引元信息。
type IndexMeta struct {
	RootDir     string    `json:"root_dir"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	FileCount   int       `json:"file_count"`
	SymbolCount int       `json:"symbol_count"`
}

// NewCodebaseIndex 创建代码库索引。
func NewCodebaseIndex(cfg Config, rootDir string) *CodebaseIndex {
	home, _ := os.UserHomeDir()
	indexDir := filepath.Join(home, ".codecrew", "index")
	os.MkdirAll(indexDir, 0o755)

	return &CodebaseIndex{
		cfg:      cfg,
		rootDir:  rootDir,
		indexDir: indexDir,
		files:    make(map[string]*FileInfo),
		inverted: make(map[string]map[string]int),
		docFreq:  make(map[string]int),
	}
}

// Build 构建索引（全量扫描）。
// indexTask 是并发索引的任务单元。
type indexTask struct {
	path    string
	info    os.FileInfo
	relPath string
}

// indexResult 是并发索引的结果。
type indexResult struct {
	fileInfo *FileInfo
	tokens   map[string]int
}

func (idx *CodebaseIndex) Build() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// 清空旧索引
	idx.files = make(map[string]*FileInfo)
	idx.inverted = make(map[string]map[string]int)
	idx.docFreq = make(map[string]int)

	// 第一阶段：收集所有文件
	var tasks []indexTask
	err := filepath.Walk(idx.rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			for _, dir := range idx.cfg.ExcludeDirs {
				if base == dir {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if idx.cfg.shouldExclude(path, info) {
			return nil
		}
		relPath, _ := filepath.Rel(idx.rootDir, path)
		tasks = append(tasks, indexTask{path: path, info: info, relPath: relPath})
		return nil
	})
	if err != nil {
		return fmt.Errorf("索引扫描失败: %w", err)
	}

	// 第二阶段：并发读取和索引（最多 8 个 worker）
	workerCount := 8
	if len(tasks) < workerCount {
		workerCount = len(tasks)
	}
	taskCh := make(chan indexTask, len(tasks))
	resultCh := make(chan indexResult, len(tasks))

	for w := 0; w < workerCount; w++ {
		go func() {
			for task := range taskCh {
				content, err := os.ReadFile(task.path)
				if err != nil {
					continue
				}
				text := string(content)
				fileInfo := &FileInfo{
					Path:       task.relPath,
					Language:   detectLanguage(task.path),
					Size:       task.info.Size(),
					Lines:      strings.Count(text, "\n") + 1,
					ModifiedAt: task.info.ModTime(),
					IndexedAt:  time.Now(),
					Symbols:    extractSymbols(text, task.relPath),
				}
				// 计算 token 频率（不修改共享状态）
				tokens := make(map[string]int)
				for _, tok := range tokenize(text) {
					tokens[tok]++
				}
				resultCh <- indexResult{fileInfo: fileInfo, tokens: tokens}
			}
		}()
	}

	for _, task := range tasks {
		taskCh <- task
	}
	close(taskCh)

	// 第三阶段：合并结果
	for i := 0; i < len(tasks); i++ {
		result := <-resultCh
		if result.fileInfo == nil {
			continue
		}
		idx.files[result.fileInfo.Path] = result.fileInfo
		for tok, count := range result.tokens {
			if idx.inverted[tok] == nil {
				idx.inverted[tok] = make(map[string]int)
			}
			idx.inverted[tok][result.fileInfo.Path] = count
			idx.docFreq[tok]++
		}
	}

	idx.totalDocs = len(idx.files)
	idx.meta = IndexMeta{
		RootDir:     idx.rootDir,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		FileCount:   len(idx.files),
		SymbolCount: idx.countSymbols(),
	}

	// 持久化
	idx.save()

	return nil
}

// indexFile 为单个文件构建倒排索引。
func (idx *CodebaseIndex) indexFile(path string, content string) {
	words := tokenize(content)
	wordCount := make(map[string]int)
	for _, w := range words {
		wordCount[w]++
	}

	for word, count := range wordCount {
		if idx.inverted[word] == nil {
			idx.inverted[word] = make(map[string]int)
		}
		idx.inverted[word][path] += count
	}
}

// Update 更新单个文件的索引（增量更新）。
func (idx *CodebaseIndex) Update(path string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	fullPath := filepath.Join(idx.rootDir, path)
	info, err := os.Stat(fullPath)
	if err != nil {
		// 文件被删除，从索引中移除
		delete(idx.files, path)
		idx.removeFromInverted(path)
		return nil
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return err
	}

	// 移除旧索引
	idx.removeFromInverted(path)

	// 添加新索引
	relPath, _ := filepath.Rel(idx.rootDir, fullPath)
	fileInfo := &FileInfo{
		Path:       relPath,
		Language:   detectLanguage(fullPath),
		Size:       info.Size(),
		Lines:      strings.Count(string(content), "\n") + 1,
		ModifiedAt: info.ModTime(),
		IndexedAt:  time.Now(),
		Symbols:    extractSymbols(string(content), relPath),
	}
	idx.files[relPath] = fileInfo
	idx.indexFile(relPath, string(content))
	idx.totalDocs = len(idx.files)
	idx.meta.UpdatedAt = time.Now()
	idx.meta.FileCount = len(idx.files)
	idx.meta.SymbolCount = idx.countSymbols()

	return nil
}

// removeFromInverted 从倒排索引中移除文件。
func (idx *CodebaseIndex) removeFromInverted(path string) {
	for word, files := range idx.inverted {
		delete(files, path)
		if len(files) == 0 {
			delete(idx.inverted, word)
		}
	}
}

// IsStale 检查索引是否过期。
func (idx *CodebaseIndex) IsStale() bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if len(idx.files) == 0 {
		return true
	}
	interval := time.Duration(idx.cfg.IndexInterval) * time.Hour
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	return time.Since(idx.meta.UpdatedAt) > interval
}

// Meta 返回索引元信息。
func (idx *CodebaseIndex) Meta() IndexMeta {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.meta
}

// Files 返回所有索引文件。
func (idx *CodebaseIndex) Files() []FileInfo {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	out := make([]FileInfo, 0, len(idx.files))
	for _, f := range idx.files {
		out = append(out, *f)
	}
	return out
}

// GetFile 获取单个文件信息。
func (idx *CodebaseIndex) GetFile(path string) *FileInfo {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if f, ok := idx.files[path]; ok {
		return f
	}
	return nil
}

// countSymbols 统计符号总数。
func (idx *CodebaseIndex) countSymbols() int {
	count := 0
	for _, f := range idx.files {
		count += len(f.Symbols)
	}
	return count
}

// save 持久化索引到磁盘。
func (idx *CodebaseIndex) save() {
	data := struct {
		Meta      IndexMeta                 `json:"meta"`
		Files     map[string]*FileInfo      `json:"files"`
		Inverted  map[string]map[string]int `json:"inverted"`
		DocFreq   map[string]int            `json:"doc_freq"`
		TotalDocs int                       `json:"total_docs"`
	}{
		Meta:      idx.meta,
		Files:     idx.files,
		Inverted:  idx.inverted,
		DocFreq:   idx.docFreq,
		TotalDocs: idx.totalDocs,
	}

	indexFile := filepath.Join(idx.indexDir, sanitizePath(idx.rootDir)+".json")
	if content, err := json.MarshalIndent(data, "", "  "); err == nil {
		os.WriteFile(indexFile, content, 0o644)
	}
}

// Load 从磁盘加载索引。
func (idx *CodebaseIndex) Load() bool {
	indexFile := filepath.Join(idx.indexDir, sanitizePath(idx.rootDir)+".json")
	data, err := os.ReadFile(indexFile)
	if err != nil {
		return false
	}

	var saved struct {
		Meta      IndexMeta                 `json:"meta"`
		Files     map[string]*FileInfo      `json:"files"`
		Inverted  map[string]map[string]int `json:"inverted"`
		DocFreq   map[string]int            `json:"doc_freq"`
		TotalDocs int                       `json:"total_docs"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		return false
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.meta = saved.Meta
	idx.files = saved.Files
	idx.inverted = saved.Inverted
	idx.docFreq = saved.DocFreq
	idx.totalDocs = saved.TotalDocs
	return true
}

// detectLanguage 根据文件扩展名检测语言。
func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".jsx", ".mjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "cpp"
	case ".rs":
		return "rust"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".swift":
		return "swift"
	case ".kt", ".kts":
		return "kotlin"
	case ".sh", ".bash":
		return "shell"
	case ".sql":
		return "sql"
	case ".html", ".htm":
		return "html"
	case ".css", ".scss", ".less":
		return "css"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".md":
		return "markdown"
	default:
		return "unknown"
	}
}

// extractSymbols 从文件内容中提取符号（函数、类型、常量等）。
func extractSymbols(content, path string) []Symbol {
	var symbols []Symbol
	lang := detectLanguage(path)

	switch lang {
	case "go":
		symbols = extractGoSymbols(content)
	case "python":
		symbols = extractPythonSymbols(content)
	case "javascript", "typescript":
		symbols = extractJSSymbols(content)
	default:
		// 通用：提取看起来像函数定义的行
		symbols = extractGenericSymbols(content)
	}

	return symbols
}

// extractGoSymbols 提取 Go 符号。
func extractGoSymbols(content string) []Symbol {
	var symbols []Symbol
	lines := strings.Split(content, "\n")

	funcRegex := regexp.MustCompile(`^func\s+(\([^)]+\)\s+)?(\w+)\s*\(`)
	typeRegex := regexp.MustCompile(`^type\s+(\w+)\s+`)
	constRegex := regexp.MustCompile(`^const\s+(\w+)\s+`)
	varRegex := regexp.MustCompile(`^var\s+(\w+)\s+`)

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if m := funcRegex.FindStringSubmatch(line); m != nil {
			receiver := ""
			if m[1] != "" {
				receiver = strings.Trim(m[1], "() ")
			}
			symbols = append(symbols, Symbol{
				Name:     m[2],
				Kind:     "func",
				Line:     i + 1,
				Receiver: receiver,
			})
		}
		if m := typeRegex.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: "type", Line: i + 1})
		}
		if m := constRegex.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: "const", Line: i + 1})
		}
		if m := varRegex.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: "var", Line: i + 1})
		}
	}
	return symbols
}

// extractPythonSymbols 提取 Python 符号。
func extractPythonSymbols(content string) []Symbol {
	var symbols []Symbol
	lines := strings.Split(content, "\n")
	classRegex := regexp.MustCompile(`^class\s+(\w+)`)
	funcRegex := regexp.MustCompile(`^\s*def\s+(\w+)\s*\(`)

	for i, line := range lines {
		if m := classRegex.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: "class", Line: i + 1})
		}
		if m := funcRegex.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: "func", Line: i + 1})
		}
	}
	return symbols
}

// extractJSSymbols 提取 JS/TS 符号。
func extractJSSymbols(content string) []Symbol {
	var symbols []Symbol
	lines := strings.Split(content, "\n")
	classRegex := regexp.MustCompile(`^class\s+(\w+)`)
	funcRegex := regexp.MustCompile(`^(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*\(`)
	arrowRegex := regexp.MustCompile(`^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?\(`)

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if m := classRegex.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: "class", Line: i + 1})
		}
		if m := funcRegex.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: "func", Line: i + 1})
		}
		if m := arrowRegex.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: "func", Line: i + 1})
		}
	}
	return symbols
}

// extractGenericSymbols 通用符号提取。
func extractGenericSymbols(content string) []Symbol {
	var symbols []Symbol
	lines := strings.Split(content, "\n")
	funcRegex := regexp.MustCompile(`(?i)^\s*(?:function|func|def|fn)\s+(\w+)`)

	for i, line := range lines {
		if m := funcRegex.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: "func", Line: i + 1})
		}
	}
	return symbols
}

// sanitizePath 将路径转换为安全的文件名。
func sanitizePath(path string) string {
	path = strings.ReplaceAll(path, "/", "_")
	path = strings.ReplaceAll(path, "\\", "_")
	path = strings.ReplaceAll(path, ":", "_")
	path = strings.ReplaceAll(path, " ", "_")
	if len(path) > 100 {
		path = path[:100]
	}
	return path
}
