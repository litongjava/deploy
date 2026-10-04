package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"deploy-cli/internal/output"
	"deploy-cli/internal/version"
)

func newDoctorCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "调用服务端自检接口，展示环境检查结果",
		Long: `调用 GET /api/v1/system/doctor，彩色展示服务端环境自检结果。

平台不负责安装工具链，只报告缺失项并给出安装提示。`,
		Args: exactArgs(0, "doctor 不接受位置参数"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := app.AuthClient()
			if err != nil {
				return err
			}
			ctx, cancel := app.ctx()
			defer cancel()

			doc, err := client.Doctor(ctx)
			if err != nil {
				return err
			}

			p := app.Printer
			return app.jsonOrHuman(doc, func() {
				p.String(output.KeyValue([][2]string{
					{"客户端", "deploy-cli " + version.Version},
					{"服务器", app.Settings.Server},
					{"服务端版本", fmt.Sprintf("%s（%s/%s，go %s）", orDash(doc.ServerVersion), orDash(doc.OS), orDash(doc.Arch), orDash(doc.GoVersion))},
					{"主机名", orDash(doc.Hostname)},
					{"已运行", strconv.FormatInt(doc.UptimeSec, 10) + " 秒"},
					{"工作目录", fmt.Sprintf("%s  %s 可写", orDash(doc.WebRoot), p.OK(doc.WebRootWritable))},
					{"数据目录", fmt.Sprintf("%s  %s 可写", orDash(doc.DataDir), p.OK(doc.DataDirWritable))},
					{"Shell", fmt.Sprintf("%s（%s）可用: %s", orDash(doc.Shell.Selected), orDash(doc.Shell.Path), strings.Join(doc.Shell.Available, ", "))},
				}))

				p.Println()
				p.Println(p.Bold("环境自检"))
				if len(doc.Checks) == 0 {
					p.Println("（服务端未返回自检项）")
					return
				}
				for _, c := range doc.Checks {
					line := fmt.Sprintf("  %s %-10s", p.OK(c.Found), c.Name)
					switch {
					case c.Found && c.Version != "":
						line += c.Version
					case c.Found:
						line += "已安装"
					default:
						line += p.Red("未找到")
					}
					if c.Path != "" {
						line += "  " + p.Dim(c.Path)
					}
					p.Println(line)
					if !c.Found && c.Hint != "" {
						p.Println("      " + p.Yellow("提示: "+c.Hint))
					}
				}
				missing := 0
				for _, c := range doc.Checks {
					if !c.Found {
						missing++
					}
				}
				p.Println()
				if missing == 0 {
					p.Println(p.Green(fmt.Sprintf("全部 %d 项检查通过", len(doc.Checks))))
				} else {
					p.Println(p.Yellow(fmt.Sprintf("%d/%d 项缺失，按上面的提示安装即可", missing, len(doc.Checks))))
				}
			})
		},
	}
}
