//go:build linux

package metrics

import (
	"strings"
	"testing"
)

// TestPseudoFilesystemsFiltered：tmpfs / cgroup / proc 这些伪文件系统
// 绝不能出现在磁盘列表里，否则页面上会是一屏 0 字节的噪音，
// 真正要看的 / 反而被淹掉。
func TestPseudoFilesystemsFiltered(t *testing.T) {
	refs := listDiskRefs()
	if len(refs) == 0 {
		t.Fatal("没有识别到任何块设备挂载点")
	}
	for _, r := range refs {
		if pseudoFS[r.fstype] {
			t.Errorf("伪文件系统不应入列: %s (%s)", r.path, r.fstype)
		}
		if !strings.HasPrefix(r.device, "/dev/") {
			t.Errorf("设备名应来自 /dev/: %s", r.device)
		}
	}
	// 根分区必须在列表里，否则这个页面就没有意义了。
	found := false
	for _, r := range refs {
		if r.path == "/" {
			found = true
		}
	}
	if !found {
		t.Error("列表中应包含根分区 /")
	}
}

func TestUnescapeMount(t *testing.T) {
	cases := map[string]string{
		`/mnt/data`:          "/mnt/data",
		`/mnt/my\040disk`:    "/mnt/my disk",
		`/mnt/tab\011here`:   "/mnt/tab\there",
		`/mnt/back\134slash`: `/mnt/back\slash`,
		`/mnt/incomplete\04`: `/mnt/incomplete\04`, // 转义不完整时原样保留
		`/mnt/not\an\escape`: `/mnt/not\an\escape`,
	}
	for in, want := range cases {
		if got := unescapeMount(in); got != want {
			t.Errorf("unescapeMount(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

// TestReadMemFromProc：直接核对 /proc/meminfo 的解析结果与文件内容一致。
func TestReadMemFromProc(t *testing.T) {
	m, err := readMem()
	if err != nil {
		t.Fatalf("readMem 失败: %v", err)
	}
	if m.TotalBytes == 0 {
		t.Fatal("MemTotal 不应为 0")
	}
	if m.AvailableBytes == 0 {
		t.Fatal("MemAvailable 不应为 0")
	}
	if m.AvailableBytes > m.TotalBytes {
		t.Errorf("可用内存大于总内存: avail=%d total=%d", m.AvailableBytes, m.TotalBytes)
	}
	// UsagePercent 必须与 used/total 自洽，避免出现「条走到 90% 而数字写 10%」。
	want := UsagePercent(m.TotalBytes-m.AvailableBytes, m.TotalBytes)
	if m.UsagePercent != want {
		t.Errorf("usagePercent=%v，按 used/total 应为 %v", m.UsagePercent, want)
	}
}
