//go:build windows

package metrics

import (
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// cpuUnitPerSec 是 Windows 的 CPU 计数单位换算。
//
// GetSystemTimes 返回的是 FILETIME，单位 100ns，所以 1 秒 = 10^7 个单位。
// 与 Linux 分支的 100（jiffies）不同，这是 Windows 独有的口径。
const cpuUnitPerSec = 1e7

var (
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	psapi                  = windows.NewLazySystemDLL("psapi.dll")
	procGetSystemTimes     = kernel32.NewProc("GetSystemTimes")
	procGetTickCount64     = kernel32.NewProc("GetTickCount64")
	procGetPerformanceInfo = psapi.NewProc("GetPerformanceInfo")
)

// performanceInfo 对应 Win32 的 PERFORMANCE_INFORMATION。
//
// 用它而不是 MEMORYSTATUSEX 有两个原因：
//  1. CommitTotal/CommitLimit 就是任务管理器「已提交」那一栏的口径，
//     与「总量/已用/使用率」这套说法完全对应；
//  2. MEMORYSTATUSEX 里的 PageFile 字段是**页面文件大小**，不是已用的交换量。
//     用 TotalPageFile 当「交换分区总量」会在一台 16 GB 内存的机器上
//     显示 42 GB 交换，纯粹是误导。
type performanceInfo struct {
	Size              uint32
	CommitTotal       uintptr
	CommitLimit       uintptr
	CommitPeak        uintptr
	PhysicalTotal     uintptr
	PhysicalAvailable uintptr
	SystemCache       uintptr
	KernelTotal       uintptr
	KernelPaged       uintptr
	KernelNonpaged    uintptr
	PageSize          uintptr
	HandleCount       uint32
	ProcessCount      uint32
	ThreadCount       uint32
}

func readHostInfo() hostInfo {
	h := hostInfo{
		hostname: hostnameOrEmpty(),
		osName:   readOSName(),
		kernel:   readKernel(),
	}
	h.cpuModel, h.cpuMHz = readCPUModelFromRegistry()
	h.cpuCores = runtime.NumCPU()
	h.cpuPhys = 0 // Windows 没有便宜的 API 能拿到物理核数，前端会隐藏这一项
	h.disks = listDiskRefs()
	return h
}

// readCPUModelFromRegistry 读注册表里的 CPU 型号与频率。
//
// 走注册表而不是 wmic/PowerShell：前者是一次几百微秒的函数调用，
// 后者要起一个进程（几百毫秒）且 wmic 在新版 Windows 上已被移除。
// 注意 ProcessorNameString 是 REG_SZ，~MHz 是 REG_DWORD，类型不同要分别读。
func readCPUModelFromRegistry() (model string, mhz int) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`HARDWARE\DESCRIPTION\System\CentralProcessor\0`, registry.QUERY_VALUE)
	if err != nil {
		return "", 0
	}
	defer k.Close()

	if s, _, err := k.GetStringValue("ProcessorNameString"); err == nil {
		model = strings.TrimSpace(s)
	}
	if v, _, err := k.GetIntegerValue("~MHz"); err == nil {
		mhz = int(v)
	}
	return model, mhz
}

// readCPUTimes 用 GetSystemTimes 拿三个累计时间。
//
// 三个出参的顺序容易记错：idle、kernel、user。
// 而且 kernel **包含** idle，所以真正的系统态时间是 kernel - idle——
// 不减的话使用率会算高一截（在空闲机器上尤其明显）。
func readCPUTimes() (cpuTimes, error) {
	var idle, kernel, user syscall.Filetime
	r, _, err := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if r == 0 {
		return cpuTimes{}, err
	}

	i := filetimeUnits(idle)
	k := filetimeUnits(kernel)
	u := filetimeUnits(user)

	system := k - i
	if system < 0 {
		system = 0
	}
	busy := u + system
	total := busy + i
	return cpuTimes{
		user:   u,
		system: system,
		idle:   i,
		iowait: 0, // Windows 没有 iowait 概念
		busy:   busy,
		total:  total,
	}, nil
}

// filetimeUnits 把 FILETIME（100ns 单位，分高低位）转成 float 单位数。
// 先拼成 uint64 再转 float64：float64 只有 52 位尾数，分高低位分别转会把高位丢掉。
func filetimeUnits(ft syscall.Filetime) float64 {
	v := uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
	return float64(v)
}

// readLoad Windows 没有 load average，返回 Available=false 让前端隐藏这一项。
func readLoad() Load {
	return Load{}
}

