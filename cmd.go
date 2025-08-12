package main

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:                "git-tags",
	Short:              "Manage git tags",
	Long:               "A tool to manage git tags with version bumping capabilities.",
	DisableSuggestions: true,
	DisableAutoGenTag:  true,
	Version:            "v1.0.0",
}

var lsCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "Show all tags",
	Run: func(cmd *cobra.Command, args []string) {
		listTags()
	},
}

var patchCmd = &cobra.Command{
	Use:   "patch",
	Short: "Increment patch version",
	Run: func(cmd *cobra.Command, args []string) {
		bumpVersion("patch")
	},
}

var minorCmd = &cobra.Command{
	Use:   "minor",
	Short: "Increment minor version",
	Run: func(cmd *cobra.Command, args []string) {
		bumpVersion("minor")
	},
}

var majorCmd = &cobra.Command{
	Use:   "major",
	Short: "Increment major version",
	Run: func(cmd *cobra.Command, args []string) {
		bumpVersion("major")
	},
}

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push tags to remote",
	Run: func(cmd *cobra.Command, args []string) {
		branch, _ := cmd.Flags().GetString("branch")
		pushTags(branch)
	},
}

var delCmd = &cobra.Command{
	Use:     "delate",
	Aliases: []string{"del"},
	Short:   "Delete the latest tag, remote and local",
	Run: func(cmd *cobra.Command, args []string) {
		branch, _ := cmd.Flags().GetString("branch")
		deleteLatestTag(branch)
	},
}
