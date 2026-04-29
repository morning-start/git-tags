package cmd

import (
	"github.com/spf13/cobra"
	"git-tags/internal/tag"
)

var tagManager = tag.NewManager()

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
	Run: func(cmd *cobra.Command, args []string) {
		tagManager.ListTags()
	},
}

var PatchCmd = &cobra.Command{
	Use:   "patch",
	Short: "Increment patch version",
	Run: func(cmd *cobra.Command, args []string) {
		push, _ := cmd.Flags().GetBool("push")
		tagManager.BumpVersion("patch", push)
	},
}

var MinorCmd = &cobra.Command{
	Use:   "minor",
	Short: "Increment minor version",
	Run: func(cmd *cobra.Command, args []string) {
		push, _ := cmd.Flags().GetBool("push")
		tagManager.BumpVersion("minor", push)
	},
}

var MajorCmd = &cobra.Command{
	Use:   "major",
	Short: "Increment major version",
	Run: func(cmd *cobra.Command, args []string) {
		push, _ := cmd.Flags().GetBool("push")
		tagManager.BumpVersion("major", push)
	},
}

var PushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push tags to remote",
	Run: func(cmd *cobra.Command, args []string) {
		branch, _ := cmd.Flags().GetString("branch")
		tagManager.PushTag(branch)
	},
}

var DeleteCmd = &cobra.Command{
	Use:     "delete",
	Aliases: []string{"del"},
	Short:   "(del) Delete the latest tag, remote and local",
	Run: func(cmd *cobra.Command, args []string) {
		branch, _ := cmd.Flags().GetString("branch")
		tagManager.DeleteLatestTag(branch)
	},
}

func init() {
	PushCmd.Flags().StringP("branch", "b", "origin", "Specify the branch to push tags to")
	DeleteCmd.Flags().StringP("branch", "b", "origin", "Specify the remote branch to delete tags")
	PatchCmd.Flags().BoolP("push", "p", false, "Push tag to remote after creating")
	MinorCmd.Flags().BoolP("push", "p", false, "Push tag to remote after creating")
	MajorCmd.Flags().BoolP("push", "p", false, "Push tag to remote after creating")

	RootCmd.AddCommand(ListCmd, PatchCmd, MinorCmd, MajorCmd, PushCmd, DeleteCmd)
}
