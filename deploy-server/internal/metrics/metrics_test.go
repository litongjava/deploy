package metrics

import (
	"runtime"
	"testing"
	"time"
)

// 这些用例都跑在**当前平台**上。CI/开发机是 Windows，生产是 Linux，
// 两边各跑一次才算覆盖了 host_linux.go / host_windows.go。

func TestGetReturnsSaneSnapshot(t *testing.T) {
	info := Get()

	if info.Hostname == "" {
		t.Error("hostname 不应为空")
	}
	if info.OS != runtime.GOOS || info.Arch != runtime.GOARCH {
		t.Errorf("os/arch 不对: %s/%s", info.OS, info.Arch)
	}
	if info.Time.IsZero() {
		t.Error("time 不应为零值")
	}
	if info.UptimeSec <= 0 {
		t.Errorf("uptimeSec 应大于 0，得到 %d", info.UptimeSec)
	}
	if runtime.GOOS == "linux" && info.OSName == "" {
		t.Error("Linux 上应能读到 os-release 的 PRETTY_NAME")
	}
}

func TestCPUSnapshotIsConsistent(t *testing.T) {
	// 先建基准（Get 会启动后台采样），再手动触发一次采样拿到差分。
	// 不依赖采样周期，可以避免用例睡上好几秒。
	Get()
	time.Sleep(300 * time.Millisecond)
	SampleNow()

	info := Get()
	cpu := info.CPU

	if cpu.Cores < 1 {
		t.Fatalf("cores 应至少为 1，得到 %d", cpu.Cores)
	}
	if runtime.GOOS == "linux" {
		if cpu.Model == "" {
			t.Error("Linux 上应能从 /proc/cpuinfo 读到 CPU 型号")
		}
		if cpu.PhysicalCores <= 0 {
			t.Error("Linux 上应能算出物理核数")
		}
		if cpu.PhysicalCores > cpu.Cores {
			t.Errorf("物理核数不应超过逻辑核数: phys=%d cores=%d", cpu.PhysicalCores, cpu.Cores)
		}
	}

	if !cpu.Available {
		t.Fatal("第二次采样后 cpu.available 应为 true")
	}
	if cpu.UsagePercent < 0 || cpu.UsagePercent > 100 {
		t.Errorf("usagePercent 超出 0~100: %v", cpu.UsagePercent)
	}

	// 这个断言是这个用例的核心：所有核心累加的 CPU 时间应当约等于
	// 采样窗口 × 逻辑核心数。它一次验证了三件事——
	//   1) 计数单位换算（Linux 的 USER_HZ=100 / Windows 的 100ns）；
	//   2) 差分的正确性（不是拿开机至今的累计值在算）；
	//   3) 多核口径（CPU 时间是按核心累加的，不是墙上时钟）。
	// 单位一旦写错，这里会差 10 倍或 10^5 倍。
	if cpu.SampleWindowSec <= 0 {
		t.Fatalf("sampleWindowSec 应大于 0，得到 %v", cpu.SampleWindowSec)
	}
	want := cpu.SampleWindowSec * float64(cpu.Cores)
	// 容差：各项 round2 的取整误差，加上两次采样之间读取 hostInfo 的耗时。
	tolerance := want*0.2 + 0.5
	if diff := cpu.TotalSec - want; diff > tolerance || diff < -tolerance {
		t.Errorf("totalSec=%.2f 与 采样窗口 %.2fs × %d 核 = %.2f 不匹配（容差 %.2f）",
			cpu.TotalSec, cpu.SampleWindowSec, cpu.Cores, want, tolerance)
	}
	// 各分段之和必须等于总量，否则「使用率」和明细对不上。
	parts := cpu.BusySec + cpu.IdleSec
	if diff := parts - cpu.TotalSec; diff > 0.5 || diff < -0.5 {
		t.Errorf("busy(%.2f)+idle(%.2f)=%.2f 与 total(%.2f) 不一致",
			cpu.BusySec, cpu.IdleSec, parts, cpu.TotalSec)
	}
	// 使用率必须与明细自洽，避免「进度条 90% 而 busy/total 写 10%」。
	// 这里直接做除法而不是复用 UsagePercent：后者只接受整数，
	// 传 round2 过的秒数会把误差放大。
	if cpu.TotalSec > 0 {
		recomputed := cpu.BusySec / cpu.TotalSec * 100
		if diff := recomputed - cpu.UsagePercent; diff > 0.5 || diff < -0.5 {
			t.Errorf("usagePercent=%.1f 与 busy/total 推出的 %.1f 不一致",
				cpu.UsagePercent, recomputed)
		}
	}
}

