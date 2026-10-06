package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"deploy-server/internal/middleware"
	"deploy-server/internal/runner"
	"deploy-server/internal/store"
)

// 中间件一键安装（页面：侧边栏「环境自检」→ 中间件面板）。
//
// 与工具链安装（handlers_toolchain.go）是同一套套路：脚本嵌入二进制后落盘，
// 再当作**一个普通 Run** 跑，于是实时日志、取消、重跑、下载全部复用。
//
// 三处刻意的差异：
//  1. 组件表多两个维度 —— 安装方式（docker/native）与动作（安装/启停/移除），
//     因为有状态服务不像编译器那样「装上就完事」。
//  2. **密码参数加密落库**：PG_PASSWORD 这类参数走 secret.Box，读取接口只回
//     「已设置 / 未设置」。参见 middlewareConfig 的注释。
//  3. 探测结果带 5 秒缓存：探测 docker 状态要 fork/exec 一次 docker CLI（几十毫秒），
//     而面板会反复刷新，连点「重新检测」不该把服务器 fork 到卡住。
const (
	middlewareProjectKey = "system-middleware"
	middlewareProbeTTL   = 5 * time.Second
)

type middlewareProbeDTO struct {
	Name  string `json:"name"`
	Found bool   `json:"found"`
}

type middlewareFieldDTO struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Default     string `json:"default"`
	Placeholder string `json:"placeholder"`
	Help        string `json:"help"`
	Secret      bool   `json:"secret"`
	// Value 是保存过的覆盖值；空串表示「用默认」。
	// Secret 为 true 时**永远**是空串，改用 Set 表示是否已配置。
	Value string `json:"value"`
	// Set 仅对 Secret 参数有意义：true = 已保存过密码。
	Set bool `json:"set"`
}

type middlewareComponentDTO struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Note      string `json:"note"`
	Category  string `json:"category"`
	Estimate  string `json:"estimate"`
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Version   string `json:"version"`
	Path      string `json:"path"`
	Detail    string `json:"detail"`
	// Modes 是该组件可选的方式；Mode 是当前保存的配置；EffectiveMode 是实际在跑的方式。
	Modes           []string             `json:"modes"`
	DefaultMode     string               `json:"defaultMode"`
	Mode            string               `json:"mode"`
	EffectiveMode   string               `json:"effectiveMode"`
	NativeSupported bool                 `json:"nativeSupported"`
	Probes          []middlewareProbeDTO `json:"probes"`
	Fields          []middlewareFieldDTO `json:"fields"`
}

// ---------- 配置存取（settings 表，密码用 secret.Box 加密） ----------

// middlewareConfig 是一个中间件在 settings 表里的持久化形态。
//
// 为什么不像 toolchain 那样把参数直接塞进一个 JSON map：
// 密码类参数（PG_PASSWORD、MYSQL_ROOT_PASSWORD、REDIS_PASSWORD）如果明文落库，
// 就和平台「Secret 环境变量必须加密」的口径不一致 —— 备份一份库文件即泄露数据库口令。
// 所以 Values 存明文普通参数、Secrets 存 AES-GCM 密文，读取接口只回是否已设置。
type middlewareConfig struct {
	Mode    string            `json:"mode,omitempty"`
	Values  map[string]string `json:"values,omitempty"`
	Secrets map[string]string `json:"secrets,omitempty"` // 参数名 → 密文
}

func middlewareSettingKey(component string) string { return "middleware." + component }

func (s *Server) loadMiddlewareConfig(component string) middlewareConfig {
	cfg := middlewareConfig{}
	raw, ok := s.st.GetSetting(middlewareSettingKey(component))
	if !ok || strings.TrimSpace(raw) == "" {
		return cfg
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		// 配置坏了就回默认，而不是让整个面板 500：使用者还能重新保存一次修好它。
		return middlewareConfig{}
	}
	return cfg
}

func (s *Server) saveMiddlewareConfig(component string, cfg middlewareConfig) error {
	body, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return s.st.SetSetting(middlewareSettingKey(component), string(body))
}

