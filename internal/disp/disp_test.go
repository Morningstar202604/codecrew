package disp

import (
	"strings"
	"testing"
)

func TestWidthCountsCJKAsTwo(t *testing.T) {
	cases := map[string]int{
		"":       0,
		"abc":    3,
		"中":      2,
		"中文":     4,
		"a中b":    4,
		"Ａ":      2, // 全角 A
		"-":      1, // 破折号按 1 列（许多终端如此）
		" café":  5, // é 为单个 NFC 码点，算 1 列
		"日本語한국어": 12,
		"→":      1, // 箭头是窄字符
	}
	for in, want := range cases {
		if got := Width(in); got != want {
			t.Fatalf("Width(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestPadAlignsMixedText(t *testing.T) {
	// 关键回归：中英混排必须按显示宽度补空格，否则列表错位
	left := Pad("developer", 12) + "描述A"
	right := Pad("审查员", 12) + "描述B"
	if Width(left) != Width(right) {
		t.Fatalf("对齐失败: %q(%d) vs %q(%d)", left, Width(left), right, Width(right))
	}
	if Pad("已经很长了的字符串", 4) != "已经很长了的字符串" {
		t.Fatal("超宽不应截断")
	}
}

func TestTruncateKeepsDisplayWidth(t *testing.T) {
	got := Truncate("abcdefghijklmnopqrstuvwxyz", 10)
	if Width(got) > 10 || !strings.HasSuffix(got, "…") {
		t.Fatalf("Truncate = %q (%d 列)", got, Width(got))
	}
	got = Truncate("一二三四五六七八九十", 7)
	if Width(got) > 7 {
		t.Fatalf("中文截断超宽: %q (%d 列)", got, Width(got))
	}
	if Truncate("短", 10) != "短" {
		t.Fatal("未超宽应原样返回")
	}
}
