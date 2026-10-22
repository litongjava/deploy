package cmd

import (
	"deploy-cli/internal/exitcode"
	"encoding/json"
	"github.com/spf13/cobra"
	"os"
	"strings"
)

func validateProjectDependencies(key, kind string, dependencies []string) error {
	if kind != "" && kind != "app" && kind != "lib" {
		return exitcode.Usagef("--kind must be app or lib")
	}
	for _, dependency := range dependencies {
		if !projectKeyRE.MatchString(dependency) || dependency == key {
			return exitcode.Usagef("invalid dependency %q", dependency)
		}
	}
	return nil
}

func newProjectUpdateCmd(app *App) *cobra.Command {
	var file, kind string
	var dependencies []string
	cmd := &cobra.Command{
		Use: "update <key>", Short: "update supplied project fields, preserving omitted fields",
		Args: exactArgs(1, "usage: deploy project update <key> [--file definition.json] [--kind app|lib] [--dependencies key,...]"),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := strings.TrimSpace(args[0])
			if !projectKeyRE.MatchString(key) {
				return exitcode.Usagef("invalid project key %q", key)
			}
			in := map[string]any{}
			if file != "" {
				pf, base, err := loadProjectFile(file)
				if err != nil {
					return err
				}
				raw, err := os.ReadFile(file)
				if err != nil {
					return err
				}
				if err = json.Unmarshal(raw, &in); err != nil {
					return err
				}
				if in == nil {
					return exitcode.Usagef("project definition must be an object")
				}
				if pf.Key != "" && pf.Key != key {
					return exitcode.Usagef("file key does not match target project")
				}
				delete(in, "key")
				if _, ok := in["commands"]; ok {
					commands, err := pf.commandInputs(base, "abort")
					if err != nil {
						return err
					}
					in["commands"] = commands
				}
				if _, ok := in["env"]; ok {
					return exitcode.Usagef("update requires envs entries instead of env shorthand")
				}
				kind = pf.Kind
				if !cmd.Flags().Changed("dependencies") {
					dependencies = pf.Dependencies
				}
			}
			if cmd.Flags().Changed("kind") {
				kind, _ = cmd.Flags().GetString("kind")
				in["kind"] = kind
			}
			if cmd.Flags().Changed("dependencies") {
				if dependencies == nil {
					dependencies = []string{}
				}
				in["dependencies"] = dependencies
			}
			if err := validateProjectDependencies(key, kind, dependencies); err != nil {
				return err
			}
			if len(in) == 0 {
				return exitcode.Usagef("supply --file, --kind or --dependencies")
			}
			client, err := app.AuthClient()
			if err != nil {
				return err
			}
			ctx, cancel := app.ctx()
			defer cancel()
			project, err := client.UpdateProject(ctx, key, in)
			if err != nil {
				return err
			}
			return app.jsonOrHuman(project, func() { app.Printer.Printf("updated project %s\n", project.Key) })
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "JSON fields to update; omitted fields remain unchanged")
	cmd.Flags().StringVar(&kind, "kind", "", "project type: app or lib")
	cmd.Flags().StringSliceVar(&dependencies, "dependencies", nil, "replace dependencies; use --dependencies= to clear")
	return cmd
}