// middlewareEffectiveMode 解析「这个组件该用哪种方式」。
//
// 优先级：调用方本次指定的 > 保存过的 > 组件默认。
//
// 两个刻意的取舍：
//   - 显式指定的方式**原样返回**，即使该组件并不支持它 —— 由调用方去报错。
//     若在这里悄悄换成默认值，使用者点了「native」却装了容器，还要翻日志才发现，
//     那才是更糟的体验（elasticsearch 就是这么一项）。
//   - 保存的方式若已不被支持（组件表变更后可能出现），退回默认而不是让安装失败。
func middlewareEffectiveMode(comp middleware.Component, saved, override string) string {
	if strings.TrimSpace(override) != "" {
		return override
	}
	for _, x := range comp.Modes {
		if x == saved {
			return saved
		}
	}
	return comp.DefaultMode
}

// middlewareInstallParams 汇总本次要用到的参数（含解密后的密码）：
// 既作为环境变量注入安装脚本，也存进 Run 的参数里成为安装记录。
//
// 注意：密码会随参数写进 Run 记录。这是**有意的** —— 使用者要能在运行详情页
// 看到「这次按什么参数装的」，否则密码随机生成后就彻底找不到了。
// 平台不对 Run 参数打码，因为它们本来就是使用者自己填的配置。
func (s *Server) middlewareInstallParams(keys []string, mode string) (map[string]string, error) {
	params := map[string]string{
		"components": strings.Join(keys, ","),
		"action":     middleware.ActionInstall,
	}
	for _, k := range keys {
		comp, ok := middleware.Lookup(k)
		if !ok {
			continue
		}
		cfg := s.loadMiddlewareConfig(k)
		params["mode."+k] = middlewareEffectiveMode(comp, cfg.Mode, mode)
		for name, v := range cfg.Values {
			params[name] = v
		}
		for name, enc := range cfg.Secrets {
			if enc == "" {
				continue
			}
			plain, err := s.box.Decrypt(enc)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt %s of %s: %w", name, k, err)
			}
			params[name] = plain
		}
	}
	return params, nil
}

// ---------- docker 探测（带短缓存） ----------

type dockerProbe struct {
	available bool
	version   string
	// containers: 容器名 → "running"/"exited"，一次 docker ps 全查回来，
	// 避免每个组件各 fork 一次。
	containers map[string]string
}

var (
	dockerProbeMu     sync.Mutex
	dockerProbeCache  *dockerProbe
	dockerProbeExpiry time.Time
)

func (s *Server) dockerProbe() dockerProbe {
	dockerProbeMu.Lock()
	defer dockerProbeMu.Unlock()
	if dockerProbeCache != nil && time.Now().Before(dockerProbeExpiry) {
		return *dockerProbeCache
	}
	p := dockerProbe{containers: map[string]string{}}
	env := runner.LoginEnv(s.cfg.ShellPreference)

	if path, err := runner.LookPathIn("docker", env); err == nil {
		p.available = true
		p.version = runBoundedOutput(env, 5*time.Second, path, "--version")
		// 一次拿到「所有容器名 + 状态」：docker ps -a 的输出是稳定两列。
		out := runBoundedOutput(env, 5*time.Second, path, "ps", "-a", "--format", "{{.Names}}\t{{.State}}")
		for _, line := range strings.Split(out, "\n") {
			parts := strings.SplitN(strings.TrimSpace(line), "\t", 2)
			if len(parts) == 2 && parts[0] != "" {
				p.containers[parts[0]] = parts[1]
			}
		}
	}

	dockerProbeCache = &p
	dockerProbeExpiry = time.Now().Add(middlewareProbeTTL)
	return p
}

