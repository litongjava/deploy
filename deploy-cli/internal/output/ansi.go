// Package output 负责终端的彩色输出、ANSI 转义序列剥离与表格渲染。
//
// 设计原则：
//   - 只依赖标准库 + golang.org/x/term（判断 TTY），不引入重型 TUI 库；
//   - 非 TTY、--no-color、NO_COLOR 或 --json 时一律不产生颜色；
//   - 处理日志流时使用带状态的 StripWriter，保证被切分到两个数据块里的
//     转义序列也能被正确剥离。
package output

import (
	"io"
	"strings"
)

// esc 是 ANSI 转义序列的起始字节。
const esc = 0x1b

// StripANSI 删除 s 中所有完整的 ANSI 转义序列（CSI / OSC / 两字节 Fe）。
// 假定输入是完整文本（不跨块切分）。
func StripANSI(s string) string {
	text, _ := stripBytes([]byte(s))
	return string(text)
}

// StripWriter 是一个 io.Writer 包装器：写入其中的 ANSI 转义序列会被剥离，
// 并且会缓存结尾处不完整的转义序列，等下一次 Write 补齐后再判断。
type StripWriter struct {
	w    io.Writer
	tail []byte
}

// NewStripWriter 构造一个剥离 ANSI 的 Writer；w 为 nil 时使用 io.Discard。
func NewStripWriter(w io.Writer) *StripWriter {
	if w == nil {
		w = io.Discard
	}
	return &StripWriter{w: w}
}

// Write 实现 io.Writer。始终返回 len(p), nil（写入语义上完全接受）。
func (s *StripWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	buf := make([]byte, 0, len(s.tail)+len(p))
	buf = append(buf, s.tail...)
	buf = append(buf, p...)

	text, held := stripBytes(buf)
	s.tail = append(s.tail[:0], held...)
	if len(text) > 0 {
		if _, err := s.w.Write(text); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

// Flush 输出被缓存的、无法判定的尾部内容（例如流结束时只剩一个 ESC）。
func (s *StripWriter) Flush() error {
	if len(s.tail) == 0 {
		return nil
	}
	_, err := s.w.Write([]byte{esc})
	s.tail = s.tail[:0]
	return err
}

// stripBytes 扫描 b，返回可安全输出的文本与结尾处不完整的转义序列。
func stripBytes(b []byte) (text, held []byte) {
	out := make([]byte, 0, len(b))
	i := 0
	for i < len(b) {
		if b[i] != esc {
			out = append(out, b[i])
			i++
			continue
		}
		n, complete := ansiLen(b[i:])
		if !complete {
			return out, b[i:]
		}
		i += n
	}
	return out, nil
}

// ansiLen 返回 b（以 ESC 开头）处一个转义序列的长度；complete=false 表示
// 序列在 b 结束前还没结束，需要更多数据。
func ansiLen(b []byte) (int, bool) {
	if len(b) < 2 {
		return 0, false
	}
	switch c := b[1]; {
	case c == '[': // CSI: ESC [ 参数 中间字节 终止字节
		i := 2
		for i < len(b) && b[i] >= 0x30 && b[i] <= 0x3f {
			i++
		}
		for i < len(b) && b[i] >= 0x20 && b[i] <= 0x2f {
			i++
		}
		if i >= len(b) {
			return 0, false
		}
		if b[i] >= 0x40 && b[i] <= 0x7e {
			return i + 1, true
		}
		// 非法终止字节：当作两字节序列吞掉，避免死循环。
		return 2, true
	case c == ']': // OSC: ESC ] ... BEL 或 ESC \
		for i := 2; i < len(b); i++ {
			if b[i] == 0x07 {
				return i + 1, true
			}
			if b[i] == esc && i+1 < len(b) && b[i+1] == '\\' {
				return i + 2, true
			}
		}
		return 0, false
	case c >= 0x40 && c <= 0x5f: // 两字节 Fe 序列
		return 2, true
	default:
		return 2, true
	}
}

// NeedsStripping 报告 s 中是否含有可能的 ANSI 转义序列。
func NeedsStripping(s string) bool {
	return strings.IndexByte(s, esc) >= 0
}
