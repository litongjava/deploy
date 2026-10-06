// Package middleware 提供「中间件一键安装」：docker / postgresql / mysql / redis / elasticsearch。
//
// 与 internal/toolchain 是**两套东西**，刻意不合并：
//   - toolchain 装的是构建工具链（gcc/java/maven/node/go/rust/certbot），无状态、不占端口、装完即用；
//   - 这里的每一件都是**有状态服务**：有端口、有密码、有数据目录，装完还要能启停查、还得保证
//     重复点「安装」不会重建容器把数据冲掉。
//
// 把两类混在一个脚本里，会让「一键安装缺失」变得危险：构建工具链重装无害，
// 数据库重装可能换掉数据卷指向。所以这里单独一套组件表、单独一个内嵌脚本、单独一组接口。
//
// 安装脚本同样被 go:embed 进二进制，触发安装时落盘到 <dataDir>/middleware/install.sh，
// 再交给执行引擎当普通 Run 跑 —— 实时日志、取消、重跑、下载、并发排队全部复用。
package middleware

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
	return filepath.Join(dataDir, "middleware", ScriptName)
}

// EnsureScript 把脚本写到数据目录；每次覆盖，保证脚本与二进制同版本。
func EnsureScript(dataDir string) (string, error) {
	body, err := Script()
	if err != nil {
		return "", err
	}
	p := ScriptPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return "", fmt.Errorf("failed to create the middleware directory: %w", err)
	}
	if err := os.WriteFile(p, body, 0o755); err != nil {
		return "", fmt.Errorf("failed to write the middleware installer: %w", err)
	}
	return p, nil
}

// 安装方式。
const (
	// ModeDocker 用容器跑（默认）：隔离、卸载干净、跨发行版一致。
	ModeDocker = "docker"
	// ModeNative 用发行版包管理器直装：省内存、不依赖 docker，但版本受发行版限制。
	ModeNative = "native"
)

// 可用动作。install 是默认，其余是「已经装好之后」的运维动作。
const (
	ActionInstall = "install"
	ActionStart   = "start"
	ActionStop    = "stop"
	ActionRestart = "restart"
	ActionRemove  = "remove"
	ActionUpgrade = "upgrade"
	ActionLogs    = "logs"
)

// Actions 是全部动作（按「无害 → 危险」排序，页面上照这个顺序摆按钮）。
var Actions = []string{
	ActionInstall, ActionStart, ActionStop, ActionRestart, ActionUpgrade, ActionLogs, ActionRemove,
}

// IsAction 校验动作名。
func IsAction(a string) bool {
	for _, x := range Actions {
		if x == a {
			return true
		}
	}
	return false
}

// Probe 是页面上展示的「探测项」，只描述**怎么显示**。
//
// 真正的判定逻辑在服务端 handlers_middleware.go：它要区分「容器方式装了」「原生方式装了」
// 「装了但没在跑」三种情况，还要读容器状态 —— 那不是一次 LookPath 能表达的，
// 所以这里刻意不做成通用探针，只保留一行可读的名字。
type Probe struct {
	Name string
}

// Field 是一个可编辑参数。
//
// Key 直接是脚本读的**环境变量名**（PG_PORT、REDIS_PASSWORD……），保存后安装时注入环境，
// 所以「编辑参数」不需要给脚本设计新配置格式，也不会让值经过命令行拼接。
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Default     string `json:"default"`
	Placeholder string `json:"placeholder"`
	Help        string `json:"help"`
	// Secret 标记密码类参数：读取接口只回「是否已设置」，绝不回明文（见 handlers_middleware.go）。
	Secret bool `json:"secret"`
}

// Component 是页面上可勾选的一项；Key 必须与 install.sh 的 ALL_COMPONENTS 一致。
type Component struct {
	Key         string
	Name        string
	Note        string
	Category    string // runtime | database | cache | search
	Estimate    string
	Modes       []string
	DefaultMode string
	// NativeSupported=false 表示只支持容器方式（elasticsearch）。
	// 明确标出来，页面就把它置灰，而不是让使用者点了才失败。
	NativeSupported bool
	Probes          []Probe
	Fields          []Field
}

