//go:build windows

package metrics

import (
	"errors"
	"fmt"
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
	ntdll                  = windows.NewLazySystemDLL("ntdll.dll")
	procGetSystemTimes     = kernel32.NewProc("GetSystemTimes")
	procGetTickCount64     = kernel32.NewProc("GetTickCount64")
	procGetPerformanceInfo = psapi.NewProc("GetPerformanceInfo")
	// 逐核 CPU 时间只能从 ntdll 拿：Win32 层没有对应的公开接口。
	procNtQuerySystemInformation = ntdll.NewProc("NtQuerySystemInformation")
)

// systemProcessorPerformanceInformation 是 NtQuerySystemInformation 的信息类号，
// 返回每个逻辑核心的累计时间（SYSTEM_PROCESSOR_PERFORMANCE_INFORMATION 数组）。
const systemProcessorPerformanceInformation = 8

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

// readCPUTimes 取整机与每个逻辑核心的累计 CPU 时间。
//
// 整机走 kernel32 的 GetSystemTimes（稳定、无缓冲区约定），
// 逐核只能走 ntdll 的 NtQuerySystemInformation(SystemProcessorPerformanceInformation)——
// Win32 层没有提供逐核时间的公开接口。
func readCPUTimes() (cpuTimes, []cpuTimes, error) {
	total, err := readTotalCPUTimes()
	if err != nil {
		return cpuTimes{}, nil, err
	}
	// 逐核是「锦上添花」：拿不到就返回 nil，页面照常显示整机使用率，
	// 不要因为一个未文档化的 API 失败就让整页 CPU 数据消失。
	perCore, err := readPerCoreCPUTimes()
	if err != nil {
		warn("per-core cpu times unavailable", "err", err)
		perCore = nil
	}
	return total, perCore, nil
}

// readTotalCPUTimes 用 GetSystemTimes 拿整机的三个累计时间。
//
// 三个出参的顺序容易记错：idle、kernel、user。
// 而且 kernel **包含** idle，所以真正的系统态时间是 kernel - idle——
// 不减的话使用率会算高一截（在空闲机器上尤其明显）。
func readTotalCPUTimes() (cpuTimes, error) {
	var idle, kernel, user syscall.Filetime
	r, _, err := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if r == 0 {
		return cpuTimes{}, err
	}
	return combineCPUTimes(filetimeUnits(idle), filetimeUnits(kernel), filetimeUnits(user)), nil
}

// combineCPUTimes 把 Win32 的三个累计值折成统一的 cpuTimes。
// GetSystemTimes 与逐核结构体的字段含义相同，所以共用这一份换算。
func combineCPUTimes(idle, kernel, user float64) cpuTimes {
	system := kernel - idle
	if system < 0 {
		system = 0
	}
	busy := user + system
	return cpuTimes{
		user:   user,
		system: system,
		idle:   idle,
		iowait: 0, // Windows 没有 iowait 概念
		busy:   busy,
		total:  busy + idle,
	}
}

// SYSTEM_PROCESSOR_PERFORMANCE_INFORMATION 是 ntdll 的逐核结构体，
// 每个逻辑核心一项，四个字段都是 LARGE_INTEGER（100ns 单位的累计时间）。
type systemProcessorPerformanceInfo struct {
	IdleTime   int64
	KernelTime int64 // 含 IdleTime
	UserTime   int64
	DpcTime    int64
	// 后面还有 InterruptTime 与三个保留 DWORD，我们不用，但结构体大小必须写全，
	// 否则 NtQuerySystemInformation 会按元素大小步进、读到错位的垃圾。
	InterruptTime int64
	Interrupts    uint32
	_             uint32
}

// ntStatusInfoLengthMismatch 是 NtQuerySystemInformation 在缓冲区太小时的返回码，
// 即 STATUS_INFO_LENGTH_MISMATCH = 0xC0000004。此时 ReturnLength 给出所需字节数。
//
// 注意别写成 0x80000005（那是 Win32 的 ERROR_INSUFFICIENT_BUFFER）：
// NTSTATUS 与 Win32 错误码是两套编号，混用会让这里永远匹配不上，
// 逐核数据就会静默变成「拿不到」（实测踩过）。
const ntStatusInfoLengthMismatch = 0xC0000004

// readPerCoreCPUTimes 用 NtQuerySystemInformation 读每个逻辑核心的累计时间。
//
// 为什么需要两趟：缓冲区大小取决于逻辑核心数，而这个数只能在调用后从
// ReturnLength 里知道（runtime.NumCPU() 在 CPU 组/亲和性受限时可能偏小）。
// 所以第一次故意给一个 0 长度缓冲区，拿回所需大小后再正式查一次。
// 第一次返回的 error 是 ERROR_INSUFFICIENT_BUFFER，属预期路径，不算失败。
func readPerCoreCPUTimes() ([]cpuTimes, error) {
	size, err := querySystemProcessorInfo(nil)
	if err != nil && size == 0 {
		return nil, err
	}
	// 按元素大小向上取整，留一个元素的余量。
	itemSize := int(unsafe.Sizeof(systemProcessorPerformanceInfo{}))
	if itemSize == 0 {
		return nil, errors.New("unexpected zero-sized processor info struct")
	}
	count := (size + itemSize - 1) / itemSize
	if count <= 0 {
		return nil, errors.New("NtQuerySystemInformation returned no processors")
	}

	buf := make([]systemProcessorPerformanceInfo, count)
	got, err := querySystemProcessorInfo(buf)
	if err != nil {
		return nil, err
	}
	n := got / itemSize
	if n > len(buf) {
		n = len(buf)
	}

	out := make([]cpuTimes, 0, n)
	for i := 0; i < n; i++ {
		p := buf[i]
		out = append(out, combineCPUTimes(
			float64(p.IdleTime), float64(p.KernelTime), float64(p.UserTime)))
	}
	return out, nil
}

// querySystemProcessorInfo 调 NtQuerySystemInformation 查询逐核性能信息，
// 返回写入的字节数（buf 为 nil 时返回所需字节数）。
func querySystemProcessorInfo(buf []systemProcessorPerformanceInfo) (int, error) {
	var (
		ptr    uintptr
		length uint32
	)
	if len(buf) > 0 {
		ptr = uintptr(unsafe.Pointer(&buf[0]))
		length = uint32(len(buf) * int(unsafe.Sizeof(buf[0])))
	}

	var retLen uint32
	r, _, _ := procNtQuerySystemInformation.Call(
		uintptr(systemProcessorPerformanceInformation),
		ptr,
		uintptr(length),
		uintptr(unsafe.Pointer(&retLen)),
	)
	// NTSTATUS：非负为成功。
	if int32(r) >= 0 {
		return int(retLen), nil
	}
	if uint32(r) == ntStatusInfoLengthMismatch {
		// 缓冲区不足：retLen 是所需大小，交给调用方重试。
		return int(retLen), errors.New("buffer too small")
	}
	return 0, fmt.Errorf("NtQuerySystemInformation failed: 0x%08X", uint32(r))
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
