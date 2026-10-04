package api

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"deploy-server/internal/runner"
	"deploy-server/internal/store"
	"deploy-server/internal/toolchain"
)

// 环境工具链一键安装（页面：设置 → 系统设置 → 环境自检）。
//
// 实现取巧但很值：安装脚本嵌入二进制后落盘，再当作**一个普通 Run** 跑。
// 于是实时日志、取消、重跑、下载、日志脱敏、并发排队全部直接复用，
// 不需要为「安装进度」另造一套上报通道。

// toolchainProjectKey 是承载安装任务的内置项目 key。
const toolchainProjectKey = "system-toolchain"

type toolchainProbeDTO struct {
	Name    string `json:"name"`
	Found   bool   `json:"found"`
	Version string `json:"version"`
	Path    string `json:"path"`
}

type toolchainComponentDTO struct {
	Key       string              `json:"key"`
	Name      string              `json:"name"`
	Note      string              `json:"note"`
	Estimate  string              `json:"estimate"`
	Installed bool                `json:"installed"`
	Version   string              `json:"version"`
	Path      string              `json:"path"`
	Probes    []toolchainProbeDTO `json:"probes"`
}

// probeComponent 检测一个组件是否已安装（全部探针命中才算）。
func probeComponent(c toolchain.Component) toolchainComponentDTO {
	dto := toolchainComponentDTO{
		Key: c.Key, Name: c.Name, Note: c.Note, Estimate: c.Estimate,
		Installed: true, Probes: make([]toolchainProbeDTO, 0, len(c.Probes)),
	}
	versions := make([]string, 0, len(c.Probes))
	paths := make([]string, 0, len(c.Probes))
	for _, p := range c.Probes {
		probe := toolchainProbeDTO{Name: p.Name}
		if len(p.Paths) > 0 {
			for _, path := range p.Paths {
				if fileExists(path) {
					probe.Found, probe.Path = true, path
					break
				}
			}
		} else {
			if path, err := exec.LookPath(p.Name); err == nil {
				probe.Found, probe.Path = true, path
				probe.Version = probeVersion(path, p.Args)
			}
		}
		if !probe.Found {
			dto.Installed = false
		}
		if probe.Version != "" {
			versions = append(versions, probe.Version)
		}
		if probe.Path != "" {
			paths = append(paths, probe.Path)
		}
		dto.Probes = append(dto.Probes, probe)
	}
	dto.Version = strings.Join(versions, " / ")
	dto.Path = strings.Join(paths, " / ")
	return dto
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	if p, err := exec.LookPath(path); err == nil {
		path = p
	}
	_, err := os.Stat(path)
	return err == nil
}

// probeVersion 跑一次版本命令，带硬超时（有些工具会去连网）。
func probeVersion(path string, args []string) string {
	if len(args) == 0 {
		return ""
	}
	cmd := exec.Command(path, args...)
	done := make(chan string, 1)
	go func() {
		out, _ := cmd.CombinedOutput()
		done <- string(out)
	}()
	select {
	case out := <-done:
		return firstLine(out)
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		return "(version check timed out)"
	}
}

// listToolchains 返回可安装组件及其当前状态。
func (s *Server) listToolchains(c *gin.Context) {
	list := make([]toolchainComponentDTO, 0, len(toolchain.Components))
	missing := make([]string, 0, len(toolchain.Components))
	for _, comp := range toolchain.Components {
		dto := probeComponent(comp)
		if !dto.Installed {
			missing = append(missing, dto.Key)
		}
		list = append(list, dto)
	}

	path := toolchain.ScriptPath(s.cfg.DataDir)
	OK(c, gin.H{
		"supported":  runtime.GOOS == "linux",
		"platform":   runtime.GOOS + "/" + runtime.GOARCH,
		"components": list,
		"missing":    missing,
		"scriptPath": path,
		"scriptUrl":  "/api/v1/system/toolchains/script",
		"projectKey": toolchainProjectKey,
		"mirror":     "cn",
		"hint":       "安装以普通 Run 的形式执行，可以在运行详情页看实时日志、取消或重跑",
		"installCmd": "bash " + path + " --only <组件>",
	})
}

