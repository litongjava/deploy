package ws

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"deploy-cli/internal/api"
	"deploy-cli/internal/exitcode"
)

// ---------- 测试替身 ----------

type fakeLogs struct {
	mu       sync.Mutex
	data     string
	statuses []string // 依次返回；耗尽后沿用最后一个
	runCalls int
	logCalls int
	logErr   error
	runErr   error
	exitCode *int
	duration int64
}

func (f *fakeLogs) RunLog(_ context.Context, _ int64, offset int64, limit int) (*api.LogChunk, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logCalls++
	if f.logErr != nil {
		return nil, f.logErr
	}
	total := int64(len(f.data))
	if offset > total {
		offset = total
	}
	end := offset + int64(limit)
	eof := true
	if end >= total {
		end = total
	} else {
		eof = false
	}
	return &api.LogChunk{
		Offset:     offset,
		NextOffset: end,
		EOF:        eof,
		Data:       f.data[offset:end],
	}, nil
}

func (f *fakeLogs) GetRun(_ context.Context, _ int64) (*api.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runCalls++
	if f.runErr != nil {
		return nil, f.runErr
	}
	status := "running"
	if len(f.statuses) > 0 {
		idx := f.runCalls - 1
		if idx >= len(f.statuses) {
			idx = len(f.statuses) - 1
		}
		status = f.statuses[idx]
	}
	return &api.Run{ID: 1, Status: status, ExitCode: f.exitCode, DurationMs: f.duration}, nil
}

func (f *fakeLogs) setData(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data = s
}

type fakeConn struct {
	mu     sync.Mutex
	frames []*Frame
	err    error
	closed bool
	idx    int
}

func (c *fakeConn) ReadFrame() (*Frame, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.idx < len(c.frames) {
		f := c.frames[c.idx]
		c.idx++
		return f, nil
	}
	if c.err != nil {
		return nil, c.err
	}
	return nil, errors.New("connection reset by peer")
}

func (c *fakeConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

type fakeStream struct {
	mu    sync.Mutex
	conns []*fakeConn
	hooks []func() // 与 conns 一一对应，在建连前更新「服务端日志文件」
	err   error
	calls int
	last  int64
}

func (s *fakeStream) StreamLogs(_ context.Context, _ int64, offset int64) (Conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.last = offset
	if s.err != nil {
		return nil, s.err
	}
	idx := s.calls - 1
	if idx >= len(s.conns) {
		return nil, errors.New("no more fake connections")
	}
	if idx < len(s.hooks) && s.hooks[idx] != nil {
		s.hooks[idx]()
	}
	return s.conns[idx], nil
}

func intPtr(i int) *int { return &i }

func fastOptions(w *bytes.Buffer) Options {
	return Options{
		RunID:      1,
		Writer:     w,
		MinBackoff: time.Millisecond,
		MaxBackoff: 2 * time.Millisecond,
		Sleep:      func(context.Context, time.Duration) error { return nil },
	}
}

// ---------- 用例 ----------

// 运行已经结束时：只用 HTTP 补齐历史，不建 WebSocket 连接。
func TestFollowTerminalRunUsesOnlyHTTP(t *testing.T) {
	logs := &fakeLogs{data: "hello world", statuses: []string{"success"}, exitCode: intPtr(0)}
	stream := &fakeStream{}
	var out bytes.Buffer
	opts := fastOptions(&out)
	f := &Follower{Logs: logs, Stream: stream}

	res, err := f.Follow(context.Background(), opts)
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}
	if out.String() != "hello world" {
		t.Fatalf("日志 = %q", out.String())
	}
	if !res.Terminated || res.Status != "success" {
		t.Fatalf("结果 = %+v", res)
	}
	if stream.calls != 0 {
		t.Fatalf("终态运行不应连 WebSocket，实际连接 %d 次", stream.calls)
	}
	if got := res.ProcessExitCode(); got != exitcode.OK {
		t.Fatalf("退出码 = %d", got)
	}
}