func TestMemorySnapshot(t *testing.T) {
	info := Get()
	m := info.Memory

	if m.TotalBytes == 0 {
		t.Fatal("totalBytes 应大于 0")
	}
	if m.UsedBytes > m.TotalBytes {
		t.Errorf("已用内存超过总内存: used=%d total=%d", m.UsedBytes, m.TotalBytes)
	}
	if m.UsagePercent < 0 || m.UsagePercent > 100 {
		t.Errorf("usagePercent 超出 0~100: %v", m.UsagePercent)
	}
	if m.SwapUsedBytes > m.SwapTotalBytes {
		t.Errorf("已用交换分区超过总量: used=%d total=%d", m.SwapUsedBytes, m.SwapTotalBytes)
	}
}

func TestDiskSnapshot(t *testing.T) {
	info := Get()
	if len(info.Disks) == 0 {
		t.Fatal("至少应识别出一个文件系统")
	}

	for _, d := range info.Disks {
		if d.Path == "" {
			t.Errorf("磁盘缺少 path: %+v", d)
		}
		if d.TotalBytes == 0 {
			t.Errorf("磁盘 %s 的 totalBytes 为 0", d.Path)
		}
		if d.UsedBytes > d.TotalBytes {
			t.Errorf("磁盘 %s 已用超过总量: %+v", d.Path, d)
		}
		if d.UsagePercent < 0 || d.UsagePercent > 100 {
			t.Errorf("磁盘 %s 使用率越界: %v", d.Path, d.UsagePercent)
		}
	}

	// Linux 上根分区必须排第一，否则页面第一眼看到的可能是 /boot 这种小分区。
	if runtime.GOOS == "linux" && info.Disks[0].Path != "/" {
		t.Errorf("Linux 上根分区应排在最前，得到 %s", info.Disks[0].Path)
	}
}

func TestUsagePercent(t *testing.T) {
	cases := []struct {
		used, total uint64
		want        float64
	}{
		{0, 0, 0},       // 不能产生 NaN
		{50, 100, 50},   // 整除
		{1, 3, 33.3},    // 保留一位小数
		{100, 100, 100}, // 上界
		{0, 100, 0},     // 下界
		{2, 3, 66.7},    // 四舍五入方向
	}
	for _, c := range cases {
		if got := UsagePercent(c.used, c.total); got != c.want {
			t.Errorf("UsagePercent(%d, %d) = %v, 期望 %v", c.used, c.total, got, c.want)
		}
	}
}

func TestClampPercent(t *testing.T) {
	// 计数器差分会算出 100.0000001 这种值，前端进度条会因此报警告。
	if got := clampPercent(100.0000001); got != 100 {
		t.Errorf("clampPercent(100.0000001) = %v, 期望 100", got)
	}
	if got := clampPercent(-0.5); got != 0 {
		t.Errorf("clampPercent(-0.5) = %v, 期望 0", got)
	}
	if got := clampPercent(42.55); got != 42.6 {
		t.Errorf("clampPercent(42.55) = %v, 期望 42.6", got)
	}
}

// TestGetReturnsCopy 确保外部改动不会污染内部快照。
func TestGetReturnsCopy(t *testing.T) {
	first := Get()
	if len(first.Disks) == 0 {
		t.Skip("当前平台没有磁盘数据")
	}
	original := first.Disks[0].Path
	first.Disks[0].Path = "tampered"

	second := Get()
	if second.Disks[0].Path != original {
		t.Fatalf("Get 返回的切片被外部修改后污染了内部快照: %q", second.Disks[0].Path)
	}
}
