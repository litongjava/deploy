package cmd

import (
	"github.com/spf13/cobra"

	"deploy-cli/internal/exitcode"
)

func newProjectCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "project",
		Aliases: []string{"projects"},
		Short:   "项目相关操作（list / get / create）",
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return exitcode.Usagef("未知子命令 %q，可用：list、get、create", args[0])
			}
			return cmd.Help()
		},
	}
	cmd.AddCommand(newProjectListCmd(app), newProjectGetCmd(app), newProjectCreateCmd(app))
	return cmd
}
