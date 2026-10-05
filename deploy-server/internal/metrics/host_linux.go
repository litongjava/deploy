//go:build linux

package metrics

import (
	"bufio"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// cpuUnitPerSec 是 /proc/stat 里 CPU 计数的单位换算：USER_HZ 固定为 100。
//
// 严格说应该用 sysconf(_SC_CLK_TCK)，但 Linux 用户态的 USER_HZ 在任何
// 架构上都是 100（内核文档 Documentation/filesystems/proc.rst），写死即可。
const cpuUnitPerSec = 100

func readHostInfo() hostInfo {
	h := hostInfo{hostname: hostnameOrEmpty(), osName: readOSName(), kernel: readKernel()}
	h.cpuModel, h.cpuCores, h.cpuPhys, h.cpuMHz = readCPUInfo()
	h.disks = listDiskRefs()
	return h
}

// readOSName 取 /etc/os-release 的 PRETTY_NAME（如 "Debian GNU/Linux 12 (bookworm)"）。
func readOSName() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "PRETTY_NAME=") {
			continue
		}
		return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"'`)
	}
	return ""
}

// readKernel 读 /proc/sys/kernel/osrelease（比 uname 系统调用省事，也不用 cgo）。
func readKernel() string {
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// readCPUInfo 解析 /proc/cpuinfo。
//
// 物理核数按「(physical id, core id) 去重」统计——这是唯一在超线程机器上
// 不被逻辑核数带偏的算法。拿不到这两个字段（ARM / 新版内核）时退化为 0。
func readCPUInfo() (model string, cores, phys, mhz int) {
	var physID, coreID string

	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return "", 0, 0, 0
	}
	defer f.Close()

	pairs := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		key, val, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)

		switch key {
		case "processor":
			cores++
		case "model name", "cpu model", "Model", "Hardware":
			// x86 是 "model name"，部分 ARM 是 "Hardware"/"cpu model"。
			if model == "" && val != "" {
				model = val
			}
		case "cpu MHz":
			// 取第一个非 0 的频率做展示；各核会有细微差异，没必要取平均。
			if mhz == 0 {
				if v, err := strconv.ParseFloat(val, 64); err == nil && v > 0 {
					mhz = int(v + 0.5)
				}
			}
		case "physical id":
			physID = val
		case "core id":
			coreID = val
		}
		if physID != "" && coreID != "" {
			pairs[physID+":"+coreID] = true
			physID, coreID = "", ""
		}
	}

	phys = len(pairs)
	return model, cores, phys, mhz
}

// readCPUTimes 读 /proc/stat：返回聚合的 cpu 行与每个逻辑核心的 cpuN 行。
//
// 逐核数据就是 /proc/stat 里 cpu0/cpu1/... 那些行，和整机行完全同构，
// 所以两边的口径天然一致（都用 parseStatCPU 解析），
// 不会出现「各核心加起来 ≠ 整机」这种自相矛盾的画面。
func readCPUTimes() (cpuTimes, []cpuTimes, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuTimes{}, nil, err
	}
	defer f.Close()

	var (
		total   cpuTimes
		hasSum  bool
		perCore []cpuTimes
	)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "cpu "):
			total = parseStatCPU(line)
			hasSum = true
		case strings.HasPrefix(line, "cpu"):
			// cpuN：顺序即核心序号，通常 0..N-1 且连续。
			perCore = append(perCore, parseStatCPU(line))
		}
	}
	if !hasSum {
		return cpuTimes{}, nil, errors.New("/proc/stat: no aggregate cpu line")
	}
	return total, perCore, nil
}

// parseStatCPU 解析一行 cpu/cpuN 统计。
//
// 字段顺序：user nice system idle iowait irq softirq steal guest guest_nice
// （后两个只在某些内核/配置下出现，缺了按 0 处理）。
func parseStatCPU(line string) cpuTimes {
	fields := strings.Fields(line)
	if len(fields) > 0 {
		fields = fields[1:] // 去掉开头的 "cpu" / "cpuN"
	}
	var v [10]float64
	for i := 0; i < len(fields) && i < len(v); i++ {
		v[i], _ = strconv.ParseFloat(fields[i], 64)
	}
	user := v[0] + v[1]                   // user + nice
	system := v[2] + v[5] + v[6]          // system + irq + softirq
	idle := v[3]                          //
	iowait := v[4]                        //
	busy := user + system + iowait + v[7] // v[7]=steal 也算 busy
	// guest/guest_nice(v[8]/v[9]) 已包含在 user/nice 里，不重复加。
	return cpuTimes{
		user: user, system: system, idle: idle, iowait: iowait,
		busy: busy, total: busy + idle,
	}
}

// readLoad 读 /proc/loadavg。
func readLoad() Load {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return Load{}
	}
	f := strings.Fields(string(b))
	if len(f) < 3 {
		return Load{}
	}
	a1, err1 := strconv.ParseFloat(f[0], 64)
	a5, err5 := strconv.ParseFloat(f[1], 64)
	a15, err15 := strconv.ParseFloat(f[2], 64)
	if err1 != nil || err5 != nil || err15 != nil {
		return Load{}
	}
	return Load{Available: true, Avg1: a1, Avg5: a5, Avg15: a15}
}

