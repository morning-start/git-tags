package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"git-tags/internal/config"
	"git-tags/internal/core"
	"git-tags/internal/provider"
)

// loadConfig 加载 .git-tags.toml；解析失败时直接退出（配置错误不应静默忽略）。
func loadConfig() *config.Config {
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置错误: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

var (
	cfg    = loadConfig()
	engine = core.New(cfg,
		provider.NewTauri(),
		provider.NewFlutter(),
		provider.NewUV(),
		provider.NewNode(),
	)
)

// newContext 构造 provider 执行上下文：项目根为当前目录，日志输出到 stdout。
func newContext() *provider.Context {
	return &provider.Context{
		Project: &provider.Project{Root: "."},
		Log:     func(format string, args ...any) { fmt.Printf(format+"\n", args...) },
	}
}

var RootCmd = &cobra.Command{
	Use:               "git-tags",
	Short:             "Manage git tags",
	Long:              "A tool to manage git tags with version bumping capabilities.",
	CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	Version:           "v1.0.0",
}

var ListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "(ls) Show all tags",
	RunE: func(cmd *cobra.Command, args []string) error {
		return engine.ListTags()
	},
}

func newBumpCmd(name, level string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: fmt.Sprintf("Increment %s version", level),
		RunE: func(cmd *cobra.Command, args []string) error {
			push, _ := cmd.Flags().GetBool("push")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			commit, _ := cmd.Flags().GetBool("commit")
			noTag, _ := cmd.Flags().GetBool("no-tag")
			return engine.Bump(newContext(), level, core.Options{
				Push: push, DryRun: dryRun, Commit: commit, NoTag: noTag,
			})
		},
	}
}

var PatchCmd = newBumpCmd("patch", "patch")
var MinorCmd = newBumpCmd("minor", "minor")
var MajorCmd = newBumpCmd("major", "major")

var PushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push tags to remote",
	RunE: func(cmd *cobra.Command, args []string) error {
		branch, _ := cmd.Flags().GetString("branch")
		return engine.PushTag(branch)
	},
}

var CheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check version consistency across project providers",
	Long:  "Compare every active provider's version with the canonical git tag and report inconsistencies.",
	RunE: func(cmd *cobra.Command, args []string) error {
		items, err := engine.Check(newContext())
		if err != nil {
			return err
		}
		inSync := true
		for _, it := range items {
			status := "✓"
			if !it.InSync {
				status = "✗"
				inSync = false
			}
			fmt.Printf("%-10s %-12s %s\n", it.Provider, it.Version, status)
		}
		if !inSync {
			return fmt.Errorf("版本不同步：请运行 git-tags sync 以权威源版本写回各 provider")
		}
		return nil
	},
}

var SyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync canonical git tag version to all providers",
	Long:  "Write the canonical version (latest git tag) to every writable target of all active providers.",
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		return engine.Sync(newContext(), dryRun)
	},
}

var SetCmd = &cobra.Command{
	Use:   "set <version>",
	Short: "Set the project version explicitly",
	Long:  "Set a new version, sync it to all providers, and optionally create a tag.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		commit, _ := cmd.Flags().GetBool("commit")
		noTag, _ := cmd.Flags().GetBool("no-tag")
		push, _ := cmd.Flags().GetBool("push")
		return engine.Set(newContext(), args[0], core.Options{
			DryRun: dryRun, Commit: commit, NoTag: noTag, Push: push,
		})
	},
}

var DeleteCmd = &cobra.Command{
	Use:     "delete",
	Aliases: []string{"del"},
	Short:   "(del) Delete the latest tag, remote and local",
	RunE: func(cmd *cobra.Command, args []string) error {
		branch, _ := cmd.Flags().GetString("branch")
		return engine.DeleteLatestTag(branch)
	},
}

func init() {
	PushCmd.Flags().StringP("branch", "b", "origin", "Specify the branch to push tags to")
	DeleteCmd.Flags().StringP("branch", "b", "origin", "Specify the remote branch to delete tags")
	SyncCmd.Flags().Bool("dry-run", false, "Preview changes without applying them")
	SetCmd.Flags().BoolP("push", "p", false, "Push tag to remote after creating")
	SetCmd.Flags().Bool("dry-run", false, "Preview changes without applying them")
	SetCmd.Flags().Bool("commit", false, "Commit version changes together with the tag")
	SetCmd.Flags().Bool("no-tag", false, "Only update version files, do not create a tag")
	for _, c := range []*cobra.Command{PatchCmd, MinorCmd, MajorCmd} {
		c.Flags().BoolP("push", "p", false, "Push tag to remote after creating")
		c.Flags().Bool("dry-run", false, "Preview changes without applying them")
		c.Flags().Bool("commit", false, "Commit version changes together with the tag")
		c.Flags().Bool("no-tag", false, "Only update version files, do not create a tag")
	}

	RootCmd.AddCommand(ListCmd, PatchCmd, MinorCmd, MajorCmd, PushCmd, DeleteCmd, CheckCmd, SyncCmd, SetCmd)
}
