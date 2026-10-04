package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestStripANSI(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello world", "hello world"},
		{"color", "\x1b[31m红色\x1b[0m", "红色"},
		{"bold+cyan", "\x1b[1m\x1b[36m标题\x1b[0m: ok", "标题: ok"},
		{"cursor", "\x1b[2K\x1b[1G进度", "进度"},
		{"osc-title", "\x1b]0;window title\x07正文", "正文"},
		{"osc-st", "\x1b]8;;http://x\x1b\\链接", "链接"},
		{"reset-only", "\x1b[0m", ""},
		{"chinese-passthrough", "构建成功 ✔ 12.3s", "构建成功 ✔ 12.3s"},
		{"mixed", "a\x1b[31mb\x1b[0mc", "abc"},
		{"two-byte", "a\x1bMb", "ab"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripANSI(tc.in); got != tc.want {
				t.Fatalf("StripANSI(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestStripWriterWholeChunk(t *testing.T) {
	var buf bytes.Buffer
	w := NewStripWriter(&buf)
	if _, err := w.Write([]byte("\x1b[32mok\x1b[0m\n")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "ok\n" {
		t.Fatalf("got %q", got)
	}
}

// 关键用例：转义序列被切分到多个 Write 调用里，仍必须被完整剥离。
func TestStripWriterSplitSequence(t *testing.T) {
	var buf bytes.Buffer
	w := NewStripWriter(&buf)
	chunks := []string{"a\x1b[3", "1mred", "\x1b[", "0mb\x1b", "]0;t\x07c"}
	for _, c := range chunks {
		if _, err := w.Write([]byte(c)); err != nil {
			t.Fatal(err)
		}
	}
	if got := buf.String(); got != "aredbc" {
		t.Fatalf("got %q, want %q", got, "aredbc")
	}
}

func TestStripWriterIncompleteTailIsHeld(t *testing.T) {
	var buf bytes.Buffer
	w := NewStripWriter(&buf)
	if _, err := w.Write([]byte("done\x1b[")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "done" {
		t.Fatalf("emitted %q before sequence completed", got)
	}
	if _, err := w.Write([]byte("0m\n")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "done\n" {
		t.Fatalf("got %q", got)
	}
}

func TestStripWriterFlush(t *testing.T) {
	var buf bytes.Buffer
	w := NewStripWriter(&buf)
	if _, err := w.Write([]byte("x\x1b")); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "x\x1b" {
		t.Fatalf("got %q", got)
	}
}

func TestNeedsStripping(t *testing.T) {
	if NeedsStripping("plain") {
		t.Fatal("false positive")
	}
	if !NeedsStripping("\x1b[1mx") {
		t.Fatal("missed escape")
	}
}

func TestStripANSIKeepsUTF8Intact(t *testing.T) {
	in := strings.Repeat("中", 64) + "\x1b[0m"
	if got := StripANSI(in); got != strings.Repeat("中", 64) {
		t.Fatalf("utf8 broken: %q", got)
	}
}
