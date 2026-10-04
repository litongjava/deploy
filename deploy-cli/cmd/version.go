package cmd

import (
	"runtime"

	"github.com/spf13/cobra"

	"deploy-cli/internal/version"
)

func newVersionCmd(app *App) *cobra.Command {
	var short bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "打印版本信息",
		Args:  exactArgs(0, "version 不接受位置参数"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if app.Printer.JSON {
				return app.Printer.JSONOut(map[string]string{
					"version":   version.Version,
					"commit":    version.Commit,
					"buildTime": version.BuildTime,
					"go":        runtime.Version(),
					"os":        runtime.GOOS,
					"arch":      runtime.GOARCH,
				})
			}
			if short {
				app.Printer.Println(version.Version)
				return nil
			}
			app.Printer.Println("deploy " + version.String())
			return nil
		},
	}
	cmd.Flags().BoolVar(&short, "short", false, "只输出版本号")
	return cmd
}
