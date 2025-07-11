package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
)

const initialVersion = "v0.0.0"

var rootCmd = &cobra.Command{
	Use:                "git-tags",
	Short:              "Manage git tags",
	Long:               "A tool to manage git tags with version bumping capabilities.",
	DisableSuggestions: true,
	DisableAutoGenTag:  true,
}

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "Show all tags",
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
		pushTags()
	},
}

func init() {
	rootCmd.AddCommand(lsCmd)
	rootCmd.AddCommand(patchCmd)
	rootCmd.AddCommand(minorCmd)
	rootCmd.AddCommand(majorCmd)
	rootCmd.AddCommand(pushCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func listTags() {
	cmd := exec.Command("git", "tag")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing tags: %v\n", err)
		return
	}
	fmt.Println(strings.TrimSpace(string(output)))
}

func getLatestTag() string {
	cmd := exec.Command("git", "tag", "-l", "v*", "--sort=-v:refname")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return initialVersion
	}
	tags := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(tags) > 0 && tags[0] != "" {
		return tags[0]
	}
	return initialVersion
}

func bumpVersion(level string) {
	latestTag := getLatestTag()
	// 移除v前缀以解析版本号
	cleanedTag := strings.TrimPrefix(latestTag, "v")
	v, err := semver.NewVersion(cleanedTag)
	if err != nil {
		v, _ = semver.NewVersion(strings.TrimPrefix(initialVersion, "v"))
	}

	var newVersion semver.Version
	switch level {
	case "patch":
		newVersion = v.IncPatch()
	case "minor":
		newVersion = v.IncMinor()
	case "major":
		newVersion = v.IncMajor()
	}

	newTag := "v" + newVersion.String()
	cmd := exec.Command("git", "tag", newTag)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating tag: %v\n", err)
		fmt.Println(string(output))
		return
	}
	fmt.Printf("Created tag %s\n", newTag)
}

func pushTags() {
	latestTag := getLatestTag()
	cmd := exec.Command("git", "push", "origin", latestTag)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error pushing tag %s: %v\n", latestTag, err)
		fmt.Println(string(output))
		return
	}
	fmt.Println(string(output))
}
