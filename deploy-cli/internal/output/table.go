package output

import (
	"strings"
	"unicode"
)

// DisplayWidth 返回字符串在等宽终端里占用的列数（CJK 全角按 2 列）。
// 计算前会剥离 ANSI 转义序列，因此可以安全地把彩色文本放进表格。
func DisplayWidth(s string) int {
	s = StripANSI(s)
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

func runeWidth(r rune) int {
	switch {
	case r == 0:
		return 0
	case r < 32 || (r >= 0x7f && r < 0xa0):
		return 0
	case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r):
		return 0
	case isWide(r):
		return 2
	default:
		return 1
	}
}

// isWide 判定东亚全角/宽字符，覆盖常见区间（等价于 go-runewidth 的简化版）。
func isWide(r rune) bool {
	switch {
	case r < 0x1100:
		return false
	case r <= 0x115f, // Hangul Jamo
		r == 0x2329, r == 0x232a,
		r >= 0x2e80 && r <= 0x303e, // CJK 部首、假名标点
		r >= 0x3041 && r <= 0x33ff,
		r >= 0x3400 && r <= 0x4dbf,
		r >= 0x4e00 && r <= 0x9fff, // CJK 统一表意文字
		r >= 0xa000 && r <= 0xa4cf,
		r >= 0xa960 && r <= 0xa97f,
		r >= 0xac00 && r <= 0xd7a3, // Hangul 音节
		r >= 0xf900 && r <= 0xfaff,
		r >= 0xfe10 && r <= 0xfe19,
		r >= 0xfe30 && r <= 0xfe6f,
		r >= 0xff00 && r <= 0xff60, // 全角
		r >= 0xffe0 && r <= 0xffe6,
		r >= 0x1f300 && r <= 0x1f64f, // emoji
		r >= 0x1f900 && r <= 0x1f9ff,
		r >= 0x20000 && r <= 0x2fffd,
		r >= 0x30000 && r <= 0x3fffd:
		return true
	}
	return false
}

// padRight 在 s 右侧补空格，使其显示宽度达到 width。
func padRight(s string, width int) string {
	d := width - DisplayWidth(s)
	if d <= 0 {
		return s
	}
	return s + strings.Repeat(" ", d)
}

// Table 渲染一个简单的等宽表格（表头 + 行），列宽按显示宽度对齐。
func Table(headers []string, rows [][]string) string {
	cols := len(headers)
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	widths := make([]int, cols)
	for i, h := range headers {
		widths[i] = DisplayWidth(h)
	}
	for _, r := range rows {
		for i, c := range r {
			if w := DisplayWidth(c); w > widths[i] {
				widths[i] = w
			}
		}
	}

	var b strings.Builder
	writeRow := func(cells []string) {
		parts := make([]string, 0, cols)
		for i := 0; i < cols; i++ {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			if i == cols-1 {
				parts = append(parts, cell) // 最后一列不补空格
			} else {
				parts = append(parts, padRight(cell, widths[i]))
			}
		}
		b.WriteString(strings.TrimRight(strings.Join(parts, "  "), " "))
		b.WriteByte('\n')
	}
	writeRow(headers)
	seps := make([]string, cols)
	for i := range seps {
		seps[i] = strings.Repeat("-", widths[i])
	}
	writeRow(seps)
	for _, r := range rows {
		writeRow(r)
	}
	return b.String()
}

// KeyValue 渲染 "键: 值" 的简单对齐块，键在左侧补空格。
func KeyValue(pairs [][2]string) string {
	width := 0
	for _, p := range pairs {
		if w := DisplayWidth(p[0]); w > width {
			width = w
		}
	}
	var b strings.Builder
	for _, p := range pairs {
		b.WriteString(padRight(p[0], width))
		b.WriteString("  ")
		b.WriteString(p[1])
		b.WriteByte('\n')
	}
	return b.String()
}
