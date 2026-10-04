// Command deploy-server 是轻量命令式 CI/CD 平台的服务端。
//
// 单二进制交付：HTTP API + WebSocket 实时日志 + 执行引擎 + 内嵌前端 + SQLite。
// 用法：
//
//	deploy-server -listen 0.0.0.0:8080 -data-dir /srv/deploy/data -web-root /srv/deploy/workspaces
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"deploy-server/internal/api"
	"deploy-server/internal/config"
	"deploy-server/internal/runner"
	"deploy-server/internal/secret"
	"deploy-server/internal/store"
)

// version 由构建脚本用 -ldflags "-X main.version=..." 注入。
var version = "0.1.0-dev"

func main() {
	// 版本查询要能在加载配置之前就用（打包脚本、容器探针会调它）。
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-v", "--version", "version":
			fmt.Printf("deploy-server %s\n", version)
			return
		}
	}

	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}

	setupLogger()

	slog.Info("启动 deploy-server", "version", version, "listen", cfg.Listen)
	slog.Info("数据目录", "dataDir", cfg.DataDir, "configFile", cfg.ConfigPath)

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		slog.Error("打开数据库失败", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	box, err := secret.New(cfg.SecretKey)
	if err != nil {
		slog.Error("初始化加密模块失败", "err", err)
		os.Exit(1)
	}

	exec := runner.New(cfg, st, box)
	// 让执行器读到运行时可变的设置（日志上限），不用重启就能生效。
	exec.SetSettingsIntReader(func(key string, def int) int {
		if v, ok := st.GetSetting(key); ok {
			if n, err := strconv.Atoi(v); err == nil {
				return n
			}
		}
		return def
	})
	// 把上一个进程留下的 running/queued 标记为中断。
	// 不这样做的话，重启后界面上会一直挂着一堆「运行中」，但进程早就没了。
	if n, err := exec.RecoverInterrupted(); err != nil {
		slog.Warn("恢复中断的运行记录失败", "err", err)
	} else if n > 0 {
		slog.Warn("已把服务重启前未结束的运行标记为中断", "count", n)
	}

	srv := api.NewServer(cfg, st, box, exec, version)

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 20 * time.Second,
		// 不设 WriteTimeout：WebSocket 是长连接，写超时会把它掐掉。
		IdleTimeout: 120 * time.Second,
	}

	go func() {
		slog.Info("HTTP 服务已就绪", "url", cfg.ServerURL)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP 服务退出", "err", err)
			os.Exit(1)
		}
	}()

	// 日志清理：启动跑一次，之后每 6 小时一次。
	stopCleanup := startCleanup(srv)

	// 优雅关闭：先停止接收新请求，再等正在跑的 Run 收尾。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("收到退出信号，开始优雅关闭")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		slog.Warn("HTTP 关闭超时", "err", err)
	}
	stopCleanup()

	if !exec.Wait(60 * time.Second) {
		slog.Warn("仍有运行未结束，进程即将退出（下次启动会被标记为中断）")
	}
	slog.Info("已退出")
}

func setupLogger() {
	level := slog.LevelInfo
	if os.Getenv("DEPLOY_LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h))
}

// startCleanup 起一个后台协程按保留天数清理历史运行与日志，返回停止函数。
func startCleanup(srv *api.Server) func() {
	stop := make(chan struct{})
	go func() {
		run := func() {
			if n, err := srv.CleanupOldRuns(); err != nil {
				slog.Warn("清理历史运行失败", "err", err)
			} else if n > 0 {
				slog.Info("已清理过期运行记录", "count", n)
			}
		}
		run()
		t := time.NewTicker(6 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				run()
			}
		}
	}()
	return func() { close(stop) }
}
