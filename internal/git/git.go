package git

import (
	"fmt"
	"os/exec"
	"strings"
)

type Runner struct{}

func NewRunner() *Runner {
	return &Runner{}
}

func (r *Runner) Run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func (r *Runner) ListTags() (string, error) {
	return r.Run("tag")
}

func (r *Runner) GetLatestTag() string {
	output, err := r.Run("tag", "-l", "v*", "--sort=-v:refname")
	if err != nil {
		return "v0.0.0"
	}
	tags := strings.Split(output, "\n")
	if len(tags) > 0 && tags[0] != "" {
		return tags[0]
	}
	return "v0.0.0"
}

func (r *Runner) CreateTag(tag string) error {
	output, err := r.Run("tag", tag)
	if err != nil {
		return fmt.Errorf("error creating tag %s: %w\n%s", tag, err, output)
	}
	fmt.Printf("Created tag %s\n", tag)
	return nil
}

func (r *Runner) PushTag(branch, tag string) error {
	output, err := r.Run("push", branch, tag)
	if err != nil {
		return fmt.Errorf("error pushing tag %s: %w\n%s", tag, err, output)
	}
	fmt.Println(output)
	return nil
}

func (r *Runner) DeleteLocalTag(tag string) error {
	output, err := r.Run("tag", "-d", tag)
	if err != nil {
		return fmt.Errorf("error deleting local tag %s: %w\n%s", tag, err, output)
	}
	fmt.Println(output)
	return nil
}

func (r *Runner) DeleteRemoteTag(branch, tag string) error {
	output, err := r.Run("push", branch, "--delete", tag)
	if err != nil {
		if strings.Contains(output, "exit status 1") {
			fmt.Printf("remote %s tag does not exist\n", tag)
			return nil
		}
		return fmt.Errorf("error deleting remote tag %s: %w\n%s", tag, err, output)
	}
	fmt.Println(output)
	return nil
}
