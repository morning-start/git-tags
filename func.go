package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/pterm/pterm"
)

const initialVersion = "v0.0.0"

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

func pushTags(branch string) {
	latestTag := getLatestTag()
	// spinner
	spinner, _ := pterm.DefaultSpinner.Start("Pushing tags...")
	cmd := exec.Command("git", "push", branch, latestTag)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error pushing tag %s: %v\n", latestTag, err)
		fmt.Println(string(output))
		return
	}
	spinner.Success("Pushed tags")
	fmt.Println(string(output))
}

func deleteLatestTag(branch string) {
	latestTag := getLatestTag()
	cmd := exec.Command("git", "push", branch, "--delete", latestTag)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting tag %s: %v\n", latestTag, err)
		return
	}
	fmt.Println(string(output))

	// Implement local latest tag deletion logic here
	cmd_l := exec.Command("git", "tag", "-d", latestTag)
	output_l, err_l := cmd_l.CombinedOutput()
	if err_l != nil {
		fmt.Fprintf(os.Stderr, "Error deleting tag %s: %v\n", latestTag, err)
		return
	}
	fmt.Println(string(output_l))
}
