// Package disp 提供终端显示宽度计算，解决中英混排对齐问题。
package disp

import (
	"strings"
	"unicode/utf8"
)

// RuneCount 返回字符数（按 rune 计）。
func RuneCount(s string) int { return utf8.RuneCountInString(s) }

// Width 返回 s 在终端中占用的列数（CJK/全角字符按 2 列计算）。
func Width(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

func runeWidth(r rune) int {
	if r == 0 {
		return 0
	}
	if isWide(r) {
		return 2
	}
	return 1
}

// isWide 判断是否为宽字符（CJK、全角标点、假名、韩文、Emoji 区段等）。
func isWide(r rune) bool {
	switch {
	case r < 0x1100:
		return false
	case r <= 0x115F: // 韩文字母 Jamo
		return true
	case r == 0x2329 || r == 0x232A:
		return true
	case r >= 0x2E80 && r <= 0x303E: // CJK 部首、康熙部首、中日韩符号
		return true
	case r >= 0x3041 && r <= 0x33FF: // 假名、注音、CJK 兼容
		return true
	case r >= 0x3400 && r <= 0x4DBF: // CJK 扩展 A
		return true
	case r >= 0x4E00 && r <= 0x9FFF: // CJK 统一汉字
		return true
	case r >= 0xA000 && r <= 0xA4CF: // 彝文
		return true
	case r >= 0xAC00 && r <= 0xD7A3: // 韩文音节
		return true
	case r >= 0xF900 && r <= 0xFAFF: // CJK 兼容汉字
		return true
	case r >= 0xFE30 && r <= 0xFE6F: // CJK 兼容形式
		return true
	case r >= 0xFF00 && r <= 0xFF60: // 全角 ASCII
		return true
	case r >= 0xFFE0 && r <= 0xFFE6: // 全角符号
		return true
	case r >= 0x1F300 && r <= 0x1FAFF: // Emoji 及图形符号
		return true
	case r >= 0x20000 && r <= 0x3FFFD: // CJK 扩展 B 及以后
		return true
	}
	return false
}

// Pad 把 s 右侧补空格到 width 列宽；若已超宽则原样返回。
func Pad(s string, width int) string {
	diff := width - Width(s)
	if diff <= 0 {
		return s
	}
	return s + strings.Repeat(" ", diff)
}

// Truncate 按显示宽度截断，超出部分以 … 结尾。
func Truncate(s string, width int) string {
	if Width(s) <= width {
		return s
	}
	var sb strings.Builder
	used := 0
	for _, r := range s {
		rw := runeWidth(r)
		if used+rw > width-1 {
			break
		}
		sb.WriteRune(r)
		used += rw
	}
	sb.WriteString("…")
	return sb.String()
}
