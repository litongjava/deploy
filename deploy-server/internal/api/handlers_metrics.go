package api

import (
	"github.com/gin-gonic/gin"

	"deploy-server/internal/metrics"
)

// 服务器资源监控接口（页面：顶层菜单「服务器」）。
//
// 数据全部来自 internal/metrics —— 那里有一个后台采样协程按固定间隔刷新快照，
// 所以这个 handler 只是读一份内存里的结构，不阻塞、也不做任何系统调用。
// 这正是能高频轮询（前端 5 秒一次）的前提。
//
// 关于 CPU 使用率的一个重要事实：它是**两次采样之间的差分**，
// 不是「开机至今的平均值」。后者在开机很久的机器上几乎恒为 30% 上下，
// 看着像坏了。因此服务刚起来、第一次采样还没完成时
// cpu.available 为 false，前端显示「采样中」而不是 0%。

// 磁盘使用率的告警阈值（百分比）。与前端进度条的颜色保持一致，
// 放在后端算出来是为了将来做通知/告警时口径统一。
const (
	diskWarnPercent = 75.0
	diskWarnFree    = 2 << 30 // 剩余不足 2 GiB 也提前告警（大磁盘按比例看不出来）
)

// serverInfo 返回服务器基本信息与资源占用。
//
// 权限：任意登录用户可见（与 /system/doctor 一致）。
// 这里只暴露聚合指标，不含任何配置细节（工作目录、密钥、令牌等一律不出现）。
func (s *Server) serverInfo(c *gin.Context) {
	info := metrics.Get()

	OK(c, gin.H{
		"time":      info.Time,
		"hostname":  info.Hostname,
		"os":        info.OS,
		"arch":      info.Arch,
		"osName":    info.OSName,
		"kernel":    info.Kernel,
		"uptimeSec": info.UptimeSec,
		"load":      info.Load,
		"cpu":       info.CPU,
		"memory":    info.Memory,
		"disks":     diskDTOs(info.Disks),
		// warnings 让前端能解释「某项为什么是空的」，而不是让用户对着 0 猜。
		"warnings": info.Warnings,
	})
}

// diskDTO 是磁盘条目的对外形状：在采集结果上补一个 status，
// 让前端不必自己重复实现阈值判断。
type diskDTO struct {
	metrics.Disk
	Status string `json:"status"` // ok | warn
}

func diskDTOs(disks []metrics.Disk) []diskDTO {
	out := make([]diskDTO, 0, len(disks))
	for _, d := range disks {
		out = append(out, diskDTO{Disk: d, Status: diskStatus(d)})
	}
	return out
}

func diskStatus(d metrics.Disk) string {
	if d.TotalBytes == 0 {
		return "ok"
	}
	if d.UsagePercent >= diskWarnPercent || d.FreeBytes < diskWarnFree {
		return "warn"
	}
	return "ok"
}
