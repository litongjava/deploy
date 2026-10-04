// Package version 保存由 -ldflags -X 注入的构建信息。
package version

import (
	"fmt"
	"runtime"
)

// 通过 -ldflags "-X deploy-cli/internal/version.Version=..." 注入。
var (
	Version   = "dev"
	Commit    = "none"
	BuildTime = "unknown"
)

// String 返回单行版本信息。
func String() string {
	return fmt.Sprintf("%s (commit %s, built %s, %s/%s, %s)",
		Version, Commit, BuildTime, runtime.GOOS, runtime.GOARCH, runtime.Version())
}

// LdflagsArgs 供 Makefile / 构建脚本参考的完整包路径。
const LdflagsPkg = "deploy-cli/internal/version"