// 历史日志分多块返回（eof=false），必须一直拉到 eof。
func TestCatchUpPullsUntilEOF(t *testing.T) {
	logs := &fakeLogs{data: "0123456789", statuses: []string{"success"}}
	var out bytes.Buffer
	opts := fastOptions(&out)
	opts.Limit = 4
	f := &Follower{Logs: logs, Stream: &fakeStream{}}

	res, err := f.Follow(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "0123456789" {
		t.Fatalf("日志 = %q", out.String())
	}
	if res.FinalOffset != 10 {
		t.Fatalf("FinalOffset = %d", res.FinalOffset)
	}
	if logs.logCalls < 3 {
		t.Fatalf("应至少分 3 次拉取，实际 %d 次", logs.logCalls)
	}
}

// 断线 → 重连 → 服务端重发历史（history 帧）不能造成重复输出，也不能丢日志。
func TestFollowReconnectContinuesWithoutDuplicates(t *testing.T) {
	logs := &fakeLogs{statuses: []string{"running", "running", "running", "success"}}
	logs.exitCode = intPtr(0)

	first := &fakeConn{
		frames: []*Frame{{Type: FrameLog, NextOffset: 5, Data: "AAAAA"}},
		err:    errors.New("unexpected close"),
	}
	second := &fakeConn{
		frames: []*Frame{
			{Type: FrameHistory, NextOffset: 5, Data: "AAAAA"}, // 服务端重发
			{Type: FrameLog, NextOffset: 10, Data: "BBBBB"},    // 新数据
		},
		err: errors.New("unexpected close"),
	}
	stream := &fakeStream{
		conns: []*fakeConn{first, second},
		hooks: []func(){
			func() { logs.setData("AAAAA") },
			func() { logs.setData("AAAAABBBBB") },
		},
	}
	var out bytes.Buffer
	opts := fastOptions(&out)
	opts.OnStatus = func(string, *Step) {}
	f := &Follower{Logs: logs, Stream: stream}

	res, err := f.Follow(context.Background(), opts)
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}
	if got := out.String(); got != "AAAAABBBBB" {
		t.Fatalf("日志 = %q, want %q（不允许重复或丢失）", got, "AAAAABBBBB")
	}
	if !res.Terminated || res.Status != "success" {
		t.Fatalf("结果 = %+v", res)
	}
	if res.Reconnects == 0 {
		t.Fatalf("应当记录到重连")
	}
	if res.FinalOffset != 10 {
		t.Fatalf("FinalOffset = %d", res.FinalOffset)
	}
	if got := res.ProcessExitCode(); got != exitcode.OK {
		t.Fatalf("退出码 = %d", got)
	}
}

// 老版本服务端会用 exit 帧通知「订阅者跟不上被踢掉」，此时运行并没有结束。
// 把它当结束处理会让日志永久缺尾，所以这里必须按 offset 继续跟。
func TestFollowIgnoresNonTerminalExitFrame(t *testing.T) {
	logs := &fakeLogs{statuses: []string{"running", "running", "success"}, exitCode: intPtr(0)}

	first := &fakeConn{
		frames: []*Frame{
			{Type: FrameLog, NextOffset: 5, Data: "AAAAA"},
			{Type: FrameExit, Status: "running", NextOffset: 5}, // 假 exit：运行还在跑
		},
		err: errors.New("connection closed by server"),
	}
	second := &fakeConn{
		frames: []*Frame{
			{Type: FrameLog, NextOffset: 10, Data: "BBBBB"},
			{Type: FrameExit, Status: "success", ExitCode: intPtr(0), NextOffset: 10},
		},
	}
	stream := &fakeStream{
		conns: []*fakeConn{first, second},
		hooks: []func(){
			func() { logs.setData("AAAAA") },
			func() { logs.setData("AAAAABBBBB") },
		},
	}
	var out bytes.Buffer
	opts := fastOptions(&out)
	f := &Follower{Logs: logs, Stream: stream}

	res, err := f.Follow(context.Background(), opts)
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}
	if got := out.String(); got != "AAAAABBBBB" {
		t.Fatalf("日志 = %q, want %q（非终态 exit 后必须继续补齐）", got, "AAAAABBBBB")
	}
	if !res.Terminated || res.Status != "success" {
		t.Fatalf("结果 = %+v", res)
	}
	if stream.calls < 2 {
		t.Fatalf("收到非终态 exit 后应当重连，实际只连了 %d 次", stream.calls)
	}
	if res.FinalOffset != 10 {
		t.Fatalf("FinalOffset = %d", res.FinalOffset)
	}
}

// exit 帧直接给出终态。
func TestFollowExitFrame(t *testing.T) {
	logs := &fakeLogs{statuses: []string{"running"}}
	conn := &fakeConn{frames: []*Frame{
		{Type: FrameLog, NextOffset: 3, Data: "abc"},
		{Type: FrameStatus, Status: "running", Step: &Step{Name: "构建", SortOrder: 2}},
		{Type: FrameExit, Status: "failed", ExitCode: intPtr(2), DurationMs: 1234, NextOffset: 3},
	}}
	stream := &fakeStream{conns: []*fakeConn{conn}}
	var out bytes.Buffer
	opts := fastOptions(&out)
	var gotStep *Step
	opts.OnStatus = func(_ string, s *Step) { gotStep = s }
	f := &Follower{Logs: logs, Stream: stream}

	res, err := f.Follow(context.Background(), opts)
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}
	if out.String() != "abc" {
		t.Fatalf("日志 = %q", out.String())
	}
	if res.Status != "failed" || res.ExitCode == nil || *res.ExitCode != 2 || res.DurationMs != 1234 {
		t.Fatalf("结果 = %+v", res)
	}
	if gotStep == nil || gotStep.Name != "构建" {
		t.Fatalf("status 帧未回调: %+v", gotStep)
	}
	if got := res.ProcessExitCode(); got != exitcode.DeployFailed {
		t.Fatalf("失败运行退出码 = %d, want %d", got, exitcode.DeployFailed)
	}
}