// readMem 用 GetPerformanceInfo 拿物理内存与提交（commit）情况。
func readMem() (Memory, error) {
	var pi performanceInfo
	pi.Size = uint32(unsafe.Sizeof(pi))
	r, _, err := procGetPerformanceInfo.Call(uintptr(unsafe.Pointer(&pi)), uintptr(pi.Size))
	if r == 0 {
		return Memory{}, err
	}
	page := uint64(pi.PageSize)
	if page == 0 {
		page = 4096
	}

	total := uint64(pi.PhysicalTotal) * page
	available := uint64(pi.PhysicalAvailable) * page
	if total < available {
		total = available
	}
	used := total - available

	// 提交量（CommitTotal/CommitLimit）是 Windows 上最接近「交换分区」的东西：
	// 它是「已承诺给所有进程的虚拟内存」对「物理内存 + 页面文件」的占比。
	// 注意这不是已使用的页面文件大小，想要那个得查页面文件计数器（需要 PDH）。
	commit := uint64(pi.CommitTotal) * page
	commitLimit := uint64(pi.CommitLimit) * page
	commitFree := uint64(0)
	if commitLimit > commit {
		commitFree = commitLimit - commit
	}

	return Memory{
		TotalBytes:     total,
		UsedBytes:      used,
		FreeBytes:      available,
		AvailableBytes: available,
		UsagePercent:   UsagePercent(used, total),
		SwapTotalBytes: commitLimit,
		SwapUsedBytes:  commit,
		SwapFreeBytes:  commitFree,
		SwapKind:       "commit",
		SwapLabel:      "提交内存",
	}, nil
}

// readOSName 取注册表里的 ProductName（如 "Windows Server 2022 Datacenter"）。
func readOSName() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()

	name, _, err := k.GetStringValue("ProductName")
	if err != nil {
		return ""
	}
	name = strings.TrimSpace(name)

	// Windows 11 的 ProductName 至今仍写着 "Windows 10 Pro"（微软没改），
	// 直接用注册表值会把 Win11 机器报成 Win10。
	// 判定依据是构建号：22000 起为 Windows 11。
	if build := readBuildNumber(k); build >= 22000 {
		name = strings.Replace(name, "Windows 10", "Windows 11", 1)
	}
	return name
}

// readBuildNumber 取构建号（CurrentBuild 是 DWORD，CurrentBuildNumber 是字符串，
// 两者都可能缺失，所以都要试）。
func readBuildNumber(k registry.Key) int {
	if v, _, err := k.GetIntegerValue("CurrentBuild"); err == nil {
		return int(v)
	}
	if s, _, err := k.GetStringValue("CurrentBuildNumber"); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			return n
		}
	}
	return 0
}

// readKernel 用 CurrentBuildNumber 当版本号；Windows 的「内核版本」就是它。
func readKernel() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()

	// 优先带上 DisplayVersion（22H2 / 24H2），这比裸构建号好认得多。
	build := readBuildNumber(k)
	if build == 0 {
		return ""
	}
	out := strconv.Itoa(build)
	if v, _, err := k.GetStringValue("DisplayVersion"); err == nil && strings.TrimSpace(v) != "" {
		out += " (" + strings.TrimSpace(v) + ")"
	}
	return out
}

// driveFixed 来自 Win32 GetDriveType：本地固定磁盘。
// 其它取值（可移动/光驱/网络盘）这里不关心，也就不定义。
const driveFixed = 3

// listDiskRefs 枚举本机固定磁盘卷。
//
// 只保留 DRIVE_FIXED：远程盘/光驱/可移动盘要么查询慢（网络路径可能卡住），
// 要么对「服务器磁盘还剩多少」这个问题没有意义。这类机器上 C/D 盘才是要看的。
func listDiskRefs() []diskRef {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}

	refs := make([]diskRef, 0, 4)
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		rootPtr, err := syscall.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		if windows.GetDriveType(rootPtr) != driveFixed {
			continue
		}
		refs = append(refs, diskRef{
			path: root,
			// Device 用不带反斜杠的 "C:" 更贴近 Windows 用户的习惯。
			device: strings.TrimSuffix(root, `\`),
			fstype: volumeFSName(rootPtr),
		})
	}
	return refs
}

// volumeFSName 取卷的文件系统名（NTFS / ReFS），失败返回空串。
func volumeFSName(rootPtr *uint16) string {
	var (
		volName    [261]uint16
		fsName     [261]uint16
		serial     uint32
		maxCompLen uint32
		flags      uint32
	)
	err := windows.GetVolumeInformation(rootPtr,
		&volName[0], uint32(len(volName)),
		&serial, &maxCompLen, &flags,
		&fsName[0], uint32(len(fsName)))
	if err != nil {
		return ""
	}
	return syscall.UTF16ToString(fsName[:])
}

// readDiskUsage 用 GetDiskFreeSpaceEx 查每个卷的容量。
func readDiskUsage(refs []diskRef) ([]Disk, error) {
	out := make([]Disk, 0, len(refs))
	var firstErr error
	for _, r := range refs {
		rootPtr, err := syscall.UTF16PtrFromString(r.path)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		var (
			freeToCaller uint64
			total        uint64
			totalFree    uint64
		)
		if err := windows.GetDiskFreeSpaceEx(rootPtr, &freeToCaller, &total, &totalFree); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if total == 0 {
			// 空的光驱/未挂载卷会返回全 0，跳过而不是显示一条 0/0。
			continue
		}

		used := uint64(0)
		if total > totalFree {
			used = total - totalFree
		}
		out = append(out, Disk{
			Path:         r.path,
			Device:       r.device,
			FSType:       r.fstype,
			TotalBytes:   total,
			UsedBytes:    used,
			FreeBytes:    totalFree,
			UsagePercent: UsagePercent(used, total),
		})
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// hostUptimeSec 用 GetTickCount64（毫秒，系统启动至今）。
func hostUptimeSec() int64 {
	r, _, _ := procGetTickCount64.Call()
	if r == 0 {
		return 0
	}
	return int64(uint64(r) / 1000)
}
