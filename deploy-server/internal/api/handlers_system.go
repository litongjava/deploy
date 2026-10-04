package api

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"deploy-server/internal/runlog"
	"deploy-server/internal/runner"
	"deploy-server/internal/store"
	"deploy-server/internal/web"
)

// toolProbe 描述一项环境自检。
//
// 注意这里的定位是「自检」而不是「安装」（需求 D6）：平台不代替使用者
// 安装工具链，只报告有没有、什么版本、以及缺了该怎么装。
type toolProbe struct {
	Name string
	Args []string
	Hint string
}

var toolProbes = []toolProbe{
	{"git", []string{"--version"}, "Git 准备阶段与脚本里的 git 命令都需要。Debian/Ubuntu: apt install -y git"},
	{"bash", []string{"-c", "echo $BASH_VERSION"}, "Unix 下默认用它执行脚本。Debian/Ubuntu: apt install -y bash"},
	{"sh", []string{"-c", "echo ok"}, "bash 不可用时的兜底解释器。"},
	{"rsync", []string{"--version"}, "把构建产物同步到站点目录时最常用。Debian/Ubuntu: apt install -y rsync"},
	{"curl", []string{"--version"}, "脚本里做健康检查、调接口时常用。Debian/Ubuntu: apt install -y curl"},
	{"docker", []string{"--version"}, "需要以容器方式部署时使用。见 https://docs.docker.com/engine/install/"},
	{"nginx", []string{"-v"}, "托管静态站点需要。Debian/Ubuntu: apt install -y nginx"},
	{"node", []string{"--version"}, "构建前端项目需要。建议用 nvm 安装。"},
	{"npm", []string{"--version"}, "随 Node.js 一起安装。"},
	{"pnpm", []string{"--version"}, "前端常用包管理器。npm install -g pnpm"},
	{"go", []string{"version"}, "构建 Go 项目需要。见 https://go.dev/dl/"},
	{"java", []string{"-version"}, "构建 Java 项目需要。Debian/Ubuntu: apt install -y openjdk-17-jdk"},
	{"mvn", []string{"--version"}, "构建 Maven 项目需要。Debian/Ubuntu: apt install -y maven"},
	{"certbot", []string{"--version"}, "签发 HTTPS 证书需要。见 https://certbot.eff.org/"},
}

type checkDTO struct {
	Name    string `json:"name"`
	Found   bool   `json:"found"`
	Version string `json:"version"`
	Path    string `json:"path"`
	Hint    string `json:"hint"`
}

// doctor 是环境自检接口。
func (s *Server) doctor(c *gin.Context) {
	shellName, shellPath, shellAvail := runner.ShellDescription(s.cfg.ShellPreference)

	checks := make([]checkDTO, 0, len(toolProbes))
	for _, p := range toolProbes {
		checks = append(checks, probeTool(p))
	}

	hostname, _ := os.Hostname()
	webRootWritable := dirWritable(s.cfg.WebRoot)
	dataDirWritable := dirWritable(s.cfg.DataDir)
	built := web.Available()

	OK(c, gin.H{
		"os": runtime.GOOS, "arch": runtime.GOARCH, "hostname": hostname,
		"goVersion": runtime.Version(), "serverVersion": s.version,
		"uptimeSec": int(time.Since(s.startedAt).Seconds()),
		"webRoot":   s.cfg.WebRoot, "webRootWritable": webRootWritable,
		"dataDir": s.cfg.DataDir, "dataDirWritable": dataDirWritable,
		"shell":  gin.H{"selected": shellName, "path": shellPath, "available": shellAvail},
		"checks": checks,
		// 前端资源是否真的构建过：嵌了占位页时给出明确提示。
		"web": gin.H{"built": built, "note": webNote(built)},
	})
}

func webNote(built bool) string {
	if built {
		return ""
	}
	return "当前二进制内嵌的是占位页，前端尚未构建。请在 deploy-server-ui 执行 pnpm build，再运行 make ui 重新编译后端。"
}

