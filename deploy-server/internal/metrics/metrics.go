// Package metrics 采集服务器自身的资源使用情况（CPU / 内存 / 磁盘）。
//
// 为什么单独一个包、而不是直接写在 api handler 里：
//  1. 采集逻辑必须按平台分文件（Linux 读 /proc，Windows 走 Win32 API），
//     混在 HTTP 层里会把 handler 文件切成三四份带 build tag 的碎片；
//  2. 这里的东西是纯函数，能脱离 HTTP 单测（见 metrics_test.go）；
//  3. 「服务器信息」页面和以后可能加的告警/通知都需要同一份数据。
//
// 采集策略：后台每 Interval 采一次，接口读缓存。
//
// CPU 使用率只能是**两次采样之间的差分**，不能是「开机至今的平均值」——
// 后者在已经运行了很久的机器上几乎永远显示 30% 左右，看着像坏了。
// 所以这里必须维护「上一次」的 CPU 计数：用一个后台 goroutine 定时刷新，
// 接口只管读最近一次快照，既不用在请求里 sleep，也不会因为没人访问而断档。
package metrics

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

// Interval 是后台采样间隔。CPU 使用率对采样窗口敏感：
// 太短（<1s）抖动大，太长（>10s）看不出瞬间尖峰。3s 是经验值。
const Interval = 3 * time.Second

// Info 是一次完整采集的结果，也是接口返回的 data 结构。
type Info struct {
	Time     time.Time `json:"time"`
	Hostname string    `json:"hostname"`
	OS       string    `json:"os"`
	Arch     string    `json:"arch"`
	// OSName 是给人看的发行版名称（Linux 取 /etc/os-release 的 PRETTY_NAME），
	// 取不到时为空，前端回退到 os/arch。
	OSName    string `json:"osName"`
	Kernel    string `json:"kernel"`
	UptimeSec int64  `json:"uptimeSec"`

	Load   Load   `json:"load"`
	CPU    CPU    `json:"cpu"`
	Memory Memory `json:"memory"`
	Disks  []Disk `json:"disks"`
	// Warnings 收集采集过程中失败的项目，用于在前端说明「为什么某项是空的」，
	// 而不是让用户对着一个 0 猜。空切片不是 nil，方便前端直接 .length。
	Warnings []string `json:"warnings"`
}

// Load 是 Unix 的平均负载。Windows 没有等价概念，Available=false。
type Load struct {
	Available bool    `json:"available"`
	Avg1      float64 `json:"avg1"`
	Avg5      float64 `json:"avg5"`
	Avg15     float64 `json:"avg15"`
}

// CPU 描述处理器型号、核心数与使用率。
type CPU struct {
	Model string `json:"model"`
	// Cores 是逻辑核心数（含超线程），PhysicalCores 是物理核数，取不到时为 0。
	Cores         int `json:"cores"`
	PhysicalCores int `json:"physicalCores"`
	MHz           int `json:"mhz"`

	// UsagePercent 是 0~100 的百分比，采样窗口见 Interval。
	// 第一次采样之前（服务刚启动）Available=false，前端应显示「采样中」。
	Available    bool    `json:"available"`
	UsagePercent float64 `json:"usagePercent"`

	// SampleWindowSec 是本次使用率的统计窗口（两次采样之间的秒数）。
	// 刚启动时为 0。暴露它是为了透明：用户看到的 30% 到底是「最近 3 秒」的
	// 还是「最近 5 分钟」的，差别很大。
	SampleWindowSec float64 `json:"sampleWindowSec"`

	// 以下为最近一个采样窗口内各状态的累计时间（秒），供前端展示明细。
	//
	// 重要：这些是**所有核心累加**的时间，不是墙上时钟。
	// 16 核机器跑满 1 秒会得到 TotalSec ≈ 16，而不是 1——
	// 这正是 UsagePercent = BusySec / TotalSec 能成立的原因
	// （分子分母同口径，直接消掉了核心数）。
	BusySec   float64 `json:"busySec"`
	TotalSec  float64 `json:"totalSec"`
	UserSec   float64 `json:"userSec"`
	SystemSec float64 `json:"systemSec"`
	IdleSec   float64 `json:"idleSec"`
	IowaitSec float64 `json:"iowaitSec"`
}

// Memory 描述物理内存与交换分区。
type Memory struct {
	TotalBytes     uint64  `json:"totalBytes"`
	UsedBytes      uint64  `json:"usedBytes"`
	FreeBytes      uint64  `json:"freeBytes"`
	AvailableBytes uint64  `json:"availableBytes"`
	UsagePercent   float64 `json:"usagePercent"`

	// Swap* 是「比物理内存多出来的那部分」的用量。
	//
	// 它的口径在两个平台上并不相同，所以额外给出 SwapKind / SwapLabel，
	// 让前端照实显示标题，而不是在 Windows 上硬写「交换分区」：
	//   linux   -> swap   交换分区（/proc/meminfo 的 SwapTotal/SwapFree）
	//   windows -> commit 提交内存（物理内存 + 页面文件的承诺量，见 host_windows.go）
	SwapTotalBytes uint64 `json:"swapTotalBytes"`
	SwapUsedBytes  uint64 `json:"swapUsedBytes"`
	SwapFreeBytes  uint64 `json:"swapFreeBytes"`
	SwapKind       string `json:"swapKind"`
	SwapLabel      string `json:"swapLabel"`
}