// 传输层故障应重试，超过 MaxRetries 后报网络错误（退出码 4）。
func TestFollowGivesUpAfterMaxRetries(t *testing.T) {
	logs := &fakeLogs{
		statuses: []string{"running"},
		logErr:   api.TransportError("请求", errors.New("connection refused")),
	}
	stream := &fakeStream{err: api.TransportError("连接", errors.New("connection refused"))}
	var out bytes.Buffer
	opts := fastOptions(&out)
	opts.MaxRetries = 2
	f := &Follower{Logs: logs, Stream: stream}

	if _, err := f.Follow(context.Background(), opts); err == nil {
		t.Fatal("期望报错")
	} else if got := exitcode.Of(err); got != exitcode.Network {
		t.Fatalf("退出码 = %d, want %d (%v)", got, exitcode.Network, err)
	}
}

// 鉴权失败不应无限重试。
func TestFollowStopsOnAuthError(t *testing.T) {
	authErr := &api.Error{HTTPStatus: 401, Code: api.CodeUnauthorized, Message: "未登录或登录已过期"}
	logs := &fakeLogs{statuses: []string{"running"}, runErr: authErr}
	stream := &fakeStream{}
	var out bytes.Buffer
	opts := fastOptions(&out)
	f := &Follower{Logs: logs, Stream: stream}

	_, err := f.Follow(context.Background(), opts)
	if err == nil {
		t.Fatal("期望报错")
	}
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Fatalf("退出码 = %d, want %d", got, exitcode.Auth)
	}
	if stream.calls != 0 {
		t.Fatalf("鉴权失败后不应继续连接")
	}
}

// Ctrl+C：ctx 取消 → Interrupted，不视为部署失败。
func TestFollowInterruptedByContext(t *testing.T) {
	logs := &fakeLogs{statuses: []string{"running"}}
	stream := &fakeStream{} // 永远返回 "no more fake connections"
	var out bytes.Buffer
	opts := fastOptions(&out)
	ctx, cancel := context.WithCancel(context.Background())
	opts.Sleep = func(c context.Context, _ time.Duration) error {
		cancel()
		return c.Err()
	}
	f := &Follower{Logs: logs, Stream: stream}

	res, err := f.Follow(ctx, opts)
	if !res.Interrupted {
		t.Fatalf("应标记为中断: %+v", res)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if got := res.ProcessExitCode(); got != exitcode.OK {
		t.Fatalf("中断但未取消运行时应返回 0，得到 %d", got)
	}
}

func TestDedup(t *testing.T) {
	cases := []struct {
		name  string
		data  string
		start int64
		known int64
		want  string
	}{
		{"no-overlap", "abcdef", 10, 10, "abcdef"},
		{"server-ahead", "abcdef", 12, 10, "abcdef"},
		{"partial-overlap", "abcdef", 8, 10, "cdef"},
		{"full-overlap", "abcdef", 4, 10, ""},
		{"negative-start", "abcdef", -1, 10, "abcdef"},
		{"zero-known", "abcdef", 0, 0, "abcdef"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dedup(tc.data, tc.start, tc.known); got != tc.want {
				t.Fatalf("dedup(%q,%d,%d) = %q, want %q", tc.data, tc.start, tc.known, got, tc.want)
			}
		})
	}
}

func TestResultProcessExitCode(t *testing.T) {
	cases := []struct {
		res  Result
		want int
	}{
		{Result{Status: api.StatusSuccess, Terminated: true}, 0},
		{Result{Status: api.StatusFailed, Terminated: true}, 1},
		{Result{Status: api.StatusTimeout, Terminated: true}, 1},
		{Result{Status: api.StatusError, Terminated: true}, 1},
		{Result{Status: api.StatusCanceled, Terminated: true}, 1},
		{Result{Status: api.StatusRunning}, 0},
	}
	for _, tc := range cases {
		if got := tc.res.ProcessExitCode(); got != tc.want {
			t.Fatalf("status=%q terminated=%v → %d, want %d", tc.res.Status, tc.res.Terminated, got, tc.want)
		}
	}
}

func TestNextBackoff(t *testing.T) {
	if got := nextBackoff(time.Second, 5*time.Second); got != 2*time.Second {
		t.Fatalf("got %s", got)
	}
	if got := nextBackoff(4*time.Second, 5*time.Second); got != 5*time.Second {
		t.Fatalf("got %s", got)
	}
}
