package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Masterminds/semver/v3"
)

const initialVersion = "0.0.0"

func main() {
	if len(os.Args) < 2 {
		showHelp()
		return
	}

	cmd := os.Args[1]

	switch cmd {
	case "ls":
		listTags()
	case "patch":
		bumpVersion("patch")
	case "minor":
		bumpVersion("minor")
	case "major":
		bumpVersion("major")
	case "push":
		pushTags()
	case "--help", "-h":
		showHelp()
	default:
		showHelp()
	}
}

func listTags() {
	cmd := exec.Command("git", "tag")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing tags: %v\n", err)
		return
	}
	fmt.Println(string(output))
}

func getLatestTag() string {
	cmd := exec.Command("git", "tag", "--sort=-v:refname")
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
	v, err := semver.NewVersion(latestTag)
	if err != nil {
		v, _ = semver.NewVersion(initialVersion)
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

	newTag := newVersion.String()
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
	cmd := exec.Command("git", "push", "--tags")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error pushing tags: %v\n", err)
		fmt.Println(string(output))
		return
	}
	fmt.Println(string(output))
}

func showHelp() {
	fmt.Println(`Usage: git-tags [command]

Commands:
  ls		Show all tags
  patch		Increment patch version
  minor		Increment minor version
  major		Increment major version
  push		Push tags to remote
  --help, -h	Show this help message`)
}
