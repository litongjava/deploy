package cmd

import (
	"github.com/spf13/cobra"

	"deploy-cli/internal/exitcode"
)

func newProjectCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "project",
		Aliases: []string{"projects"},
		Short:   "project operations (list / get / create)",
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return exitcode.Usagef("unknown subcommand %q; available: list, get, create", args[0])
			}
			return cmd.Help()
		},
	}
	cmd.AddCommand(newProjectListCmd(app), newProjectGetCmd(app), newProjectCreateCmd(app), newProjectUpdateCmd(app))
	return cmd
}