// readMem 解析 /proc/meminfo。
//
// 「已用内存」用 MemTotal - MemAvailable，而不是 MemTotal - MemFree：
// MemFree 不含可回收的 page cache，在正常运行的机器上会让使用率虚高到 90%+，
// 是各种监控面板最常见的误报来源。MemAvailable 是内核估算的「还能给应用多少」。
func readMem() (Memory, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return Memory{}, err
	}
	defer f.Close()

	kv := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, val, found := strings.Cut(sc.Text(), ":")
		if !found {
			continue
		}
		fields := strings.Fields(val)
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		kv[strings.TrimSpace(key)] = n * 1024 // 单位是 kB
	}

	total := kv["MemTotal"]
	avail := kv["MemAvailable"]
	if avail == 0 {
		// 老内核（< 3.14）没有 MemAvailable，退化为 MemFree。
		avail = kv["MemFree"]
	}
	used := uint64(0)
	if total > avail {
		used = total - avail
	}

	swapTotal := kv["SwapTotal"]
	swapFree := kv["SwapFree"]
	swapUsed := uint64(0)
	if swapTotal > swapFree {
		swapUsed = swapTotal - swapFree
	}

	return Memory{
		TotalBytes:     total,
		UsedBytes:      used,
		FreeBytes:      kv["MemFree"],
		AvailableBytes: avail,
		UsagePercent:   UsagePercent(used, total),
		SwapTotalBytes: swapTotal,
		SwapUsedBytes:  swapUsed,
		SwapFreeBytes:  swapFree,
		SwapKind:       "swap",
		SwapLabel:      "交换分区",
	}, nil
}

// pseudoFS 是不该出现在「磁盘容量」里的伪文件系统。
//
// 不过滤的话页面上会出现几十个 0 字节的 cgroup/tmpfs 条目，
// 真正要看的 / 反而被淹掉。cgroup2fs / tracefs 这些是内核内部类型，
// 一并挡掉。
var pseudoFS = map[string]bool{
	"proc": true, "sysfs": true, "devtmpfs": true, "devpts": true, "tmpfs": true,
	"cgroup": true, "cgroup2": true, "cgroup2fs": true, "pstore": true,
	"securityfs": true, "debugfs": true, "tracefs": true, "fusectl": true,
	"mqueue": true, "hugetlbfs": true, "configfs": true, "bpf": true,
	"binfmt_misc": true, "autofs": true, "efivarfs": true, "ramfs": true,
	"rpc_pipefs": true, "nsfs": true, "squashfs": true, "overlay": true,
	"fuse.gvfsd-fuse": true, "fuse.portal": true, "selinuxfs": true,
}

// listDiskRefs 从 /proc/mounts 里挑出真实的块设备挂载点。
//
// 只看设备名以 /dev/ 开头的行：
//   - 天然排除了 tmpfs/cgroup/proc 等伪文件系统；
//   - 也排除了 overlay（docker 容器的根是 overlay，没有 /dev/ 前缀）——
//     容器里本来就该看宿主机的数据，看不出个所以然不如不显示。
//
// 注意这是解析 /proc/mounts 而不是调 getmntent，也不涉及 cgo：
// 后者要在 CGO_ENABLED=0 的交叉编译里用，成本和收益不成比例。
func listDiskRefs() []diskRef {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil
	}
	defer f.Close()

	seen := map[string]bool{}
	refs := make([]diskRef, 0, 8)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		device := unescapeMount(fields[0])
		mount := unescapeMount(fields[1])
		fstype := fields[2]

		if !strings.HasPrefix(device, "/dev/") {
			continue
		}
		if pseudoFS[fstype] {
			continue
		}
		// 同一个挂载点可能因为 bind mount 出现多次。
		if seen[mount] {
			continue
		}
		seen[mount] = true
		refs = append(refs, diskRef{path: mount, device: device, fstype: fstype})
	}

	// 根分区排最前，其余按挂载点字母序，保证多次刷新顺序稳定。
	sortRefs(refs)
	return refs
}

// unescapeMount 还原 /proc/mounts 里的八进制转义（空格是 \040，制表符是 \011）。
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// sortRefs 让 "/" 永远排第一，其余按路径排序（保证多次刷新顺序稳定）。
func sortRefs(refs []diskRef) {
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].path == "/" {
			return true
		}
		if refs[j].path == "/" {
			return false
		}
		return refs[i].path < refs[j].path
	})
}

// readDiskUsage 对每个挂载点调 statfs 拿容量。
func readDiskUsage(refs []diskRef) ([]Disk, error) {
	out := make([]Disk, 0, len(refs))
	var firstErr error
	for _, r := range refs {
		var st syscall.Statfs_t
		if err := syscall.Statfs(r.path, &st); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		// Bavail 是「非特权用户可用」，Bfree 含 root 保留块。
		// 展示总容量时用 Bfree 算已用，和 df 的口径一致（df 默认不含 reserved）。
		bsize := uint64(st.Bsize)
		total := st.Blocks * bsize
		free := st.Bfree * bsize
		used := uint64(0)
		if total > free {
			used = total - free
		}

		out = append(out, Disk{
			Path:         r.path,
			Device:       r.device,
			FSType:       r.fstype,
			TotalBytes:   total,
			UsedBytes:    used,
			FreeBytes:    st.Bavail * bsize,
			UsagePercent: UsagePercent(used, total),
		})
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// hostUptimeSec 读 /proc/uptime。
func hostUptimeSec() int64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	sec, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return int64(sec)
}
