package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"git-tags/internal/lua"
)

var PluginsCmd = &cobra.Command{
	Use:   "plugins",
	Short: "Manage Lua plugins",
}

var PluginsListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List discovered Lua plugins",
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, p := range plugins {
			status := "ok"
			if p.Err != nil {
				status = "error: " + p.Err.Error()
			}
			fmt.Printf("%-15s %-8s %-7s %3d  %s\n", p.Name, p.Kind, p.Source, p.Priority, status)
		}
		// 内嵌 provider（未被用户插件覆盖的）
		userNames := map[string]bool{}
		for _, p := range plugins {
			if p.Err == nil && p.Kind == "provider" {
				userNames[p.Name] = true
			}
		}
		for name, content := range embeddedProviders {
			if userNames[name] {
				continue
			}
			status, prio := "ok", 50
			if ep, err := lua.LoadEmbeddedProvider(name, content, lua.NewRunner(absRoot, gitProv.LatestTag)); err != nil {
				status = "error: " + err.Error()
			} else {
				prio = ep.Priority()
			}
			fmt.Printf("%-15s %-8s %-7s %3d  %s\n", name, "provider", "embedded", prio, status)
		}
		return nil
	},
}

var PluginsValidateCmd = &cobra.Command{
	Use:   "validate [path...]",
	Short: "Validate Lua plugin scripts",
	Long:  "Check syntax, manifest and required functions. Without arguments, validates all discovered plugins.",
	RunE: func(cmd *cobra.Command, args []string) error {
		var paths []string
		if len(args) > 0 {
			paths = args
		} else {
			for _, p := range plugins {
				paths = append(paths, p.Path)
			}
		}
		failed := false
		for _, path := range paths {
			if err := lua.Validate(path); err != nil {
				failed = true
				fmt.Printf("✗ %s: %v\n", path, err)
			} else {
				fmt.Printf("✓ %s\n", path)
			}
		}
		if failed {
			return fmt.Errorf("存在校验失败的插件")
		}
		return nil
	},
}

func init() {
	PluginsCmd.GroupID = "plugin"
	PluginsCmd.AddCommand(PluginsListCmd, PluginsValidateCmd)
	RootCmd.AddCommand(PluginsCmd)
}