// Disk 是单个文件系统/卷的容量信息。
type Disk struct {
	// Path 在 Linux 上是挂载点（/、/home），在 Windows 上是卷根（C:\）。
	Path string `json:"path"`
	// Device 是设备名（/dev/sda1、C:），只做展示用。
	Device string `json:"device"`
	// FSType 是文件系统类型（ext4、xfs、NTFS）；取不到时为空。
	FSType string `json:"fstype"`

	TotalBytes   uint64  `json:"totalBytes"`
	UsedBytes    uint64  `json:"usedBytes"`
	FreeBytes    uint64  `json:"freeBytes"`
	UsagePercent float64 `json:"usagePercent"`
}

// hostInfo 是「启动后不会变」的信息，只在第一次采集时读一次。
type hostInfo struct {
	hostname string
	osName   string
	kernel   string
	cpuModel string
	cpuCores int
	cpuPhys  int
	cpuMHz   int
	disks    []diskRef // 文件系统清单；容量每次重新读
	// collectErr 记录静态信息读取中的失败项，透传到 Info.Warnings。
	collectErr []string
}

// diskRef 是一个待查询容量的文件系统。
type diskRef struct {
	path   string
	device string
	fstype string
}

var (
	once   sync.Once
	mu     sync.Mutex // 保护 latest / progress（后台采样写，请求读）
	latest Info

	// progress 是上一次的 CPU 计数。CPU 使用率是差分算出来的，这是基准。
	progress cpuProgress

	sampler *Sampler

	hostOnce  sync.Once
	hostCache *hostInfo
)

// Sampler 持有后台采样循环。它由 Get 懒启动，不需要在 main 里显式初始化——
// 少一个「忘了调 Start 就拿到全 0」的坑。
type Sampler struct {
	interval time.Duration
	stop     chan struct{}
	stopOnce sync.Once
}

// Start 启动后台采样（幂等）。interval <= 0 时使用默认 Interval。
func Start(interval time.Duration) {
	once.Do(func() {
		if interval <= 0 {
			interval = Interval
		}
		// 先同步采一次：建立 CPU 差分基准，之后用户打开页面立刻能看到非 0 的使用率。
		collectAndStore()

		s := &Sampler{interval: interval, stop: make(chan struct{})}
		sampler = s
		go s.loop()
	})
}

func (s *Sampler) loop() {
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			collectAndStore()
		}
	}
}

// Stop 停掉后台采样（测试与优雅退出用）。未启动时安全。
func Stop() {
	if sampler != nil {
		sampler.stopOnce.Do(func() { close(sampler.stop) })
	}
}

// SampleNow 立即同步采一次，主要用于测试（不必等一个采样周期）。
//
// 与后台循环并发调用是安全的：collectAndStore 用 mu 把整个采集串行化，
// 所以不会出现两次采集交错读写 progress、把 CPU 差分算乱的情况。
func SampleNow() {
	Start(0)
	collectAndStore()
}

// Get 返回最近一次采集结果，并在首次调用时启动后台采样。
//
// 返回的是**快照的副本**：切片会复制一份，
// 避免调用方（JSON 序列化、告警逻辑）与后台采样协程共享底层数组。
func Get() Info {
	Start(0)
	mu.Lock()
	defer mu.Unlock()
	out := latest
	if len(latest.Disks) > 0 {
		out.Disks = append([]Disk(nil), latest.Disks...)
	}
	if len(latest.Warnings) > 0 {
		out.Warnings = append([]string(nil), latest.Warnings...)
	}
	if out.Disks == nil {
		out.Disks = []Disk{}
	}
	if out.Warnings == nil {
		out.Warnings = []string{}
	}
	return out
}

// collectAndStore 采一次并覆盖快照。
//
// mu 在**整个采集过程**里持有，而不是只在最后写快照时拿一下：
// progress 是「上一次的 CPU 计数」，读它、算差分、写回必须是原子的，
// 否则后台定时采样和 SampleNow（用户手动刷新 / 测试）并发时会交错，
// 把差分窗口算成负数或一个超长窗口，屏幕上就是 CPU 使用率乱跳。
// 采集本身只是几个文件读 + 几次 syscall，毫秒级，不会成为读接口的瓶颈。
func collectAndStore() {
	mu.Lock()
	defer mu.Unlock()

	host := hostInfoOnce()

	info := Info{
		Time:      time.Now(),
		Hostname:  host.hostname,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		OSName:    host.osName,
		Kernel:    host.kernel,
		UptimeSec: hostUptimeSec(),
		Load:      readLoad(),
		Warnings:  append([]string(nil), host.collectErr...),
		Disks:     []Disk{},
	}

	info.CPU = readCPU(host)

	if mem, err := readMem(); err == nil {
		info.Memory = mem
	} else {
		info.Warnings = append(info.Warnings, "读取内存信息失败："+err.Error())
	}

	if disks, err := readDiskUsage(host.disks); err == nil {
		info.Disks = disks
	} else {
		info.Warnings = append(info.Warnings, "读取磁盘信息失败："+err.Error())
	}

	latest = info
}

