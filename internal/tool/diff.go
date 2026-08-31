package tool

import (
	"fmt"
	"os"
	"strings"
)

// DiffHunk 表示统一 diff 中的一个连续变更块。
type DiffHunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []DiffLine
}

// DiffLine 是 hunk 中的一行，Kind 为 ' ' / '+' / '-'。
type DiffLine struct {
	Kind byte
	Text string
}

// UnifiedDiff 生成两段文本的统一 diff。超过 maxLines 行时只返回摘要，避免大文件撑爆终端。
// oldName/newName 用于文件头展示。
func UnifiedDiff(oldName, newName, oldText, newText string, maxLines int) string {
	if maxLines <= 0 {
		maxLines = 200
	}
	oldLines := strings.Split(strings.TrimRight(oldText, "\n"), "\n")
	newLines := strings.Split(strings.TrimRight(newText, "\n"), "\n")
	if oldText == "" {
		oldLines = nil
	}
	if newText == "" {
		newLines = nil
	}

	hunks := computeHunks(oldLines, newLines, 3)
	if len(hunks) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n", oldName)
	fmt.Fprintf(&sb, "+++ %s\n", newName)
	total := 0
	for _, h := range hunks {
		if total >= maxLines {
			fmt.Fprintf(&sb, "... 后续还有 %d 个 hunk 已省略（共 %d 个）\n", len(hunks)-indexOfHunk(hunks, h), len(hunks))
			break
		}
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", h.OldStart, h.OldCount, h.NewStart, h.NewCount)
		for _, line := range h.Lines {
			sb.WriteByte(line.Kind)
			sb.WriteString(line.Text)
			sb.WriteByte('\n')
			total++
			if total >= maxLines {
				sb.WriteString("... diff 过长，已截断\n")
				break
			}
		}
	}
	return sb.String()
}

func indexOfHunk(hunks []DiffHunk, target DiffHunk) int {
	for i, h := range hunks {
		if h.OldStart == target.OldStart && h.NewStart == target.NewStart {
			return i
		}
	}
	return -1
}

// computeHunks 用 LCS 算法计算变更块，context 为每个 hunk 前后保留的上下文行数。
func computeHunks(oldLines, newLines []string, context int) []DiffHunk {
	m, n := len(oldLines), len(newLines)
	// dp[i][j] = oldLines[i:] 和 newLines[j:] 的 LCS 长度
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	// 回溯 LCS，生成编辑脚本
	type edit struct {
		kind   byte // ' ' 匹配, '-' 删除, '+' 新增
		oldIdx int
		newIdx int
	}
	var script []edit
	i, j := 0, 0
	for i < m && j < n {
		if oldLines[i] == newLines[j] {
			script = append(script, edit{' ', i, j})
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			script = append(script, edit{'-', i, -1})
			i++
		} else {
			script = append(script, edit{'+', -1, j})
			j++
		}
	}
	for ; i < m; i++ {
		script = append(script, edit{'-', i, -1})
	}
	for ; j < n; j++ {
		script = append(script, edit{'+', -1, j})
	}

	// 把编辑脚本切成带上下文的 hunk
	var hunks []DiffHunk
	idx := 0
	for idx < len(script) {
		// 跳过纯上下文行，找到第一个变更
		if script[idx].kind == ' ' {
			idx++
			continue
		}
		// hunk 起点：回退 context 行上下文
		start := idx - context
		if start < 0 {
			start = 0
		}
		// hunk 终点：找到最后一个变更后再走 context 行
		end := idx
		for end < len(script) && script[end].kind != ' ' {
			end++
		}
		// 跳过变更后的连续上下文，直到下一个变更或结束
		ctxCount := 0
		for end < len(script) && ctxCount < context {
			if script[end].kind != ' ' {
				// 下一个变更紧挨着，扩展当前 hunk
				for end < len(script) && script[end].kind != ' ' {
					end++
				}
				ctxCount = 0
				continue
			}
			end++
			ctxCount++
		}

		hunk := DiffHunk{}
		var oldLineNo, newLineNo int
		// 计算起始行号
		if start < len(script) {
			if script[start].oldIdx >= 0 {
				oldLineNo = script[start].oldIdx
			} else {
				// 第一行是新增，找前面最近的匹配行
				for k := start - 1; k >= 0; k-- {
					if script[k].oldIdx >= 0 {
						oldLineNo = script[k].oldIdx + 1
						break
					}
				}
			}
			if script[start].newIdx >= 0 {
				newLineNo = script[start].newIdx
			} else {
				for k := start - 1; k >= 0; k-- {
					if script[k].newIdx >= 0 {
						newLineNo = script[k].newIdx + 1
						break
					}
				}
			}
		}
		hunk.OldStart = oldLineNo + 1
		hunk.NewStart = newLineNo + 1

		oldInHunk, newInHunk := 0, 0
		for k := start; k < end && k < len(script); k++ {
			e := script[k]
			switch e.kind {
			case ' ':
				hunk.Lines = append(hunk.Lines, DiffLine{' ', oldLines[e.oldIdx]})
				oldInHunk++
				newInHunk++
			case '-':
				hunk.Lines = append(hunk.Lines, DiffLine{'-', oldLines[e.oldIdx]})
				oldInHunk++
			case '+':
				hunk.Lines = append(hunk.Lines, DiffLine{'+', newLines[e.newIdx]})
				newInHunk++
			}
		}
		hunk.OldCount = oldInHunk
		hunk.NewCount = newInHunk
		hunks = append(hunks, hunk)
		idx = end
	}
	return hunks
}

// PreviewWrite 为 write 工具生成 diff 预览。文件不存在时展示「新建文件」摘要。
func PreviewWrite(path, content string) string {
	existing, err := readFileSafe(path)
	if err != nil || existing == "" {
		lines := strings.Count(content, "\n") + 1
		return fmt.Sprintf("新建文件 %s（%d 行 / %d 字节）\n", path, lines, len(content))
	}
	return UnifiedDiff(path+" (原)", path+" (新)", existing, content, 200)
}

// PreviewEdit 为 edit 工具生成 diff 预览。先计算替换后的内容，再与原文 diff。
func PreviewEdit(path, oldText, newText string) (string, error) {
	existing, err := readFileSafe(path)
	if err != nil {
		return "", fmt.Errorf("读取文件失败: %w", err)
	}
	count := strings.Count(existing, oldText)
	if count == 0 {
		return "", fmt.Errorf("未找到 old_text，无法预览 diff")
	}
	if count > 1 {
		return "", fmt.Errorf("old_text 匹配到 %d 处，无法唯一替换", count)
	}
	updated := strings.Replace(existing, oldText, newText, 1)
	return UnifiedDiff(path+" (原)", path+" (新)", existing, updated, 200), nil
}

// readFileSafe 读取文件，失败返回空字符串和错误。
func readFileSafe(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
