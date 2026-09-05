package knowledge

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// BM25 参数
const (
	bm25k1 = 1.5
	bm25b  = 0.75
)

// Searcher 语义搜索器（基于 BM25 算法）。
type Searcher struct {
	idx *CodebaseIndex
}

// NewSearcher 创建搜索器。
func NewSearcher(idx *CodebaseIndex) *Searcher {
	return &Searcher{idx: idx}
}

// Search 执行搜索，返回最相关的结果。
func (s *Searcher) Search(query string, maxResults int) []SearchResult {
	if maxResults <= 0 {
		maxResults = s.idx.cfg.GetMaxResults()
	}

	queryWords := tokenize(query)
	if len(queryWords) == 0 {
		return nil
	}

	s.idx.mu.RLock()
	defer s.idx.mu.RUnlock()

	if s.idx.totalDocs == 0 {
		return nil
	}

	// 计算每个文件的 BM25 分数
	scores := make(map[string]float64)
	avgDocLen := s.computeAvgDocLen()

	for _, word := range queryWords {
		files, ok := s.idx.inverted[word]
		if !ok {
			continue
		}

		// IDF
		df := len(files)
		idf := math.Log(1 + (float64(s.idx.totalDocs-df)+0.5)/(float64(df)+0.5))

		for path, tf := range files {
			docLen := s.getDocLength(path)
			// BM25 分数
			numerator := float64(tf) * (bm25k1 + 1)
			denominator := float64(tf) + bm25k1*(1-bm25b+bm25b*float64(docLen)/avgDocLen)
			scores[path] += idf * numerator / denominator
		}
	}

	// 排序
	type fileScore struct {
		path  string
		score float64
	}
	var ranked []fileScore
	for path, score := range scores {
		if score > 0 {
			ranked = append(ranked, fileScore{path, score})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].score > ranked[j].score
	})

	// 生成结果
	var results []SearchResult
	for i, fs := range ranked {
		if i >= maxResults {
			break
		}
		result := s.buildResult(fs.path, fs.score, queryWords)
		if result != nil {
			results = append(results, *result)
		}
	}

	return results
}

// SearchBySymbol 按符号名搜索。
func (s *Searcher) SearchBySymbol(symbolName string, maxResults int) []SearchResult {
	if maxResults <= 0 {
		maxResults = s.idx.cfg.GetMaxResults()
	}

	s.idx.mu.RLock()
	defer s.idx.mu.RUnlock()

	var results []SearchResult
	symbolLower := strings.ToLower(symbolName)

	for path, file := range s.idx.files {
		for _, sym := range file.Symbols {
			if strings.Contains(strings.ToLower(sym.Name), symbolLower) {
				result := s.buildResultWithSymbol(path, sym)
				if result != nil {
					results = append(results, *result)
				}
				break // 每个文件只返回一个结果
			}
		}
	}

	// 按符号名匹配度排序
	sort.Slice(results, func(i, j int) bool {
		return len(results[i].SymbolName) < len(results[j].SymbolName)
	})

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return results
}

// buildResult 构建搜索结果（找到最佳匹配行）。
func (s *Searcher) buildResult(path string, score float64, queryWords []string) *SearchResult {
	fullPath := filepath.Join(s.idx.rootDir, path)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil
	}

	lines := strings.Split(string(content), "\n")
	bestLine := -1
	bestMatch := 0

	// 找到匹配最多关键词的行
	for i, line := range lines {
		lineLower := strings.ToLower(line)
		matchCount := 0
		for _, word := range queryWords {
			if strings.Contains(lineLower, word) {
				matchCount++
			}
		}
		if matchCount > bestMatch {
			bestMatch = matchCount
			bestLine = i
		}
	}

	if bestLine < 0 {
		bestLine = 0
	}

	// 构建上下文
	contextLines := s.idx.cfg.GetContextLines()
	context := []string{}
	start := bestLine - contextLines
	if start < 0 {
		start = 0
	}
	end := bestLine + contextLines + 1
	if end > len(lines) {
		end = len(lines)
	}
	for i := start; i < end; i++ {
		context = append(context, lines[i])
	}

	// 查找匹配的符号
	symbolName := ""
	if file, ok := s.idx.files[path]; ok {
		for _, sym := range file.Symbols {
			if sym.Line >= start && sym.Line <= end {
				symbolName = sym.Name
				break
			}
		}
	}

	return &SearchResult{
		File:       path,
		Score:      score,
		Line:       bestLine + 1,
		Content:    lines[bestLine],
		Context:    context,
		SymbolName: symbolName,
	}
}

// buildResultWithSymbol 构建符号搜索结果。
func (s *Searcher) buildResultWithSymbol(path string, sym Symbol) *SearchResult {
	fullPath := filepath.Join(s.idx.rootDir, path)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil
	}

	lines := strings.Split(string(content), "\n")
	lineIdx := sym.Line - 1
	if lineIdx < 0 || lineIdx >= len(lines) {
		return nil
	}

	contextLines := s.idx.cfg.GetContextLines()
	context := []string{}
	start := lineIdx - contextLines
	if start < 0 {
		start = 0
	}
	end := lineIdx + contextLines + 1
	if end > len(lines) {
		end = len(lines)
	}
	for i := start; i < end; i++ {
		context = append(context, lines[i])
	}

	return &SearchResult{
		File:       path,
		Score:      1.0,
		Line:       sym.Line,
		Content:    lines[lineIdx],
		Context:    context,
		SymbolName: sym.Name,
	}
}

// computeAvgDocLen 计算平均文档长度（行数）。
func (s *Searcher) computeAvgDocLen() float64 {
	if s.idx.totalDocs == 0 {
		return 1
	}
	total := 0
	for _, f := range s.idx.files {
		total += f.Lines
	}
	return float64(total) / float64(s.idx.totalDocs)
}

// getDocLength 获取文档长度（行数）。
func (s *Searcher) getDocLength(path string) int {
	if f, ok := s.idx.files[path]; ok {
		return f.Lines
	}
	return 1
}

// tokenize 将文本分词（简单的空格和标点分割，转小写）。
func tokenize(text string) []string {
	// 转小写
	text = strings.ToLower(text)
	// 用非字母数字字符分割
	var words []string
	var current strings.Builder
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			current.WriteRune(r)
		} else {
			if current.Len() > 1 { // 忽略单字符
				words = append(words, current.String())
			}
			current.Reset()
		}
	}
	if current.Len() > 1 {
		words = append(words, current.String())
	}
	return words
}

// FormatResults 格式化搜索结果为文本。
func FormatResults(results []SearchResult) string {
	if len(results) == 0 {
		return "没有找到匹配的结果"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("找到 %d 个匹配结果:\n\n", len(results)))
	for i, r := range results {
		symbol := ""
		if r.SymbolName != "" {
			symbol = fmt.Sprintf(" [%s]", r.SymbolName)
		}
		fmt.Fprintf(&sb, "%d. %s:%d%s (相关性: %.2f)\n", i+1, r.File, r.Line, symbol, r.Score)
		if len(r.Context) > 0 {
			sb.WriteString("```\n")
			for _, line := range r.Context {
				sb.WriteString(line)
				sb.WriteString("\n")
			}
			sb.WriteString("```\n")
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}