// hostInfoOnce 保证静态信息只读一次（它不会变，也没必要每 3 秒重新探测一遍）。
// 即使读取完全失败也返回一个非 nil 的空结构，让上层不必到处判空。
func hostInfoOnce() *hostInfo {
	hostOnce.Do(func() {
		h := readHostInfo()
		hostCache = &h
	})
	if hostCache == nil {
		hostCache = &hostInfo{}
	}
	return hostCache
}

// withTimeout 跑一个外部命令并限制耗时，返回标准输出。
//
// 目前没有平台实现用到它（Linux 读 /proc、Windows 读注册表都不需要起进程），
// 保留它是为了将来需要探测外部工具时有个带超时的入口——这类探测可能因为
// 系统繁忙或权限问题卡住，绝不能让它拖死采样协程。
func withTimeout(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// firstLine 取第一行并做长度保护（有些命令会输出很长的警示信息）。
func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' || s[i] == '\r' {
			s = s[:i]
			break
		}
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

var _ = withTimeout
var _ = firstLine

// UsagePercent 由 used/total 算百分比，total 为 0 时返回 0（而不是 NaN）。
func UsagePercent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	p := float64(used) / float64(total) * 100
	// 保留一位小数就够了，也避免前端出现 33.33333333333333 这种噪音。
	return float64(int(p*10+0.5)) / 10
}

// hostnameOrEmpty 是 os.Hostname 的容错包装。
func hostnameOrEmpty() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return ""
}

// cpuProgress 保存上一次的 CPU 计数与时刻，用于差分。
type cpuProgress struct {
	at    time.Time
	times cpuTimes
	ok    bool
}

// cpuTimes 是一次 CPU 计数读数，单位由各平台自定义（Linux 是 jiffies，Windows 是 100ns）。
type cpuTimes struct {
	user, system, idle, iowait float64
	busy, total                float64
}

// readCPU 读一次 CPU 计数，与上一次比较算出这段时间的使用率，
// 顺手把新的计数存进 progress 供下次差分。
//
// 第一次调用没有基准，返回 Available=false（前端显示「采样中」），
// 不做「换算成开机至今平均值」这种会长期失真的补救。
func readCPU(host *hostInfo) CPU {
	out := CPU{
		Model:         host.cpuModel,
		Cores:         host.cpuCores,
		PhysicalCores: host.cpuPhys,
		MHz:           host.cpuMHz,
	}
	if out.Cores <= 0 {
		out.Cores = runtime.NumCPU()
	}

	now := time.Now()
	cur, err := readCPUTimes()
	if err != nil {
		warn("read cpu times failed", "err", err)
		return out
	}

	prev := progress
	progress = cpuProgress{at: now, times: cur, ok: true}

	if !prev.ok {
		return out
	}
	busyDelta := cur.busy - prev.times.busy
	totalDelta := cur.total - prev.times.total
	// total 不增长说明计数器被重置（极少见），保留 Available=false 等下一轮。
	if busyDelta < 0 || totalDelta <= 0 {
		return out
	}

	out.Available = true
	out.UsagePercent = clampPercent(busyDelta / totalDelta * 100)
	out.SampleWindowSec = round2(now.Sub(prev.at).Seconds())
	out.BusySec = round2(busyDelta / cpuUnitPerSec)
	out.TotalSec = round2(totalDelta / cpuUnitPerSec)
	out.UserSec = round2((cur.user - prev.times.user) / cpuUnitPerSec)
	out.SystemSec = round2((cur.system - prev.times.system) / cpuUnitPerSec)
	out.IdleSec = round2((cur.idle - prev.times.idle) / cpuUnitPerSec)
	out.IowaitSec = round2((cur.iowait - prev.times.iowait) / cpuUnitPerSec)
	return out
}

// clampPercent 把百分比夹到 [0,100]，并对 -0 做归零。
// 计数器差分偶尔会因为调度/取整算出 100.0000001 这种值，前端进度条会报警告。
func clampPercent(p float64) float64 {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return float64(int(p*10+0.5)) / 10
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

// warn 记录一次采集过程中的问题。用 Debug 级别：这类失败几乎都是
// 「某个文件不存在 / 某个卷查不到」，属于可降级的信息，不该刷满生产日志。
func warn(msg string, args ...any) {
	slog.Debug("metrics: "+msg, args...)
}
