package output

import (
	"strings"
	"testing"
)

func TestDisplayWidth(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"abc", 3},
		{"", 0},
		{"我的应用", 8},
		{"a中b", 4},
		{"\x1b[31m红色\x1b[0m", 4}, // 颜色不计宽
		{"✔", 1},
		{"→", 1},
	}
	for _, tc := range cases {
		if got := DisplayWidth(tc.in); got != tc.want {
			t.Fatalf("DisplayWidth(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestTableAlignsByDisplayWidth(t *testing.T) {
	out := Table(
		[]string{"KEY", "名称", "状态"},
		[][]string{
			{"my-app", "我的应用", "success"},
			{"a-longer-key", "b", "failed"},
		},
	)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expect header+sep+2 rows, got %d lines: %q", len(lines), out)
	}
	// 表头与分隔行、数据行的第一列起点必须一致
	if !strings.HasPrefix(lines[0], "KEY") || !strings.HasPrefix(lines[1], "---") {
		t.Fatalf("bad header/separator: %q / %q", lines[0], lines[1])
	}
	// 第二列起点应该对齐：找 "名称" 的列位置
	idx := strings.Index(lines[0], "名称")
	idx2 := strings.Index(lines[2], "我的应用")
	if idx != idx2 {
		t.Fatalf("column not aligned: header name at %d, value at %d\n%s", idx, idx2, out)
	}
}

func TestTableEmptyRows(t *testing.T) {
	out := Table([]string{"A", "B"}, nil)
	if !strings.Contains(out, "A") || !strings.Contains(out, "B") {
		t.Fatalf("header missing: %q", out)
	}
}

func TestKeyValue(t *testing.T) {
	out := KeyValue([][2]string{{"a", "1"}, {"longer", "2"}})
	if !strings.Contains(out, "a       1") {
		t.Fatalf("not padded: %q", out)
	}
}
