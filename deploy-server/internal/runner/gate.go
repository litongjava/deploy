package runner

import "sync"

// gate 是一个**上限可动态调整**的并发闸门。
//
// 为什么不用带缓冲 channel：channel 的容量建好就改不了，
// 而「最大并发数」是可以在系统设置里随时改的；为了让这个设置真的生效
// （而不是提示用户去重启），这里用条件变量自己实现。
type gate struct {
	mu    sync.Mutex
	cond  *sync.Cond
	limit int
	inUse int
}

func newGate(limit int) *gate {
	if limit < 1 {
		limit = 1
	}
	g := &gate{limit: limit}
	g.cond = sync.NewCond(&g.mu)
	return g
}

// acquire 占一个槽位，满了就排队等待。
func (g *gate) acquire() {
	g.mu.Lock()
	for g.inUse >= g.limit {
		g.cond.Wait()
	}
	g.inUse++
	g.mu.Unlock()
}

func (g *gate) release() {
	g.mu.Lock()
	if g.inUse > 0 {
		g.inUse--
	}
	g.cond.Signal()
	g.mu.Unlock()
}

// setLimit 调整上限。调大时立刻唤醒排队者；调小时不影响正在跑的，
// 只是不再放新的进来。
func (g *gate) setLimit(n int) {
	if n < 1 {
		n = 1
	}
	g.mu.Lock()
	g.limit = n
	g.cond.Broadcast()
	g.mu.Unlock()
}

func (g *gate) limitNow() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.limit
}

func (g *gate) inFlight() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inUse
}
