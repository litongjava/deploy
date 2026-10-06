package cmd

import (
	"github.com/spf13/cobra"
)

func newStopCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:     "stop <runId>",
		Aliases: []string{"cancel"},
		Short:   "cancel a running task",
		Long: `Calls POST /runs/{id}/cancel to cancel a run.

The endpoint is idempotent: it also succeeds when the run already finished or was cancelled.`,
		Args: exactArgs(1, "usage: deploy stop <runId>"),
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
				p.Printf("requested cancellation of run #%d\n", runID)
			})
		},
	}
}
