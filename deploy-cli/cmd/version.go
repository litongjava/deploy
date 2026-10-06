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
		Short: "print version information",
		Args:  exactArgs(0, "version takes no positional arguments"),
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
	cmd.Flags().BoolVar(&short, "short", false, "print the version number only")
	return cmd
}
