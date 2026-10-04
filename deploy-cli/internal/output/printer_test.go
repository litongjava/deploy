package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func newTestPrinter(jsonMode, noColor bool) (*Printer, *bytes.Buffer, *bytes.Buffer) {
	var out, errw bytes.Buffer
	return New(&out, &errw, jsonMode, noColor, false), &out, &errw
}

// bytes.Buffer 没有 Fd()，因此非 TTY：默认必须无颜色。
func TestNoColorWhenNotTTY(t *testing.T) {
	p, out, _ := newTestPrinter(false, false)
	if p.ColorEnabled() {
		t.Fatal("非 TTY 不应启用颜色")
	}
	p.Printf("%s", p.Green("ok"))
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("输出包含 ANSI: %q", out.String())
	}
}

func TestJSONModeDisablesColor(t *testing.T) {
	p, _, _ := newTestPrinter(true, false)
	if p.ColorEnabled() {
		t.Fatal("--json 不应启用颜色")
	}
}

func TestNoColorFlagDisablesColor(t *testing.T) {
	p, _, _ := newTestPrinter(false, true)
	if p.ColorEnabled() {
		t.Fatal("--no-color 不应启用颜色")
	}
}

func TestHumanHintsGoToStderr(t *testing.T) {
	p, out, errw := newTestPrinter(true, true)
	if err := p.JSONOut(map[string]any{"a": 1}); err != nil {
		t.Fatal(err)
	}
	p.Infof("提示 %s", "x")
	p.Warnf("小心")
	if strings.Contains(out.String(), "提示") || strings.Contains(out.String(), "小心") {
		t.Fatalf("提示信息污染 stdout: %q", out.String())
	}
	if !strings.Contains(errw.String(), "提示 x") || !strings.Contains(errw.String(), "小心") {
		t.Fatalf("stderr 缺少提示: %q", errw.String())
	}
	// stdout 必须是合法 JSON
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("stdout 不是合法 JSON: %v (%q)", err, out.String())
	}
}

func TestLogWriterStripsANSIWhenNoColor(t *testing.T) {
	p, out, _ := newTestPrinter(false, false) // 非 TTY → 剥离
	sw, ok := p.LogWriter().(*StripWriter)
	if !ok {
		t.Fatalf("非彩色非 JSON 模式应返回 *StripWriter")
	}
	sw.Write([]byte("\x1b[31mred\x1b[0m\n"))
	sw.Write([]byte("tail\x1b["))
	sw.Write([]byte("0m\n"))
	if got := out.String(); got != "red\ntail\n" {
		t.Fatalf("got %q", got)
	}
}

func TestLogWriterJSONL(t *testing.T) {
	p, out, _ := newTestPrinter(true, true)
	w := p.LogWriter()
	if _, err := w.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(out.String())
	var got struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("不是 JSONL: %v (%q)", err, line)
	}
	if got.Type != "log" || got.Data != "hello\n" {
		t.Fatalf("got %+v", got)
	}
}

func TestStatusColoringNoColor(t *testing.T) {
	p, _, _ := newTestPrinter(false, true)
	for _, s := range []string{"success", "failed", "error", "timeout", "canceled", "running", "queued", ""} {
		if got := p.Status(s); strings.Contains(got, "\x1b[") {
			t.Fatalf("状态 %q 带颜色: %q", s, got)
		}
	}
	if got := p.OK(true); got != "✔" {
		t.Fatalf("OK(true) = %q", got)
	}
	if got := p.OK(false); got != "✘" {
		t.Fatalf("OK(false) = %q", got)
	}
}

func TestVerboseOnlyWhenEnabled(t *testing.T) {
	p, _, errw := newTestPrinter(false, true)
	p.Verbosef("hidden")
	if errw.Len() != 0 {
		t.Fatalf("verbose 关闭却输出了: %q", errw.String())
	}
	p.Verbose = true
	p.Verbosef("shown %d", 1)
	if !strings.Contains(errw.String(), "shown 1") {
		t.Fatalf("verbose 未输出: %q", errw.String())
	}
}
