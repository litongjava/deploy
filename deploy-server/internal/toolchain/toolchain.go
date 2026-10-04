// Package toolchain 提供「环境工具链一键安装」。
//
// 安装脚本内嵌在二进制里（go:embed），因为交付物就是一个二进制：服务器上
// 没有仓库、没有 Node，脚本必须跟着二进制走。平台在触发安装时把它落到
// <dataDir>/toolchain/install.sh，再交给执行引擎当普通 Run 跑 —— 这样日志、
// 取消、重跑、下载全部复用既有机制，不需要为安装再造一套进度上报。
package toolchain

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed install.sh
var files embed.FS

// ScriptName 是脚本在数据目录下的文件名。
const ScriptName = "install.sh"

// Script 返回安装脚本内容（供页面下载 / 手动执行）。
func Script() ([]byte, error) { return files.ReadFile(ScriptName) }

// ScriptPath 返回脚本在数据目录下的落盘路径。
func ScriptPath(dataDir string) string {
	return filepath.Join(dataDir, "toolchain", ScriptName)
}

// EnsureScript 把脚本写到数据目录；每次覆盖，保证脚本与二进制同版本。
func EnsureScript(dataDir string) (string, error) {
	body, err := Script()
	if err != nil {
		return "", err
	}
	p := ScriptPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return "", fmt.Errorf("failed to create the toolchain directory: %w", err)
	}
	if err := os.WriteFile(p, body, 0o755); err != nil {
		return "", fmt.Errorf("failed to write the installer: %w", err)
	}
	return p, nil
}

// Probe 是一组件「有没有装」的判据。
//
//   - Args 非空：用 exec.LookPath 找这个命令，并执行 Args 取版本
//   - Paths 非空：任一文件/目录存在即视为已安装（nvm 这类没有可执行文件的）
type Probe struct {
	Name  string
	Args  []string
	Paths []string
}

// Component 是页面上可勾选的一项，Key 必须与 install.sh 的 ALL_COMPONENTS 一致。
type Component struct {
	Key      string
	Name     string
	Note     string
	Estimate string
	Probes   []Probe
}

// Components 是可安装的全部组件。
//
// 顺序即页面展示顺序，也是「一键安装缺失」的执行顺序（先基础工具链，再语言运行时）。
var Components = []Component{
	{
		Key: "cc", Name: "C 工具链（gcc / g++ / make）",
		Note:     "编译原生扩展、cargo 链接、make 构建都要它",
		Estimate: "约 200 MB",
		Probes: []Probe{
			{Name: "gcc", Args: []string{"-dumpversion"}},
			{Name: "make", Args: []string{"-v"}},
		},
	},
	{
		Key: "java", Name: "Java（OpenJDK）",
		Note:     "Java / Maven 项目构建；默认装 JDK 17，可改版本",
		Estimate: "约 180 MB",
		Probes:   []Probe{{Name: "java", Args: []string{"-version"}}},
	},
	{
		Key: "maven", Name: "Maven",
		Note:     "mvn 构建；顺带写一份阿里云镜像 settings.xml（已有配置则不覆盖）",
		Estimate: "约 30 MB",
		Probes:   []Probe{{Name: "mvn", Args: []string{"-v"}}},
	},
	{
		Key: "nvm", Name: "nvm（Node 版本管理）",
		Note:     "装到 /usr/local/nvm，并写 /etc/profile.d/nvm.sh",
		Estimate: "约 5 MB",
		Probes:   []Probe{{Name: "nvm", Paths: []string{"/usr/local/nvm/nvm.sh"}}},
	},
	{
		Key: "node", Name: "Node.js（经 nvm）",
		Note:     "默认装 LTS；用 nvm 管理，仅 login shell（bash -lc）可见",
		Estimate: "约 60 MB",
		Probes:   []Probe{{Name: "node", Args: []string{"--version"}}},
	},
	{
		Key: "pnpm", Name: "pnpm",
		Note:     "经 corepack 装到 /usr/local/bin，任何 shell 都可用；registry 指向 npmmirror",
		Estimate: "约 10 MB",
		Probes:   []Probe{{Name: "pnpm", Args: []string{"--version"}}},
	},
	{
		Key: "go", Name: "Go",
		Note:     "装到 /usr/local/go 并软链到 /usr/local/bin；GOPROXY 指向 goproxy.cn",
		Estimate: "约 120 MB",
		Probes:   []Probe{{Name: "go", Args: []string{"version"}}},
	},
	{
		Key: "rust", Name: "Rust（rustup）",
		Note:     "系统级 rustup（/usr/local/cargo），crates 走 rsproxy 镜像",
		Estimate: "约 250 MB",
		Probes:   []Probe{{Name: "rustc", Args: []string{"--version"}}},
	},
}

// AllKeys 返回全部组件 key。
func AllKeys() []string {
	out := make([]string, 0, len(Components))
	for _, c := range Components {
		out = append(out, c.Key)
	}
	return out
}

// Lookup 按 key 找组件。
func Lookup(key string) (Component, bool) {
	for _, c := range Components {
		if c.Key == key {
			return c, true
		}
	}
	return Component{}, false
}