// downloadToolchainScript 把内嵌的安装脚本原样返回，供人工审查或手动执行。
func (s *Server) downloadToolchainScript(c *gin.Context) {
	body, err := toolchain.Script()
	if err != nil {
		Internal(c, "failed to read the embedded installer: "+err.Error())
		return
	}
	c.Header("Content-Type", "text/x-shellscript; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="install-toolchains.sh"`)
	c.String(http.StatusOK, string(body))
}

type toolchainInstallReq struct {
	// Components 为空表示「所有缺失的组件」
	Components []string `json:"components"`
	DryRun     bool     `json:"dryRun"`
	Force      bool     `json:"force"`
	Mirror     string   `json:"mirror"` // cn | off
}

// installToolchains 触发一次安装，返回可以直接跳过去看日志的 Run。
func (s *Server) installToolchains(c *gin.Context) {
	var req toolchainInstallReq
	_ = c.ShouldBindJSON(&req)

	keys := make([]string, 0, len(req.Components))
	seen := map[string]bool{}
	for _, k := range req.Components {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		if _, ok := toolchain.Lookup(k); !ok {
			BadRequest(c, "unknown component: "+k+" (available: "+strings.Join(toolchain.AllKeys(), ",")+")")
			return
		}
		seen[k] = true
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		// 默认只装缺失的，避免重复安装把已装好的环境折腾一遍
		for _, comp := range toolchain.Components {
			if !probeComponent(comp).Installed {
				keys = append(keys, comp.Key)
			}
		}
	}
	if len(keys) == 0 {
		OK(c, gin.H{"runId": 0, "nothingToDo": true, "message": "all components are already installed"})
		return
	}

	// 安装脚本是 bash + 发行版包管理器写的，只在 Linux 上有意义。
	// 预演（dry-run）不做任何修改，允许在别的平台上先看它打算做什么。
	if runtime.GOOS != "linux" && !req.DryRun {
		BadRequest(c, "one-click toolchain installation is only available on Linux servers (current: "+runtime.GOOS+"); use dryRun to preview the plan")
		return
	}

	scriptPath, err := toolchain.EnsureScript(s.cfg.DataDir)
	if err != nil {
		Internal(c, "failed to materialize the installer: "+err.Error())
		return
	}

	args := []string{"bash", shellQuote(scriptPath), "--only", strings.Join(keys, ",")}
	if req.DryRun {
		args = append(args, "--dry-run")
	}
	if req.Force {
		args = append(args, "--force")
	}
	if req.Mirror == "off" {
		args = append(args, "--mirror", "off")
	}
	command := strings.Join(args, " ")

	p, cmdID, err := s.ensureToolchainProject(c)
	if err != nil {
		Internal(c, "failed to prepare the system project: "+err.Error())
		return
	}
	if err := s.st.DB().Model(&store.ProjectCommand{}).Where("id = ?", cmdID).
		Updates(map[string]any{"name": "install toolchains", "script": command, "enabled": true}).Error; err != nil {
		Internal(c, "failed to update the install command: "+err.Error())
		return
	}

	u := CurrentUser(c)
	run, err := s.exec.Submit(runner.SubmitRequest{
		ProjectID:   p.ID,
		CommandIDs:  []uint{cmdID},
		TriggerType: store.TriggerWeb,
		TriggerUser: u.Username,
		TriggerUID:  u.ID,
		TriggerIP:   ClientIP(c),
		TriggerUA:   c.GetHeader("User-Agent"),
	})
	if err != nil {
		Internal(c, "failed to start the installation: "+err.Error())
		return
	}
	s.audit(c, "install-toolchains", strings.Join(keys, ","), "run="+strconv.Itoa(int(run.ID)))

	OK(c, gin.H{
		"runId": run.ID, "buildNumber": run.BuildNumber, "status": run.Status,
		"projectKey": p.Key, "components": keys, "dryRun": req.DryRun,
		"command": command,
		"logUrl":  "/api/v1/runs/" + strconv.Itoa(int(run.ID)) + "/log",
	})
}

// ensureToolchainProject 保证内置安装项目与它唯一的那条命令存在。
func (s *Server) ensureToolchainProject(c *gin.Context) (*store.Project, uint, error) {
	u := CurrentUser(c)

	var p store.Project
	err := s.st.DB().Where("key = ?", toolchainProjectKey).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		p = store.Project{
			Key:         toolchainProjectKey,
			Name:        "环境工具链安装（系统）",
			Description: "由「系统设置 → 环境自检 → 一键安装工具链」使用；每次安装都是一次普通运行，可看日志、可取消。",
			OwnerID:     u.ID,
			OwnerName:   u.Username,
			Workdir:     filepath.Join(s.cfg.WebRoot, toolchainProjectKey),
			Shell:       "auto",
			Concurrency: store.ConcurrencyQueue,
			HookEnabled: false, // 不给它 Hook，避免被外部触发
			Kind:        store.KindApp,
		}
		if err := s.st.DB().Create(&p).Error; err != nil {
			return nil, 0, err
		}
		// HookEnabled=false 是零值，带 default:true 的列会被 GORM 跳过，
		// 必须显式落库，否则这个内置项目反而会被 Hook 触发（同 handlers_project.go）。
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
			ProjectID: p.ID, Name: "install toolchains",
			Script:    "bash " + shellQuote(toolchain.ScriptPath(s.cfg.DataDir)) + " --only " + strings.Join(toolchain.AllKeys(), ","),
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

// shellQuote 给路径加单引号，避免路径里有空格时被拆开。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
