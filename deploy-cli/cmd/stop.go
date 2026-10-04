package cmd

import (
	"github.com/spf13/cobra"
)

func newStopCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:     "stop <runId>",
		Aliases: []string{"cancel"},
		Short:   "取消正在运行的任务",
		Long: `调用 POST /runs/{id}/cancel 取消运行。

接口是幂等的：运行已结束或已取消时同样返回成功。`,
		Args: exactArgs(1, "用法：deploy stop <runId>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID, err := parseRunID(args[0])
			if err != nil {
				return err
			}
			client, err := app.AuthClient()
			if err != nil {
				return err
			}
			ctx, cancel := app.ctx()
			defer cancel()

			if err := client.CancelRun(ctx, runID); err != nil {
				return err
			}
			p := app.Printer
			return app.jsonOrHuman(map[string]any{"runId": runID, "canceled": true}, func() {
				p.Printf("已请求取消运行 #%d\n", runID)
			})
		},
	}
}
