// Command deploy 是轻量命令式 CI/CD 平台的命令行客户端。
//
// 退出码语义（脚本依赖，勿改）：
//
//	0  成功
//	1  部署失败（run 状态为 failed / timeout / error / canceled）
//	2  用法错误（参数不合法、命令不存在、本地配置损坏）
//	3  鉴权失败（401 / token 无效或过期）
//	4  网络或服务不可用（连接被拒、超时、5xx）
package main

import (
	"errors"
	"fmt"
	"os"

	"deploy-cli/cmd"
	"deploy-cli/internal/exitcode"
)

func main() {
	root := cmd.NewRootCmd()
	err := root.Execute()
	if err == nil {
		os.Exit(exitcode.OK)
	}

	code := exitcode.Of(err)

	// 运行结果类失败已经把细节打到 stdout/stderr，这里不重复刷屏。
	var silent *exitcode.Error
	if errors.As(err, &silent) && silent.Silent() {
		os.Exit(code)
	}

	// 人类可读的错误一律走 stderr：--json 时 stdout 保持纯净。
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	if hint := hintFor(code); hint != "" {
		fmt.Fprintf(os.Stderr, "hint: %s\n", hint)
	}
	os.Exit(code)
}

func hintFor(code int) string {
	switch code {
	case exitcode.Auth:
		return "not signed in, or the token has expired: run `deploy login` again, or `deploy whoami` to check"
	case exitcode.Network:
		return "check that the server is running and the address is right (`deploy config get address`)"
	case exitcode.Usage:
		return "run `deploy --help` or `deploy <command> --help` for usage"
	default:
		return ""
	}
}
