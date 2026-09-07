package provider

import (
	"strings"

	"git-tags/internal/git"
)

// GitProvider 以 git tag 为版本源，是版本同步的权威源（canonical）。
type GitProvider struct {
	runner *git.Runner
	prefix string // tag 前缀，默认 "v"
}

// NewGitProvider 创建 git provider，prefix 为 tag 前缀（空则用 "v"）。
func NewGitProvider(prefix string) *GitProvider {
	if prefix == "" {
		prefix = "v"
	}
	return &GitProvider{runner: git.NewRunner(), prefix: prefix}
}

func (p *GitProvider) Name() string  { return "git" }
func (p *GitProvider) Priority() int { return 100 }

// Detect 恒为 true：git 仓库中的 tag 始终是权威版本源。
func (p *GitProvider) Detect(ctx *Context) bool { return true }

// Read 返回最新 tag 去掉前缀后的版本号，如 "v1.2.3" → "1.2.3"。
func (p *GitProvider) Read(ctx *Context) (string, error) {
	tag := p.runner.GetLatestTag(p.prefix)
	return strings.TrimPrefix(tag, p.prefix), nil
}

// Write 不直接写文件：tag 的创建由引擎在 bump/set 流程中统一处理。
func (p *GitProvider) Write(ctx *Context, version string) error { return nil }

// Targets 无文件位置：git tag 是权威源而非同步目标。
func (p *GitProvider) Targets() []Target { return nil }

// TagFor 根据版本号生成带前缀的 tag 名。
func (p *GitProvider) TagFor(version string) string { return p.prefix + version }

// ---- 供命令层使用的 git 操作透传 ----

// ListTags 列出所有 tag。
func (p *GitProvider) ListTags() (string, error) { return p.runner.ListTags() }

// LatestTag 返回最新 tag 名（带前缀），无 tag 时返回 prefix+"0.0.0"。
func (p *GitProvider) LatestTag() string { return p.runner.GetLatestTag(p.prefix) }

// CreateTag 创建新 tag。
func (p *GitProvider) CreateTag(tag string) error { return p.runner.CreateTag(tag) }

// PushTag 推送 tag 到远程分支。
func (p *GitProvider) PushTag(branch, tag string) error { return p.runner.PushTag(branch, tag) }

// DeleteLocalTag 删除本地 tag。
func (p *GitProvider) DeleteLocalTag(tag string) error { return p.runner.DeleteLocalTag(tag) }

// DeleteRemoteTag 删除远程 tag。
func (p *GitProvider) DeleteRemoteTag(branch, tag string) error {
	return p.runner.DeleteRemoteTag(branch, tag)
}

// CommitVersionChange 暂存全部改动并提交（无暂存内容时静默跳过）。
func (p *GitProvider) CommitVersionChange(message string) error {
	return p.runner.CommitVersionChange(message)
}
