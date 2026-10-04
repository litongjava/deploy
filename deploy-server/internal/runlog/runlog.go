// Package runlog 负责运行日志的落盘与实时分发。
//
// 两条链路走同一份字节：
//   - 落盘：{logDir}/{projectKey}/{runID}.log，支持按 offset 增量读取
//   - 分发：进程内发布订阅，推给订阅了该 Run 的 WebSocket 连接
//
// 两个关键设计：
//
//  1. **先打码再落盘**。Secret 的值在写文件之前就被替换成 ***，
//     所以下载下来的日志文件里也不会带出密钥（需求 FR-RUN-12）。
//
//  2. **每个日志块都带字节偏移**。WebSocket 的处理是「先订阅、再读历史」，
//     这两步之间必然有重叠；带上 offset 就能精确去重，做到「不丢也不重」，
//     客户端断线重连时只要带上 nextOffset 就能无缝续上。
package runlog

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// Chunk 是一段带起始偏移的日志。
type Chunk struct {
	Offset int64
	Data   []byte
}

// Sub 是一个订阅者。
//
// Ch 关闭表示「流结束」或「订阅者跟不上被断开」，
// 两种情况客户端都应按 nextOffset 重连补齐，因此不需要区分。
type Sub struct {
	Ch chan Chunk
}

// Broker 是 Run 级别的发布订阅中心。
type Broker struct {
	mu   sync.Mutex
	subs map[uint]map[*Sub]struct{}
}

// NewBroker 构造一个 Broker。
func NewBroker() *Broker {
	return &Broker{subs: make(map[uint]map[*Sub]struct{})}
}

// Subscribe 订阅某个 Run 的增量日志。
func (b *Broker) Subscribe(runID uint) *Sub {
	s := &Sub{Ch: make(chan Chunk, 256)}
	b.mu.Lock()
	defer b.mu.Unlock()
	m, ok := b.subs[runID]
	if !ok {
		m = make(map[*Sub]struct{})
		b.subs[runID] = m
	}
	m[s] = struct{}{}
	return s
}

// Unsubscribe 取消订阅（幂等）。
func (b *Broker) Unsubscribe(runID uint, s *Sub) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if m, ok := b.subs[runID]; ok {
		if _, exists := m[s]; exists {
			delete(m, s)
			close(s.Ch)
		}
		if len(m) == 0 {
			delete(b.subs, runID)
		}
	}
}

// Publish 把一段新日志推给所有订阅者。
//
// 订阅者缓冲区满时**主动断开它**：宁可让客户端重连后按 offset 补齐，
// 也不能阻塞执行器 —— 否则一个慢客户端就能把整个部署卡死。
func (b *Broker) Publish(runID uint, offset int64, data []byte) {
	if len(data) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	m, ok := b.subs[runID]
	if !ok {
		return
	}
	for s := range m {
		select {
		case s.Ch <- Chunk{Offset: offset, Data: data}:
		default:
			close(s.Ch)
			delete(m, s)
		}
	}
}

// CloseRun 关闭某个 Run 的全部订阅（执行结束时调用）。幂等。
func (b *Broker) CloseRun(runID uint) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs[runID] {
		close(s.Ch)
	}
	delete(b.subs, runID)
}

// ---------- 打码 ----------

// Masker 在把日志写到下游之前，按行把 Secret 值替换成 ***。
//
// 为什么要按行缓冲：Secret 可能被命令切成两半写到两次 Write 里
// （"sec" + "ret"），不做缓冲就会漏打码。缓冲超过 maxLine 也会强制
// 冲刷，避免没有换行的输出（进度条、长 JSON）被无限攒着看不到。
type Masker struct {
	mu      sync.Mutex
	secrets [][]byte
	buf     []byte
	maxLine int
	out     func([]byte)
}

// NewMasker 构造打码器。长度小于 3 的 secret 不参与打码，
// 否则会把正常日志打成马赛克。
func NewMasker(secrets []string, out func([]byte)) *Masker {
	m := &Masker{maxLine: 8192, out: out}
	m.AddSecrets(secrets)
	return m
}

// AddSecrets 追加需要打码的值。
//
// 为什么需要它：Git 凭据要等到 Prepare 阶段才解出来，而日志从第一行起
// 就在增量落盘，所以必须能在运行中把新秘密加进清单。
func (m *Masker) AddSecrets(list []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]struct{}{}
	for _, s := range m.secrets {
		seen[string(s)] = struct{}{}
	}
	for _, s := range list {
		if len(s) < 3 {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		m.secrets = append(m.secrets, []byte(s))
	}
	// 长的先替换，避免短串把长串切碎后匹配不上。
	sort.Slice(m.secrets, func(i, j int) bool { return len(m.secrets[i]) > len(m.secrets[j]) })
}

// Write 接收原始输出片段。
func (m *Masker) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buf = append(m.buf, p...)
	for {
		i := bytes.IndexByte(m.buf, '\n')
		if i < 0 {
			if len(m.buf) >= m.maxLine {
				m.flushLocked(len(m.buf))
			}
			break
		}
		m.flushLocked(i + 1)
	}
	return len(p), nil
}

// Flush 冲刷残留缓冲，保证最后一行不丢。
func (m *Masker) Flush() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.buf) > 0 {
		m.flushLocked(len(m.buf))
	}
}

func (m *Masker) flushLocked(n int) {
	chunk := m.mask(m.buf[:n])
	m.buf = m.buf[n:]
	if len(chunk) > 0 {
		m.out(chunk)
	}
}

