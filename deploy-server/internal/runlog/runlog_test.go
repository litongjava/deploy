package runlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestWriterNormalizesInvalidUTF8 是保证「帧里 data 的字节数 == 文件里增长的字节数」
// 这个不变量的测试。
//
// 客户端（CLI 与前端终端）都靠 `nextOffset - len(data)` 推算某一帧的起点来做去重；
// 而 Go 的 JSON 编码会把非法 UTF-8 字节替换成 U+FFFD（1 字节变 3 字节）。
// 如果日志里混进二进制内容而不做归一化，客户端算出的起点就会漂移，
// 表现是「重连后日志重复刷一段或整段丢失」——很难查。
func TestWriterNormalizesInvalidUTF8(t *testing.T) {
	dir := t.TempDir()
	broker := NewBroker()
	sub := broker.Subscribe(9)
	defer broker.Unsubscribe(9, sub)

	w, err := NewWriter(dir, "proj", 9, broker, nil, 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	// 故意写入非法 UTF-8：0xff 0xfe 是永远不合法的起始字节。
	_, _ = w.Write([]byte("begin\n"))
	_, _ = w.Write([]byte{'x', 0xff, 0xfe, 'y', '\n'})
	_, _ = w.Write([]byte("end\n"))
	size := w.Close()

	// 必须先关闭订阅再 drain：Close() 只关文件，不会关订阅通道，
	// 直接 for range 会永久阻塞（这个用例第一版就是这么挂了 600 秒）。
	broker.CloseRun(9)

	var chunks []Chunk
	for c := range sub.Ch {
		chunks = append(chunks, c)
	}
	if len(chunks) == 0 {
		t.Fatal("没有收到任何日志块")
	}
	var expectOff int64
	for i, c := range chunks {
		if c.Offset != expectOff {
			t.Fatalf("第 %d 个块偏移不连续：got %d want %d", i, c.Offset, expectOff)
		}
		expectOff += int64(len(c.Data))
	}
	if expectOff != size {
		t.Fatalf("发布的字节总数 %d 与文件大小 %d 不一致", expectOff, size)
	}

	// 文件内容必须是合法 UTF-8，且长度与断言一致。
	raw, err := os.ReadFile(w.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(raw) {
		t.Fatalf("日志文件里仍有非法 UTF-8：%q", raw)
	}
	if int64(len(raw)) != size {
		t.Fatalf("文件长度 %d 与报告的 size %d 不一致", len(raw), size)
	}
	if !strings.Contains(string(raw), "begin") || !strings.Contains(string(raw), "end") {
		t.Fatalf("正常内容丢失：%q", raw)
	}
}

// TestMaskerMasksWholeLines 验证打码：整行替换，且不会把缓冲区里的
// 半个 secret 先放出去。
func TestMaskerMasksWholeLines(t *testing.T) {
	var got strings.Builder
	m := NewMasker([]string{"s3cr3t-token"}, func(b []byte) { got.Write(b) })

	// 故意把一个 secret 切成两半写进来 —— 这是最容易被漏打码的情况。
	_, _ = m.Write([]byte("token is s3cr"))
	_, _ = m.Write([]byte("3t-token!\nnext line\n"))
	m.Flush()

	out := got.String()
	if strings.Contains(out, "s3cr3t-token") {
		t.Fatalf("secret 泄漏到日志里: %q", out)
	}
	if !strings.Contains(out, "token is ***!") {
		t.Fatalf("打码结果不符预期: %q", out)
	}
	if !strings.Contains(out, "next line") {
		t.Fatalf("正常内容被吞掉: %q", out)
	}
}

// TestMaskerAddSecrets 验证运行中追加秘密（Git 凭据在 Prepare 阶段才拿到）。
func TestMaskerAddSecrets(t *testing.T) {
	var got strings.Builder
	m := NewMasker(nil, func(b []byte) { got.Write(b) })
	m.AddSecrets([]string{"late-secret-value"})
	_, _ = m.Write([]byte("using late-secret-value now\n"))
	m.Flush()

	if strings.Contains(got.String(), "late-secret-value") {
		t.Fatalf("后加入的 secret 没有被打码: %q", got.String())
	}
}

// TestMaskerIgnoresShortSecrets 太短的值不打码，否则正常日志会变成马赛克。
func TestMaskerIgnoresShortSecrets(t *testing.T) {
	var got strings.Builder
	m := NewMasker([]string{"ab"}, func(b []byte) { got.Write(b) })
	_, _ = m.Write([]byte("about\n"))
	m.Flush()
	if got.String() != "about\n" {
		t.Fatalf("短 secret 不该参与打码，得到 %q", got.String())
	}
}

// TestWriterOffsetsAndReadRange 验证「落盘偏移」与「发布偏移」一致，
// 这是 WebSocket 去重和断线续传的正确性基础。
func TestWriterOffsetsAndReadRange(t *testing.T) {
	dir := t.TempDir()
	broker := NewBroker()
	sub := broker.Subscribe(7)
	defer broker.Unsubscribe(7, sub)

	w, err := NewWriter(dir, "proj", 7, broker, nil, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("world\n")); err != nil {
		t.Fatal(err)
	}
	size := w.Close()
	if size != int64(len("hello\nworld\n")) {
		t.Fatalf("大小不对: %d", size)
	}

	// 发布的偏移必须与文件内容对得上。
	c1 := <-sub.Ch
	if c1.Offset != 0 || string(c1.Data) != "hello\n" {
		t.Fatalf("第一个 chunk 偏移/内容不对: %+v", c1)
	}
	c2 := <-sub.Ch
	if c2.Offset != 6 || string(c2.Data) != "world\n" {
		t.Fatalf("第二个 chunk 偏移/内容不对: %+v", c2)
	}

	// 按 offset 增量读取。
	data, next, eof, err := ReadRange(w.Path(), 6, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "world\n" || next != 12 || !eof {
		t.Fatalf("ReadRange 结果不对: data=%q next=%d eof=%v", data, next, eof)
	}

	broker.CloseRun(7)
	if _, ok := <-sub.Ch; ok {
		t.Fatal("CloseRun 之后订阅通道应该已关闭")
	}
}

// TestWriterTruncatesAtLimit 日志超过上限要截断并明确提示。
func TestWriterTruncatesAtLimit(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir, "proj", 8, nil, nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		_, _ = w.Write([]byte("0123456789\n"))
	}
	w.Close()

	raw, err := os.ReadFile(w.Path())
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(raw)) > 120 {
		t.Fatalf("日志没有被限制住，长度 %d", len(raw))
	}
	if !strings.Contains(string(raw), "日志已达上限") {
		t.Fatalf("缺少截断提示: %q", string(raw))
	}
}

// TestPathForIsConsistent 路径推导必须唯一且安全。
func TestPathForIsConsistent(t *testing.T) {
	p := PathFor("/data/logs", "my-app", 3)
	want := filepath.Join("/data/logs", "my-app", "3.log")
	if filepath.ToSlash(p) != filepath.ToSlash(want) {
		t.Fatalf("路径不符: %s != %s", p, want)
	}

	// 目录穿越必须被消解：恶意 key 只能变成 logDir 下的**一个**目录名，
	// 不能真的跳到别的目录去。
	//
	// 注意断言的是「消解后的名字里不含分隔符、也不是 . 或 ..」，
	// 而不是「名字里不含 ..」—— `.._.._etc` 是合法的单级目录名，是安全的；
	// 只要它没有把路径拆成多级，就跳不出去。
	for _, evil := range []string{"../../etc", `..\..\windows`, "a/b/c", "..", "."} {
		got := PathFor("/data/logs", evil, 1)
		dir := filepath.Dir(got)
		name := filepath.Base(dir)

		if want := filepath.Join(filepath.FromSlash("/data/logs"), name); dir != want {
			t.Fatalf("key=%q 产生了多级目录: dir=%q 期望 %q", evil, dir, want)
		}
		if name == "" || name == "." || name == ".." {
			t.Fatalf("key=%q 消解出了非法目录名: %q", evil, name)
		}
		if strings.ContainsAny(name, `/\`) {
			t.Fatalf("key=%q 消解后的目录名仍含分隔符: %q", evil, name)
		}
	}
}
