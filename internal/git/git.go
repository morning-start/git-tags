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

// GetLatestTagOrEmpty 同 GetLatestTag，但无匹配 tag 时返回空串（不做 0.0.0 兜底），
// 用于区分「确实存在更早 tag」与「无 tag 可回滚」的场景。
func (r *Runner) GetLatestTagOrEmpty(prefix string) string {
	output, err := r.Run("tag", "-l", prefix+"*", "--sort=-v:refname")
	if err != nil {
		return ""
	}
	tags := strings.Split(output, "\n")
	if len(tags) > 0 && tags[0] != "" {
		return tags[0]
	}
	return ""
}

func (r *Runner) CreateTag(tag string) error {
	output, err := r.Run("tag", tag)
	if err != nil {
		return fmt.Errorf("error creating tag %s: %w\n%s", tag, err, output)
	}
	fmt.Printf("Created tag %s\n", tag)
	return nil
}

// PushTag 先推送当前分支的 commit（git push <branch> HEAD），再推送 tag：
// 保证远端存在 tag 指向的提交，避免出现只被 tag 引用、不被任何分支引用的
// 悬挂 commit。任一步失败即整体报错——本地分叉/落后（non-fast-forward）时
// 阻断推送，而不是把 tag 推上去但远端缺少它指向的提交。
func (r *Runner) PushTag(branch, tag string) error {
	output, err := r.Run("push", branch, "HEAD")
	if err != nil {
		return fmt.Errorf("error pushing commit: %w\n%s", err, output)
	}
	fmt.Println(output)

	output, err = r.Run("push", branch, tag)
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
		// 远端本就没有该 tag（可能尚未推送过）：正常现象，跳过远端删除
		// 不报错，继续执行本地删除与回滚。git 标准错误输出为
		// "remote ref does not exist"（并伴随 "unable to delete ..."）。
		if strings.Contains(output, "remote ref does not exist") || strings.Contains(output, "unable to delete") {
			fmt.Printf("remote %s tag %s does not exist, skipping remote delete\n", branch, tag)
			return nil
		}
		// 未配置该 remote（纯本地仓库）：跳过远端删除，不阻断本地删除与回滚
		if strings.Contains(output, "does not appear to be a git repository") {
			fmt.Printf("remote %s not configured, skipping remote delete\n", branch)
			return nil
		}
		return fmt.Errorf("error deleting remote tag %s: %w\n%s", tag, err, output)
	}
	fmt.Println(output)
	return nil
}

// HasUncommittedChanges 检查工作区是否有未提交改动（含未跟踪文件）。
// 用 git status --porcelain 判断：输出为空表示工作区干净。
func (r *Runner) HasUncommittedChanges() (bool, error) {
	output, err := r.Run("status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("error checking git status: %w\n%s", err, output)
	}
	return output != "", nil
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
