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
//   - Args 非空：执行环境（Unix 下登录 shell，见 runner.LoginEnv）的 PATH 里找这个命令，
//     再执行 Args 取版本
//   - Paths 非空：任一文件/目录存在即视为已安装（nvm 这类没有可执行文件的）；
//     路径里可以用 $VAR 与开头的 ~，按执行环境展开
type Probe struct {
	Name  string
	Args  []string
	Paths []string
}

// Field 是组件的一个可编辑参数。
//
// Key 直接就是安装脚本读取的**环境变量名**（例如 JAVA_VERSION、JDK_URL、JAVA_DIR）：
// 页面保存后，安装时这些值会作为运行参数注入环境，脚本按 `${VAR:-默认值}` 取值。
// 这样「编辑参数」不需要给脚本设计一套新的配置格式，也不会让值经过命令行拼接。
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Default     string `json:"default"`
	Placeholder string `json:"placeholder"`
	Help        string `json:"help"`
}

// Component 是页面上可勾选的一项，Key 必须与 install.sh 的 ALL_COMPONENTS 一致。
type Component struct {
	Key      string
	Name     string
	Note     string
	Estimate string
	Probes   []Probe
	Fields   []Field
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
		Fields: []Field{
			{Key: "CC_EXTRA_PACKAGES", Label: "额外软件包（可选）", Default: "",
				Placeholder: "cmake nasm libssl-dev",
				Help:        "空格分隔，交给 apt/dnf 与基础工具链一起装；编译某些原生依赖时会用到（例如 cmake、nasm）。"},
		},
	},
	{
		Key: "java", Name: "Java（OpenJDK）",
		Note:     "Java / Maven 项目构建；默认装 JDK 17，可改版本或改用压缩包安装",
		Estimate: "约 180 MB",
		Probes:   []Probe{{Name: "java", Args: []string{"-version"}}},
		Fields: []Field{
			{Key: "JAVA_VERSION", Label: "JDK 大版本（apt 安装）", Default: "17",
				Placeholder: "17", Help: "用发行版包管理器装哪个大版本（Debian 12 / Ubuntu 24.04 一般有 17）。填 8/11/21 时若源里没有会提示可选版本。"},
			{Key: "JDK_URL", Label: "JDK 压缩包地址（可选，优先于 apt）", Default: "",
				Placeholder: "https://.../jdk-8uXXX-linux-x64.tar.gz",
				Help:        "填了就改用 tar.gz 安装（适合 JDK 8 这类 apt 里没有的版本），不再走 apt；SHA/大小不做校验，请自行确认来源可信。"},
			{Key: "JAVA_DIR", Label: "JDK 安装目录（压缩包方式）", Default: "/usr/local/jdk",
				Placeholder: "/usr/local/jdk8", Help: "解压到这里，并作为 JAVA_HOME 写入 /etc/profile.d/java.sh。"},
		},
	},
	{
		Key: "maven", Name: "Maven",
		Note:     "mvn 构建；顺带写一份阿里云镜像 settings.xml（已有配置则不覆盖）",
		Estimate: "约 30 MB",
		Probes:   []Probe{{Name: "mvn", Args: []string{"-v"}}},
		Fields: []Field{
			{Key: "MAVEN_URL", Label: "Maven 压缩包地址（可选，优先于 apt）", Default: "",
				Placeholder: "https://.../apache-maven-3.9.9-bin.tar.gz",
				Help:        "填了就跳过 apt，直接解压安装。"},
			{Key: "MAVEN_DIR", Label: "Maven 安装目录（压缩包方式）", Default: "/usr/local/maven",
				Placeholder: "/usr/local/maven", Help: "解压到这里，并把 mvn 软链到 /usr/local/bin。"},
		},
	},
	{
		Key: "nvm", Name: "nvm（Node 版本管理）",
		Note:     "装到 /usr/local/nvm，并写 /etc/profile.d/nvm.sh",
		Estimate: "约 5 MB",
		// nvm 没有可执行文件，只能按 nvm.sh 的存在判断；除本平台的默认目录外，
		// 还要认「用户自己按官方脚本装的 ~/.nvm」——否则会出现 node 明明在用、
		// 面板却说 nvm 缺失，点「一键安装缺失」就会多装一套 nvm 并改写 profile。
		Probes: []Probe{{Name: "nvm", Paths: []string{
			"/usr/local/nvm/nvm.sh",
			"$NVM_DIR/nvm.sh",
			"$HOME/.nvm/nvm.sh",
		}}},
		Fields: []Field{
			{Key: "NVM_VERSION", Label: "nvm 版本（git tag）", Default: "v0.40.1", Placeholder: "v0.40.1"},
			{Key: "NVM_GIT_URL", Label: "nvm 仓库地址", Default: "https://github.com/nvm-sh/nvm.git",
				Placeholder: "https://gitee.com/.../nvm.git", Help: "克隆不动 github 时换成可达镜像。"},
			{Key: "NVM_DIR", Label: "nvm 安装目录", Default: "/usr/local/nvm", Placeholder: "/usr/local/nvm"},
		},
	},
	{
		Key: "node", Name: "Node.js（经 nvm）",
		Note:     "默认装 LTS；用 nvm 管理，仅 login shell（bash -lc）可见",
		Estimate: "约 60 MB",
		Probes:   []Probe{{Name: "node", Args: []string{"--version"}}},
		Fields: []Field{
			{Key: "NODE_VERSION", Label: "Node 版本", Default: "--lts", Placeholder: "--lts / 20 / v20.11.1",
				Help: "交给 nvm install，写成 --lts 就是最新 LTS。"},
			{Key: "NVM_NODEJS_ORG_MIRROR", Label: "Node 下载镜像", Default: "https://npmmirror.com/mirrors/node",
				Placeholder: "https://npmmirror.com/mirrors/node", Help: "nvm 下载 node 二进制的镜像。"},
		},
	},
	{
		Key: "pnpm", Name: "pnpm",
		Note:     "经 corepack 装到 /usr/local/bin，任何 shell 都可用；registry 指向 npmmirror",
		Estimate: "约 10 MB",
		Probes:   []Probe{{Name: "pnpm", Args: []string{"--version"}}},
		Fields: []Field{
			{Key: "PNPM_VERSION", Label: "pnpm 版本", Default: "latest", Placeholder: "latest / 9.12.0",
				Help: "corepack prepare pnpm@<版本>；失败时退回 npm i -g。"},
		},
	},
	{
		Key: "go", Name: "Go",
		Note:     "装到 /usr/local/go 并软链到 /usr/local/bin；GOPROXY 指向 goproxy.cn",
		Estimate: "约 120 MB",
		Probes:   []Probe{{Name: "go", Args: []string{"version"}}},
		Fields: []Field{
			{Key: "GO_VERSION", Label: "Go 版本", Default: "1.24.2", Placeholder: "1.24.2"},
			{Key: "GO_URL", Label: "Go 压缩包地址（可选）", Default: "",
				Placeholder: "https://.../go1.24.2.linux-amd64.tar.gz",
				Help:        "留空则依次尝试阿里云镜像、golang.google.cn、官方源。"},
			{Key: "GO_DIR", Label: "Go 安装目录", Default: "/usr/local/go", Placeholder: "/usr/local/go"},
		},
	},
	{
		Key: "rust", Name: "Rust（rustup）",
		Note:     "系统级 rustup（/usr/local/cargo），crates 走 rsproxy 镜像",
		Estimate: "约 250 MB",
		Probes:   []Probe{{Name: "rustc", Args: []string{"--version"}}},
		Fields: []Field{
			{Key: "RUST_TOOLCHAIN", Label: "toolchain", Default: "stable", Placeholder: "stable / 1.83.0"},
			{Key: "RUSTUP_HOME", Label: "RUSTUP_HOME", Default: "/usr/local/rustup", Placeholder: "/usr/local/rustup"},
			{Key: "CARGO_HOME", Label: "CARGO_HOME", Default: "/usr/local/cargo", Placeholder: "/usr/local/cargo"},
			{Key: "RUSTUP_DIST_SERVER", Label: "rustup 镜像", Default: "https://rsproxy.cn",
				Placeholder: "https://rsproxy.cn", Help: "国内加速；留空走官方。"},
		},
	},
	{
		// 唯一一个不是「构建工具链」的组件：环境自检一直在报 certbot 缺失
		// （handlers_system.go 的 toolProbes），而它以前只能按提示手动装，
		// 自检面板却写着「可以在下面一键装上」——补上这一项，两处口径才一致。
		Key: "certbot", Name: "certbot（HTTPS 证书）",
		Note:     "Let's Encrypt 的 ACME 客户端；只装工具，签发仍要求域名解析到本机（HTTP-01）",
		Estimate: "约 30 MB",
		Probes:   []Probe{{Name: "certbot", Args: []string{"--version"}}},
		Fields: []Field{
			{Key: "CERTBOT_NGINX_PLUGIN", Label: "是否装 nginx 插件", Default: "1",
				Placeholder: "1 / 0",
				Help:        "装发行版的 python3-certbot-nginx：certbot --nginx 才能自动改 vhost 加 HTTPS 段。填 0 只装客户端本体。"},
			{Key: "CERTBOT_EXTRA_PACKAGES", Label: "额外软件包（可选）", Default: "",
				Placeholder: "python3-certbot-dns-cloudflare",
				Help:        "空格分隔，交给 apt/dnf 与本体一起装；用 DNS 挑战或其它验证方式时要装对应插件包。"},
		},
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
