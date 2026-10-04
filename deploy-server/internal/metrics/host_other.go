//go:build !linux && !windows

// 其它 Unix（darwin / freebsd 等）只做「能编译、能出一份不完整数据」的最低实现。
//
// 本项目的生产目标是 Linux（见 deploy-server/README.md 与 systemd 部署脚本），
// 开发机是 Windows，这两个平台有完整实现。剩下的平台不值得为它维护
// sysctl / libproc 两套采集代码，所以这里只返回静态信息和磁盘容量，
// 采集不到的项目通过 Warnings 明确说明，而不是静默显示 0。
package metrics

import (
	"runtime"
	"syscall"
)

// cpuUnitPerSec 在此分支不参与换算（没有 CPU 差分数据），保留定义以统一编译。
const cpuUnitPerSec = 1

func readHostInfo() hostInfo {
	return hostInfo{
		hostname:   hostnameOrEmpty(),
		osName:     runtime.GOOS,
		cpuCores:   runtime.NumCPU(),
		collectErr: []string{"当前平台（" + runtime.GOOS + "）暂不支持 CPU / 内存采集，仅显示磁盘容量"},
		disks:      listDiskRefs(),
	}
}

func readCPUTimes() (cpuTimes, error) {
	return cpuTimes{}, syscall.ENOSYS
}

func readLoad() Load { return Load{} }

func readMem() (Memory, error) { return Memory{}, syscall.ENOSYS }

// hostUptimeSec 无系统调用可用时返回 0（前端显示 "-"）。
func hostUptimeSec() int64 { return 0 }

// listDiskRefs 只列根分区：没有 /proc/mounts 也没有注册表，
// 遍历挂载点需要 getfsstat，成本高于收益。
func listDiskRefs() []diskRef {
	return []diskRef{{path: "/", device: "/", fstype: ""}}
}

func readDiskUsage(refs []diskRef) ([]Disk, error) {
	out := make([]Disk, 0, len(refs))
	for _, r := range refs {
		var st syscall.Statfs_t
		if err := syscall.Statfs(r.path, &st); err != nil {
			continue
		}
		bsize := uint64(st.Bsize)
		total := st.Blocks * bsize
		free := st.Bfree * bsize
		used := uint64(0)
		if total > free {
			used = total - free
		}
		out = append(out, Disk{
			Path: r.path, Device: r.device, FSType: r.fstype,
			TotalBytes: total, UsedBytes: used, FreeBytes: st.Bavail * bsize,
			UsagePercent: UsagePercent(used, total),
		})
	}
	return out, nil
}