// Components 是可安装的全部中间件。
//
// 顺序即页面展示顺序，也是「一键安装缺失」的执行顺序：docker 必须排第一，
// 因为其余四件默认都跑在它的容器里。
var Components = []Component{
	{
		Key: "docker", Name: "Docker（容器运行时）",
		Note:     "其余四项默认都以容器方式跑，所以它是前置；它自己只支持原生方式安装",
		Category: "runtime",
		Estimate: "约 400 MB",
		Modes:    []string{ModeNative}, DefaultMode: ModeNative, NativeSupported: true,
		Probes: []Probe{{Name: "docker --version"}},
		Fields: []Field{},
	},
	{
		Key: "postgresql", Name: "PostgreSQL",
		Note:     "关系型数据库；默认容器方式，数据卷持久化，密码留空则随机生成",
		Category: "database",
		Estimate: "约 150 MB（镜像）",
		Modes:    []string{ModeDocker, ModeNative}, DefaultMode: ModeDocker, NativeSupported: true,
		Probes: []Probe{{Name: "容器 " + ContainerName("postgresql")}, {Name: "psql"}},
		Fields: []Field{
			{Key: "PG_VERSION", Label: "版本", Default: "16", Placeholder: "16",
				Help: "容器方式即镜像 tag（postgres:16）；原生方式在 Debian/Ubuntu 上作为 postgresql-<版本> 包名。"},
			{Key: "PG_PORT", Label: "端口", Default: "5432", Placeholder: "5432"},
			{Key: "PG_USER", Label: "超级用户名", Default: "postgres", Placeholder: "postgres",
				Help: "仅容器方式生效（POSTGRES_USER）。"},
			{Key: "PG_PASSWORD", Label: "密码", Default: "", Placeholder: "留空则自动生成随机密码",
				Help: "留空时脚本生成 32 位随机密码并打印在运行日志末尾。密码会存进容器环境变量，可用 docker inspect 回看。", Secret: true},
		},
	},
	{
		Key: "mysql", Name: "MySQL",
		Note:     "关系型数据库；默认容器方式，数据卷持久化，root 密码留空则随机生成",
		Category: "database",
		Estimate: "约 600 MB（镜像）",
		Modes:    []string{ModeDocker, ModeNative}, DefaultMode: ModeDocker, NativeSupported: true,
		Probes: []Probe{{Name: "容器 " + ContainerName("mysql")}, {Name: "mysql"}},
		Fields: []Field{
			{Key: "MYSQL_VERSION", Label: "版本", Default: "8.0", Placeholder: "8.0 / 8.4",
				Help: "容器方式即镜像 tag（mysql:8.0）。"},
			{Key: "MYSQL_PORT", Label: "端口", Default: "3306", Placeholder: "3306"},
			{Key: "MYSQL_ROOT_PASSWORD", Label: "root 密码", Default: "", Placeholder: "留空则自动生成随机密码",
				Help: "留空时脚本生成 32 位随机密码并打印在运行日志末尾。", Secret: true},
		},
	},
	{
		Key: "redis", Name: "Redis",
		Note:     "缓存 / 队列；默认容器方式并开启 AOF 持久化",
		Category: "cache",
		Estimate: "约 40 MB（镜像）",
		Modes:    []string{ModeDocker, ModeNative}, DefaultMode: ModeDocker, NativeSupported: true,
		Probes: []Probe{{Name: "容器 " + ContainerName("redis")}, {Name: "redis-server"}},
		Fields: []Field{
			// 官方文档的取镜像命令就是 `docker run --name some-redis -d redis`，
			// 也就是 latest；用 latest 当默认而不是 7，才不会出现
			// 「本地明明有 redis:latest、脚本却非要去拉 redis:7」这种在本机
			// 镜像加速不可用时直接卡死的局面（拉取失败时容器就跑不起来，
			// 而部署机上往往早就有一份能用的镜像）。
			{Key: "REDIS_VERSION", Label: "版本", Default: "latest", Placeholder: "latest / 7 / 7.4",
				Help: "容器方式即镜像 tag（redis:latest）。留 latest 时，若本机已有该 tag 的镜像，docker 会直接用本地的。"},
			{Key: "REDIS_PORT", Label: "端口", Default: "6379", Placeholder: "6379"},
			{Key: "REDIS_PASSWORD", Label: "密码", Default: "", Placeholder: "留空则不设密码",
				Help: "**留空等于无密码**：内网自用可接受，公网暴露前必须设置。原生方式会写入 requirepass。", Secret: true},
			{Key: "REDIS_DATA_DIR", Label: "宿主机数据目录（仅备忘）", Default: "/srv/deploy/middleware/redis",
				Placeholder: "/srv/deploy/middleware/redis",
				Help:        "容器方式下数据实际存在 docker 卷里；这里只用于报告与运维备忘。"},
		},
	},
	{
		Key: "elasticsearch", Name: "Elasticsearch",
		Note:     "搜索引擎；单节点 + 关闭安全（内网自用），只支持容器方式",
		Category: "search",
		Estimate: "约 1.4 GB（镜像 + 数据）",
		Modes:    []string{ModeDocker}, DefaultMode: ModeDocker, NativeSupported: false,
		Probes: []Probe{{Name: "容器 " + ContainerName("elasticsearch")}},
		Fields: []Field{
			{Key: "ES_VERSION", Label: "版本", Default: "8.15.0", Placeholder: "8.15.0"},
			{Key: "ES_PORT", Label: "HTTP 端口", Default: "9200", Placeholder: "9200"},
			{Key: "ES_TRANSPORT_PORT", Label: "节点间端口", Default: "9300", Placeholder: "9300"},
			{Key: "ES_JAVA_OPTS", Label: "JVM 堆", Default: "-Xms1g -Xmx1g", Placeholder: "-Xms2g -Xmx2g",
				Help: "默认 1G 堆；生产建议按物理内存一半设置，且不超过 31g。"},
			{Key: "ES_DATA_DIR", Label: "宿主机数据目录（仅备忘）", Default: "/srv/deploy/middleware/elasticsearch",
				Placeholder: "/srv/deploy/middleware/elasticsearch",
				Help:        "容器方式下数据实际存在 docker 卷里；这里只用于报告与运维备忘。"},
		},
	},
}

// AllKeys 返回全部中间件 key。
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

// 容器与数据卷的名字前缀。必须与 install.sh 里的 CONTAINER_PREFIX / VOLUME_PREFIX 默认值一致，
// 否则平台报告的容器名和脚本实际操作的不是同一个 —— 这是很隐蔽的一类 bug，有测试盯着。
const (
	ContainerPrefix = "deploy-mw-"
	VolumePrefix    = "deploy-mw-"
)

// ContainerName 返回容器方式下的容器名。
func ContainerName(key string) string { return ContainerPrefix + key }

// VolumeName 返回容器方式下的数据卷名。
func VolumeName(key string) string { return VolumePrefix + key + "-data" }
