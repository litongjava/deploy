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
		Short: "call the server doctor endpoint and show the environment report",
		Long: `Calls GET /api/v1/system/doctor and prints the server environment report.

The platform does not install toolchains; it only reports what is missing and how to install it.`,
		Args: exactArgs(0, "doctor takes no positional arguments"),
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
					{"client", "deploy-cli " + version.Version},
					{"server", app.Settings.Server},
					{"server version", fmt.Sprintf("%s (%s/%s, go %s)", orDash(doc.ServerVersion), orDash(doc.OS), orDash(doc.Arch), orDash(doc.GoVersion))},
					{"hostname", orDash(doc.Hostname)},
					{"uptime", strconv.FormatInt(doc.UptimeSec, 10) + " s"},
					{"workdir", fmt.Sprintf("%s  %s writable", orDash(doc.WebRoot), p.OK(doc.WebRootWritable))},
					{"data dir", fmt.Sprintf("%s  %s writable", orDash(doc.DataDir), p.OK(doc.DataDirWritable))},
					{"Shell", fmt.Sprintf("%s (%s) available: %s", orDash(doc.Shell.Selected), orDash(doc.Shell.Path), strings.Join(doc.Shell.Available, ", "))},
				}))

				p.Println()
				p.Println(p.Bold("environment report"))
				if len(doc.Checks) == 0 {
					p.Println("(the server returned no checks)")
					return
				}
				for _, c := range doc.Checks {
					line := fmt.Sprintf("  %s %-10s", p.OK(c.Found), c.Name)
					switch {
					case c.Found && c.Version != "":
						line += c.Version
					case c.Found:
						line += "installed"
					default:
						line += p.Red("not found")
					}
					if c.Path != "" {
						line += "  " + p.Dim(c.Path)
					}
					p.Println(line)
					if !c.Found && c.Hint != "" {
						p.Println("      " + p.Yellow("hint: "+c.Hint))
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
					p.Println(p.Green(fmt.Sprintf("all %d checks passed", len(doc.Checks))))
				} else {
					p.Println(p.Yellow(fmt.Sprintf("%d/%d missing; install them following the hints above", missing, len(doc.Checks))))
				}
			})
		},
	}
}
