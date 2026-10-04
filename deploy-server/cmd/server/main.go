// Command deploy-server 是轻量命令式 CI/CD 平台的服务端。
//
// 单二进制交付：HTTP API + WebSocket 实时日志 + 执行引擎 + 内嵌前端 + SQLite。
// 用法：
//
//	deploy-server -listen 0.0.0.0:10055 -data-dir /srv/deploy/data -web-root /srv/deploy/workspaces
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
	"deploy-server/internal/metrics"
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
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	setupLogger()

	slog.Info("starting deploy-server", "version", version, "listen", cfg.Listen)
	slog.Info("data directory", "dataDir", cfg.DataDir, "configFile", cfg.ConfigPath)

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		slog.Error("failed to open database", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	box, err := secret.New(cfg.SecretKey)
	if err != nil {
		slog.Error("failed to initialize crypto module", "err", err)
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
		slog.Warn("failed to recover interrupted runs", "err", err)
	} else if n > 0 {
		slog.Warn("marked runs left unfinished by the previous process as interrupted", "count", n)
	}

	srv := api.NewServer(cfg, st, box, exec, version)

	// 服务器资源采样（顶层菜单「服务器」页面用）。
	//
	// 在这里主动启动而不是等第一次请求：CPU 使用率是两次采样的差分，
	// 早启动 3 秒就能让用户第一次打开页面看到真实数字，
	// 而不是「采样中」。采样本身只是个后台 goroutine，开销可以忽略。
	metrics.Start(0)
	defer metrics.Stop()

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 20 * time.Second,
		// 不设 WriteTimeout：WebSocket 是长连接，写超时会把它掐掉。
		IdleTimeout: 120 * time.Second,
	}

	go func() {
		slog.Info("HTTP server ready", "url", cfg.ServerURL)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server exited", "err", err)
			os.Exit(1)
		}
	}()

	// 日志清理：启动跑一次，之后每 6 小时一次。
	stopCleanup := startCleanup(srv)

	// 优雅关闭：先停止接收新请求，再等正在跑的 Run 收尾。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("received shutdown signal, starting graceful shutdown")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		slog.Warn("timed out waiting for the HTTP server to shut down", "err", err)
	}
	stopCleanup()

	if !exec.Wait(60 * time.Second) {
		slog.Warn("some runs are still active; exiting anyway (they are marked interrupted on next start)")
	}
	slog.Info("exited")
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
				slog.Warn("failed to prune old runs", "err", err)
			} else if n > 0 {
				slog.Info("pruned expired runs", "count", n)
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