// runBoundedOutput 跑一条命令取首行输出，带硬超时。
// docker CLI 在守护进程没起时可能等 socket，不能让它拖住 HTTP 请求。
func runBoundedOutput(env []string, timeout time.Duration, path string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	if len(env) > 0 {
		cmd.Env = env
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(firstLine(string(out)))
}

// ---------- 单个组件的探测 ----------

// nativeVersionCmd 是各中间件原生方式下的版本命令。
// 不放进 middleware.Component，因为那是「安装计划」的一部分，而这里只是探测展示。
var nativeVersionCmd = map[string][]string{
	"postgresql":    {"psql", "--version"},
	"mysql":         {"mysql", "--version"},
	"redis":         {"redis-server", "--version"},
	"elasticsearch": {"elasticsearch", "--version"},
}

func (s *Server) probeMiddleware(comp middleware.Component) middlewareComponentDTO {
	env := runner.LoginEnv(s.cfg.ShellPreference)
	dto := middlewareComponentDTO{
		Key: comp.Key, Name: comp.Name, Note: comp.Note, Category: comp.Category,
		Estimate: comp.Estimate, Modes: comp.Modes, DefaultMode: comp.DefaultMode,
		NativeSupported: comp.NativeSupported,
		Probes:          make([]middlewareProbeDTO, 0, len(comp.Probes)),
	}
	for _, p := range comp.Probes {
		dto.Probes = append(dto.Probes, middlewareProbeDTO{Name: p.Name})
	}

	dp := s.dockerProbe()

	// docker 本体：只看 CLI 与守护进程，没有容器维度。
	if comp.Key == "docker" {
		dto.Installed = dp.available
		dto.Version = dp.version
		dto.EffectiveMode = middleware.ModeNative
		if dp.available {
			if path, err := runner.LookPathIn("docker", env); err == nil {
				dto.Path = path
				// docker info 成功 = 守护进程可用。
				if runBoundedOutput(env, 5*time.Second, path, "info", "--format", "{{.ServerVersion}}") != "" {
					dto.Running = true
				}
			}
		}
		if dto.Installed && !dto.Running {
			dto.Detail = "已安装，但守护进程未运行（systemctl enable --now docker）"
		}
		markProbes(dto.Probes, dp.available)
		return dto
	}

	// 其余四项：容器方式优先，其次原生包。
	container := middleware.ContainerName(comp.Key)
	state, hasContainer := "", false
	if dp.available {
		state, hasContainer = dp.containers[container]
	}
	volume := middleware.VolumeName(comp.Key)
	port := middlewareConfiguredPort(comp, s.loadMiddlewareConfig(comp.Key))

	nativePath, nativeVersion := "", ""
	if cmd, ok := nativeVersionCmd[comp.Key]; ok {
		if path, err := runner.LookPathIn(cmd[0], env); err == nil {
			nativePath = path
			nativeVersion = runBoundedOutput(env, 5*time.Second, path, cmd[1:]...)
		}
	}

	switch {
	case hasContainer:
		dto.Installed = true
		dto.Running = state == "running"
		dto.EffectiveMode = middleware.ModeDocker
		dto.Version = s.containerImage(env, container)
		dto.Path = "container:" + container
		dto.Detail = fmt.Sprintf("容器 %s（%s）  卷 %s", container, state, volume)
		if port != "" {
			dto.Detail += "  端口 " + port
		}
		if !dto.Running {
			dto.Detail += "  —— 已停止，点「启动所选」拉起"
		}
	case nativePath != "":
		dto.Installed = true
		dto.EffectiveMode = middleware.ModeNative
		dto.Version = nativeVersion
		dto.Path = nativePath
		dto.Running = s.nativeServiceActive(comp.Key, env)
		dto.Detail = "原生安装（" + nativePath + "）"
		if port != "" {
			dto.Detail += "  端口 " + port
		}
		if !dto.Running {
			dto.Detail += "  —— 已停止"
		}
	default:
		dto.EffectiveMode = middlewareEffectiveMode(comp, s.loadMiddlewareConfig(comp.Key).Mode, "")
		dto.Path = "将创建容器 " + container + "，数据卷 " + volume
	}

	// 逐项标注探测结果。不能笼统地用 dto.Installed 覆盖全部探测项：
	// 「容器方式装了」和「原生方式装了」是两条互斥的事实，
	// 全标成命中会让人看到「容器 ✔ + psql ✔」这种自相矛盾的面板
	// （开发机上装着 PostgreSQL、同时并没有容器时就会出现）。
	for i := range dto.Probes {
		name := comp.Probes[i].Name
		switch {
		case strings.HasPrefix(name, "容器 "):
			dto.Probes[i].Found = hasContainer
		default:
			dto.Probes[i].Found = nativePath != ""
		}
	}
	return dto
}

// markProbes 把全部探测项标成同一个结果（仅 docker 本体这种单探测项的场景使用）。
func markProbes(probes []middlewareProbeDTO, found bool) {
	for i := range probes {
		probes[i].Found = found
	}
}

// containerImage 读容器用的镜像 tag，作为「版本」显示。
func (s *Server) containerImage(env []string, container string) string {
	path, err := runner.LookPathIn("docker", env)
	if err != nil {
		return ""
	}
	return runBoundedOutput(env, 5*time.Second, path, "inspect", "-f", "{{.Config.Image}}", container)
}

// nativeServiceActive 查 systemd 单元是否在跑。
// 没有 systemd、或不是 Linux 时保守地当作在跑 —— 把「能用」报成「没在跑」会误导使用者去点启动。
func (s *Server) nativeServiceActive(key string, env []string) bool {
	if runtime.GOOS != "linux" {
		return true
	}
	units := map[string][]string{
		"postgresql":    {"postgresql"},
		"mysql":         {"mysql", "mariadb"},
		"redis":         {"redis-server", "redis"},
		"elasticsearch": {"elasticsearch"},
	}
	path, err := runner.LookPathIn("systemctl", env)
	if err != nil {
		return true
	}
	for _, u := range units[key] {
		if runBoundedOutput(env, 5*time.Second, path, "is-active", u) == "active" {
			return true
		}
	}
	return false
}

// middlewareConfiguredPort 取该组件保存的端口参数（用于在详情里显示）。
func middlewareConfiguredPort(comp middleware.Component, cfg middlewareConfig) string {
	for _, f := range comp.Fields {
		// 端口类参数的命名约定是 *_PORT；ES_TRANSPORT_PORT 是节点间端口，展示主端口时跳过。
		if strings.HasSuffix(f.Key, "_PORT") && !strings.Contains(f.Key, "TRANSPORT") {
			if v, ok := cfg.Values[f.Key]; ok && v != "" {
				return v
			}
			return f.Default
		}
	}
	return ""
}

// middlewareFields 组装某个组件的可编辑参数（含已保存值；密码只回是否已设置）。
func (s *Server) middlewareFields(comp middleware.Component) []middlewareFieldDTO {
	cfg := s.loadMiddlewareConfig(comp.Key)
	out := make([]middlewareFieldDTO, 0, len(comp.Fields))
	for _, f := range comp.Fields {
		dto := middlewareFieldDTO{
			Key: f.Key, Label: f.Label, Default: f.Default,
			Placeholder: f.Placeholder, Help: f.Help, Secret: f.Secret,
		}
		if f.Secret {
			// 明文永不出参：只告诉前端「已经设过密码了」。
			dto.Set = strings.TrimSpace(cfg.Secrets[f.Key]) != ""
		} else {
			dto.Value = cfg.Values[f.Key]
		}
		out = append(out, dto)
	}
	return out
}

// ---------- 接口 ----------

// listMiddleware 返回可安装的中间件及其当前状态与可编辑参数。
func (s *Server) listMiddleware(c *gin.Context) {
	dp := s.dockerProbe()
	list := make([]middlewareComponentDTO, 0, len(middleware.Components))
	missing := make([]string, 0, len(middleware.Components))
	running := make([]string, 0, len(middleware.Components))
	for _, comp := range middleware.Components {
		dto := s.probeMiddleware(comp)
		dto.Mode = middlewareEffectiveMode(comp, s.loadMiddlewareConfig(comp.Key).Mode, "")
		dto.Fields = s.middlewareFields(comp)
		if !dto.Installed {
			missing = append(missing, dto.Key)
		}
		if dto.Running {
			running = append(running, dto.Key)
		}
		list = append(list, dto)
	}

	path := middleware.ScriptPath(s.cfg.DataDir)
	OK(c, gin.H{
		"supported":       runtime.GOOS == "linux",
		"platform":        runtime.GOOS + "/" + runtime.GOARCH,
		"dockerAvailable": dp.available,
		"dockerVersion":   dp.version,
		"components":      list,
		"missing":         missing,
		"running":         running,
		"actions":         middleware.Actions,
		"scriptPath":      path,
		"scriptUrl":       "/api/v1/system/middleware/script",
		"projectKey":      middlewareProjectKey,
		"mirror":          "cn",
		"hint": "安装与启停都是一次普通运行，可以在运行详情页看实时日志、取消或重跑；" +
			"移除只删容器/包，数据卷与数据目录会保留",
		"installCmd": "bash " + path + " --only <中间件> [--mode docker|native] [--action install]",
	})
}

type middlewareFieldsReq struct {
	Fields map[string]string `json:"fields"`
	Mode   string            `json:"mode"`
}

// updateMiddlewareFields 保存某个中间件的参数与安装方式（仅 admin）。
//
// 密码参数的处理与工具链不同：传空串表示**清空已保存的密码**（回到「随机生成」），
// 传 placeholderSecret（前端回显的占位符）或根本不传该键表示**保持原密码不变**。
// 这样「只改端口」不会误把密码清掉，而「想清空密码」也有明确表达方式。
func (s *Server) updateMiddlewareFields(c *gin.Context) {
	key := c.Param("key")
	comp, ok := middleware.Lookup(key)
	if !ok {
		BadRequest(c, "unknown middleware: "+key+" (available: "+strings.Join(middleware.AllKeys(), ",")+")")
		return
	}
	var req middlewareFieldsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, `request body must look like {"fields":{"PG_PORT":"5432"},"mode":"docker"}`)
		return
	}

	allowed := map[string]middleware.Field{}
	for _, f := range comp.Fields {
		allowed[f.Key] = f
	}

	cfg := s.loadMiddlewareConfig(key)
	if cfg.Values == nil {
		cfg.Values = map[string]string{}
	}
	if cfg.Secrets == nil {
		cfg.Secrets = map[string]string{}
	}

	changed := make([]string, 0, len(req.Fields))
	for k, v := range req.Fields {
		f, ok := allowed[k]
		if !ok {
			BadRequest(c, fmt.Sprintf("middleware %s has no parameter %s", key, k))
			return
		}
		v = strings.TrimSpace(v)
		changed = append(changed, k)
		if f.Secret {
			switch {
			case v == "":
				delete(cfg.Secrets, k) // 显式清空 → 下次安装随机生成
			case v == placeholderSecret:
				// 前端回显的占位符：原样保留密文
			default:
				enc, err := s.box.Encrypt(v)
				if err != nil {
					Internal(c, "failed to encrypt the parameter: "+err.Error())
					return
				}
				cfg.Secrets[k] = enc
			}
			continue
		}
		if v == "" {
			delete(cfg.Values, k) // 空值等价于回默认
		} else {
			cfg.Values[k] = v
		}
	}

	if req.Mode != "" {
		supported := false
		for _, m := range comp.Modes {
			if m == req.Mode {
				supported = true
			}
		}
		if !supported {
			BadRequest(c, fmt.Sprintf("middleware %s does not support mode %s (available: %s)",
				key, req.Mode, strings.Join(comp.Modes, ",")))
			return
		}
		cfg.Mode = req.Mode
	}

	if err := s.saveMiddlewareConfig(key, cfg); err != nil {
		Internal(c, "failed to save the parameters: "+err.Error())
		return
	}
	sort.Strings(changed)
	s.audit(c, "update-middleware-params", key, strings.Join(changed, " ")+" mode="+cfg.Mode)

	OK(c, gin.H{"key": key, "fields": s.middlewareFields(comp), "mode": cfg.Mode})
}

