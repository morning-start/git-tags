package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// Runner 执行 git 命令；可通过 SetDir 指定工作目录（项目根）。
type Runner struct {
	dir string
}

func NewRunner() *Runner {
	return &Runner{}
}

// SetDir 设置 git 命令的工作目录（通常为项目根）。
func (r *Runner) SetDir(dir string) { r.dir = dir }

func (r *Runner) Run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func (r *Runner) ListTags() (string, error) {
	return r.Run("tag")
}

func (r *Runner) GetLatestTag(prefix string) string {
	output, err := r.Run("tag", "-l", prefix+"*", "--sort=-v:refname")
	if err != nil {
		return prefix + "0.0.0"
	}
	tags := strings.Split(output, "\n")
	if len(tags) > 0 && tags[0] != "" {
		return tags[0]
	}
	return prefix + "0.0.0"
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

// CommitVersionChange 暂存全部改动并提交；无暂存内容时静默跳过（如 --commit
// 但版本文件没有实际变化的情况）。
func (r *Runner) CommitVersionChange(message string) error {
	if _, err := r.Run("add", "-A"); err != nil {
		return err
	}
	// git diff --cached --quiet 退出码 0 表示无暂存变更 → 跳过提交
	if _, err := r.Run("diff", "--cached", "--quiet"); err == nil {
		return nil
	}
	output, err := r.Run("commit", "-m", message)
	if err != nil {
		return fmt.Errorf("error committing version change: %w\n%s", err, output)
	}
	fmt.Println(output)
	return nil
}