func (m *Masker) mask(in []byte) []byte {
	out := in
	for _, sec := range m.secrets {
		if bytes.Contains(out, sec) {
			out = bytes.ReplaceAll(out, sec, []byte("***"))
		}
	}
	return out
}

// ---------- 写入器 ----------

// Writer 是单个 Run 的日志写入器：打码、落盘、发布、大小上限。
type Writer struct {
	mu       sync.Mutex
	file     *os.File
	path     string
	size     int64
	maxBytes int64
	runID    uint
	broker   *Broker
	masker   *Masker
	overflow bool
	closed   bool
}

// PathFor 给出某个 Run 的日志文件路径。这是**唯一**的路径推导逻辑：
// 执行器写、handler 读、删项目时清理，三处都调它。
func PathFor(logDir, projectKey string, runID uint) string {
	return filepath.Join(logDir, safeName(projectKey), fmt.Sprintf("%d.log", runID))
}

// NewWriter 创建（或截断）某个 Run 的日志文件。
func NewWriter(logDir, projectKey string, runID uint, broker *Broker, secrets []string, maxBytes int64) (*Writer, error) {
	dir := filepath.Join(logDir, safeName(projectKey))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}
	path := PathFor(logDir, projectKey, runID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return nil, fmt.Errorf("创建日志文件失败: %w", err)
	}
	w := &Writer{file: f, path: path, maxBytes: maxBytes, runID: runID, broker: broker}
	w.masker = NewMasker(secrets, w.emit)
	return w, nil
}

// emit 由 Masker 调用，此时数据已经打过码。
func (w *Writer) emit(data []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}

	if w.maxBytes > 0 && w.size >= w.maxBytes {
		if !w.overflow {
			w.overflow = true
			w.writeLocked([]byte("\n*** 日志已达上限，后续输出被丢弃 ***\n"))
		}
		return
	}
	if capped := w.capLocked(data); len(capped) > 0 {
		w.writeLocked(capped)
	}
}

// writeLocked 是唯一的落盘+发布点，保证「文件偏移」与「发布偏移」永远一致。
func (w *Writer) writeLocked(data []byte) {
	// 归一化成合法 UTF-8，这是维持上面那句承诺的关键。
	//
	// 不这么做会有一个很隐蔽的 bug：实时推送的每一帧都带 nextOffset（字节偏移），
	// 客户端靠 `nextOffset - len(data)` 推算本帧起点来做去重。
	// 而 Go 的 JSON 编码遇到非法 UTF-8 字节会替换成 U+FFFD（1 字节变 3 字节），
	// 于是「帧里 data 的长度」与「文件里实际写进去的字节数」就对不上了。
	// 触发条件并不罕见：命令 `cat` 一个图片、程序输出半截多字节字符、
	// 或者某个工具写了非 UTF-8 的本地编码文本。
	// 在唯一的出口做一次归一化，文件里就是合法 UTF-8，两边字节数恒等。
	if !utf8.Valid(data) {
		data = []byte(strings.ToValidUTF8(string(data), "\uFFFD"))
	}

	off := w.size
	if _, err := w.file.Write(data); err != nil {
		return
	}
	w.size += int64(len(data))
	if w.broker != nil {
		cp := make([]byte, len(data))
		copy(cp, data)
		w.broker.Publish(w.runID, off, cp)
	}
}

func (w *Writer) capLocked(data []byte) []byte {
	if w.maxBytes <= 0 {
		return data
	}
	room := w.maxBytes - w.size
	if room <= 0 {
		return nil
	}
	if int64(len(data)) > room {
		return data[:room]
	}
	return data
}

// Write 实现 io.Writer，作为命令 stdout/stderr 的落点。
func (w *Writer) Write(p []byte) (int, error) { return w.masker.Write(p) }

// Close 冲刷并关闭文件，返回最终大小。幂等。
func (w *Writer) Close() int64 {
	w.masker.Flush()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return w.size
	}
	w.closed = true
	_ = w.file.Sync()
	_ = w.file.Close()
	return w.size
}

// Path 是日志文件路径。
func (w *Writer) Path() string { return w.path }

// AddSecrets 见 Masker.AddSecrets。执行器在解出 Git 凭据后立刻调用它，
// 保证凭据不会因为「日志已经从第一行开始增量落盘」而漏出去。
func (w *Writer) AddSecrets(list []string) { w.masker.AddSecrets(list) }

// Size 是当前已写入字节数（不含尚未冲刷的缓冲）。
func (w *Writer) Size() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.size
}

// ReadRange 读取文件 [offset, offset+limit) 的内容。
// 返回数据、下一个 offset、是否已到文件末尾。
func ReadRange(path string, offset int64, limit int) ([]byte, int64, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, offset, true, nil
		}
		return nil, offset, false, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, offset, false, err
	}
	if offset > st.Size() {
		offset = st.Size()
	}
	if _, err := f.Seek(offset, 0); err != nil {
		return nil, offset, false, err
	}
	buf := make([]byte, limit)
	n, _ := f.Read(buf)
	next := offset + int64(n)
	return buf[:n], next, next >= st.Size(), nil
}

// safeName 把项目 key 变成一个安全的目录名。
//
// 白名单里包含 '.'，所以必须单独挡掉 "." 与 ".."：
// 否则 key=".." 会让日志路径变成 {logDir}/../1.log，直接跳出日志根目录。
// （这条是测试发现的真实缺陷，不是理论顾虑。）
func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	switch out {
	case "", ".", "..":
		return "default"
	}
	return out
}