// downloadMiddlewareScript 把内嵌的安装脚本原样返回，供人工审查或手动执行。
func (s *Server) downloadMiddlewareScript(c *gin.Context) {
	body, err := middleware.Script()
	if err != nil {
		Internal(c, "failed to read the embedded installer: "+err.Error())
		return
	}
	c.Header("Content-Type", "text/x-shellscript; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="install-middleware.sh"`)
	c.String(http.StatusOK, string(body))
}

type middlewareInstallReq struct {
	// Components 为空表示「所有缺失的中间件」（仅 install 动作允许为空）
	Components []string `json:"components"`
	Mode       string   `json:"mode"`   // docker | native，空 = 各组件默认
	Action     string   `json:"action"` // install（默认）/ start / stop / restart / remove / upgrade / logs
	DryRun     bool     `json:"dryRun"`
	Force      bool     `json:"force"`
	Mirror     string   `json:"mirror"` // cn | off
}

// installMiddleware 触发一次中间件操作，返回可以直接跳过去看日志的 Run。
func (s *Server) installMiddleware(c *gin.Context) {
	var req middlewareInstallReq
	_ = c.ShouldBindJSON(&req)

	action := strings.TrimSpace(req.Action)
	if action == "" {
		action = middleware.ActionInstall
	}
	if !middleware.IsAction(action) {
		BadRequest(c, "unknown action: "+action+" (available: "+strings.Join(middleware.Actions, ",")+")")
		return
	}
	if m := strings.TrimSpace(req.Mode); m != "" && m != middleware.ModeDocker && m != middleware.ModeNative {
		BadRequest(c, "mode must be docker or native (got: "+m+")")
		return
	}

	keys := make([]string, 0, len(req.Components))
	seen := map[string]bool{}
	for _, k := range req.Components {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		if _, ok := middleware.Lookup(k); !ok {
			BadRequest(c, "unknown middleware: "+k+" (available: "+strings.Join(middleware.AllKeys(), ",")+")")
			return
		}
		seen[k] = true
		keys = append(keys, k)
	}

	// 空列表只对 install 有意义（= 装所有缺失的）；启停/移除必须明确点名，
	// 否则「清空勾选后点了停止」会意外把所有中间件停掉。
	if len(keys) == 0 {
		if action != middleware.ActionInstall {
			BadRequest(c, "components must not be empty for action "+action)
			return
		}
		for _, comp := range middleware.Components {
			if !s.probeMiddleware(comp).Installed {
				keys = append(keys, comp.Key)
			}
		}
	}
	if len(keys) == 0 {
		OK(c, gin.H{"runId": 0, "nothingToDo": true, "message": "all middleware components are already installed"})
		return
	}

	// 脚本是 bash + 发行版包管理器 + docker CLI 写的，只在 Linux 上有意义。
	// 预演不做任何修改，允许在别的平台上先看它打算做什么。
	if runtime.GOOS != "linux" && !req.DryRun {
		BadRequest(c, "middleware installation is only available on Linux servers (current: "+runtime.GOOS+"); use dryRun to preview the plan")
		return
	}

	// 不支持的组合提前拦掉，比让使用者在日志里发现「这一项被跳过了」友好得多。
	for _, k := range keys {
		comp, _ := middleware.Lookup(k)
		mode := middlewareEffectiveMode(comp, s.loadMiddlewareConfig(k).Mode, req.Mode)
		supported := false
		for _, m := range comp.Modes {
			if m == mode {
				supported = true
			}
		}
		if !supported {
			BadRequest(c, fmt.Sprintf("%s does not support the %s mode (available: %s)",
				k, mode, strings.Join(comp.Modes, ",")))
			return
		}
	}

	scriptPath, err := middleware.EnsureScript(s.cfg.DataDir)
	if err != nil {
		Internal(c, "failed to materialize the installer: "+err.Error())
		return
	}

	args := []string{"bash", shellQuote(scriptPath), "--only", strings.Join(keys, ","), "--action", action}
	if req.DryRun {
		args = append(args, "--dry-run")
	}
	if req.Force {
		args = append(args, "--force")
	}
	if req.Mirror == "off" {
		args = append(args, "--mirror", "off")
	}
	// 逐组件的方式：脚本只认一个全局 --mode，而各组件的方式可能不同，
	// 所以用 --mode-of <组件>=<方式> 逐个传（它优先于 --mode）。
	for _, k := range keys {
		comp, _ := middleware.Lookup(k)
		args = append(args, "--mode-of", k+"="+middlewareEffectiveMode(comp, s.loadMiddlewareConfig(k).Mode, req.Mode))
	}
	command := strings.Join(args, " ")

	p, cmdID, err := s.ensureMiddlewareProject(c)
	if err != nil {
		Internal(c, "failed to prepare the system project: "+err.Error())
		return
	}
	if err := s.st.DB().Model(&store.ProjectCommand{}).Where("id = ?", cmdID).
		Updates(map[string]any{"name": action + " middleware", "script": command, "enabled": true}).Error; err != nil {
		Internal(c, "failed to update the command: "+err.Error())
		return
	}

	params, err := s.middlewareInstallParams(keys, req.Mode)
	if err != nil {
		Internal(c, "failed to prepare the parameters: "+err.Error())
		return
	}
	params["action"] = action

	u := CurrentUser(c)
	run, err := s.exec.Submit(runner.SubmitRequest{
		ProjectID:   p.ID,
		CommandIDs:  []uint{cmdID},
		Params:      params,
		TriggerType: store.TriggerWeb,
		TriggerUser: u.Username,
		TriggerUID:  u.ID,
		TriggerIP:   ClientIP(c),
		TriggerUA:   c.GetHeader("User-Agent"),
	})
	if err != nil {
		Internal(c, "failed to start the operation: "+err.Error())
		return
	}
	s.audit(c, "middleware-"+action, strings.Join(keys, ","), "run="+strconv.Itoa(int(run.ID)))

	OK(c, gin.H{
		"runId": run.ID, "buildNumber": run.BuildNumber, "status": run.Status,
		"projectKey": p.Key, "components": keys, "action": action, "dryRun": req.DryRun,
		"command": command,
		"logUrl":  "/api/v1/runs/" + strconv.Itoa(int(run.ID)) + "/log",
	})
}

// ensureMiddlewareProject 保证内置中间件项目与它唯一的那条命令存在。
//
// 与工具链安装项目分开建（system-middleware vs system-toolchain）：
// 两者的运行历史混在一起会让「上次装数据库是什么时候」变得难查。
func (s *Server) ensureMiddlewareProject(c *gin.Context) (*store.Project, uint, error) {
	u := CurrentUser(c)

	var p store.Project
	err := s.st.DB().Where("key = ?", middlewareProjectKey).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		p = store.Project{
			Key:         middlewareProjectKey,
			Name:        "中间件安装（系统）",
			Description: "由「环境自检 → 中间件」使用；每次安装/启停都是一次普通运行，可看日志、可取消。",
			OwnerID:     u.ID,
			OwnerName:   u.Username,
			Workdir:     filepath.Join(s.cfg.WebRoot, middlewareProjectKey),
			Shell:       "auto",
			Concurrency: store.ConcurrencyQueue,
			HookEnabled: false, // 不给它 Hook，避免被外部触发
			Kind:        store.KindApp,
		}
		if err := s.st.DB().Create(&p).Error; err != nil {
			return nil, 0, err
		}
		// HookEnabled=false 是零值，带 default:true 的列会被 GORM 跳过，
		// 必须显式落库（同 handlers_project.go / handlers_toolchain.go）。
		if err := s.st.DB().Model(&store.Project{}).Where("id = ?", p.ID).
			Update("hook_enabled", false).Error; err != nil {
			return nil, 0, err
		}
	} else if err != nil {
		return nil, 0, err
	}

	var cmd store.ProjectCommand
	err = s.st.DB().Where("project_id = ?", p.ID).Order("id asc").First(&cmd).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		cmd = store.ProjectCommand{
			ProjectID: p.ID, Name: "install middleware",
			Script: "bash " + shellQuote(middleware.ScriptPath(s.cfg.DataDir)) +
				" --only " + strings.Join(middleware.AllKeys(), ","),
			SortOrder: 1, Enabled: true, FailStrategy: store.FailAbort,
		}
		if err := s.st.DB().Create(&cmd).Error; err != nil {
			return nil, 0, err
		}
	} else if err != nil {
		return nil, 0, err
	}
	return &p, cmd.ID, nil
}