func probeTool(p toolProbe) checkDTO {
	dto := checkDTO{Name: p.Name, Hint: p.Hint}
	path, err := exec.LookPath(p.Name)
	if err != nil {
		return dto
	}
	dto.Found = true
	dto.Path = path

	// 给版本探测一个硬超时：有些工具会去连网/等锁，不能让它拖垮自检接口。
	cmd := exec.Command(path, p.Args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	done := make(chan string, 1)
	go func() {
		out, _ := cmd.CombinedOutput()
		done <- string(out)
	}()
	select {
	case out := <-done:
		dto.Version = firstLine(out)
	case <-time.After(5 * time.Second):
		dto.Version = "(版本探测超时)"
		_ = cmd.Process.Kill()
	}
	return dto
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return strings.TrimSpace(s)
}

// dirWritable 通过真的写一个临时文件来判断可写，而不是看权限位。
func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".writecheck-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

// ---------- 系统设置 ----------

type settingsDTO struct {
	WebRoot                string    `json:"webRoot"`
	DataDir                string    `json:"dataDir"`
	ServerURL              string    `json:"serverUrl"`
	Shell                  string    `json:"shell"`
	MaxConcurrentRuns      int       `json:"maxConcurrentRuns"`
	LogRetentionDays       int       `json:"logRetentionDays"`
	LogMaxBytes            int64     `json:"logMaxBytes"`
	HookRateLimitPerMinute int       `json:"hookRateLimitPerMinute"`
	Visibility             string    `json:"visibility"`
	AllowRegistration      bool      `json:"allowRegistration"`
	Notify                 notifyDTO `json:"notify"`
}

type notifyDTO struct {
	Enabled bool     `json:"enabled"`
	URL     string   `json:"url"`
	On      []string `json:"on"`
}

// settingInt 读运行时可变的整数设置，缺省返回 def。
func (s *Server) settingInt(key string, def int) int {
	if v, ok := s.st.GetSetting(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func (s *Server) settingStr(key, def string) string {
	if v, ok := s.st.GetSetting(key); ok && v != "" {
		return v
	}
	return def
}

func (s *Server) getSettings(c *gin.Context) {
	notify := notifyDTO{Enabled: false, On: []string{"failed"}}
	if v, ok := s.st.GetSetting("notifyEnabled"); ok {
		notify.Enabled = v == "true"
	}
	notify.URL = s.settingStr("notifyUrl", "")
	if v, ok := s.st.GetSetting("notifyOn"); ok {
		notify.On = strings.Split(v, ",")
	}

	OK(c, settingsDTO{
		WebRoot: s.cfg.WebRoot, DataDir: s.cfg.DataDir, ServerURL: s.cfg.ServerURL,
		Shell:                  s.cfg.ShellPreference,
		MaxConcurrentRuns:      s.exec.MaxConcurrent(),
		LogRetentionDays:       s.settingInt("logRetentionDays", s.cfg.LogRetentionDays),
		LogMaxBytes:            int64(s.settingInt("logMaxBytes", int(s.cfg.LogMaxBytes))),
		HookRateLimitPerMinute: s.effectiveHookLimit(),
		Visibility:             s.visibilityMode(),
		AllowRegistration:      s.cfg.AllowRegistration,
		Notify:                 notify,
	})
}

// updateSettings 只允许改「运行时安全」的设置。
// 工作目录、监听地址这类影响进程本身的配置，坚持要改配置文件 + 重启，
// 避免出现「设置里显示 A、进程实际跑在 B」的错位。
func (s *Server) updateSettings(c *gin.Context) {
	var req settingsDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "请求体不是合法 JSON")
		return
	}

	if req.MaxConcurrentRuns > 0 {
		if req.MaxConcurrentRuns > 100 {
			BadRequest(c, "最大并发数请设置在 1-100 之间")
			return
		}
		s.st.SetSetting("maxConcurrentRuns", strconv.Itoa(req.MaxConcurrentRuns))
		// 直接改闸门上限，设置立即生效，不需要重启。
		s.exec.SetMaxConcurrent(req.MaxConcurrentRuns)
	}
	if req.LogRetentionDays >= 0 {
		s.st.SetSetting("logRetentionDays", strconv.Itoa(req.LogRetentionDays))
	}
	if req.LogMaxBytes > 0 {
		s.st.SetSetting("logMaxBytes", strconv.FormatInt(req.LogMaxBytes, 10))
	}
	if req.HookRateLimitPerMinute >= 0 {
		s.st.SetSetting("hookRateLimitPerMinute", strconv.Itoa(req.HookRateLimitPerMinute))
	}
	if req.Visibility == "allReadable" || req.Visibility == "ownerOnly" {
		s.st.SetSetting("visibility", req.Visibility)
	}
	s.st.SetSetting("notifyEnabled", strconv.FormatBool(req.Notify.Enabled))
	s.st.SetSetting("notifyUrl", req.Notify.URL)
	s.st.SetSetting("notifyOn", strings.Join(req.Notify.On, ","))

	s.audit(c, "update-settings", "system", "")
	s.getSettings(c)
}

// cleanupOldRuns 按保留天数清理历史运行与日志文件。
// 由 main 里的定时任务调用，也可以在启动时跑一次。
func (s *Server) CleanupOldRuns() (int, error) {
	days := s.settingInt("logRetentionDays", s.cfg.LogRetentionDays)
	if days <= 0 {
		return 0, nil
	}
	cutoff := time.Now().AddDate(0, 0, -days)

	var runs []store.Run
	if err := s.st.DB().Where("created_at < ? AND status NOT IN ?", cutoff,
		[]string{store.RunRunning, store.RunQueued}).Find(&runs).Error; err != nil {
		return 0, err
	}
	if len(runs) == 0 {
		return 0, nil
	}

	ids := make([]uint, 0, len(runs))
	for _, r := range runs {
		ids = append(ids, r.ID)
		_ = os.Remove(runlog.PathFor(s.cfg.LogDir(), r.ProjectKey, r.ID))
	}
	s.st.DB().Where("run_id IN ?", ids).Delete(&store.RunStep{})
	s.st.DB().Where("id IN ?", ids).Delete(&store.Run{})
	return len(ids), nil
}
