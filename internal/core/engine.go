// Package core 实现版本同步引擎：编排所有激活的 provider，执行
// detect → read → check → compute → write → tag → commit → push 流程。
// git tag 是唯一权威源：bump 的新版本由它计算，其他 provider 只做同步。
package core

import (
	"fmt"
	"sort"

	"github.com/Masterminds/semver/v3"

	"git-tags/internal/config"
	"git-tags/internal/provider"
)

// Options 控制一次 bump/set 的行为。
type Options struct {
	DryRun bool   // 只预览，不产生任何改动
	Commit bool   // 版本改动与 tag 自动提交到同一 commit
	NoTag  bool   // 只改文件，不创建 tag
	Push   bool   // 创建 tag 后推送到远程
	Branch string // push 目标分支，默认 "origin"
}

// CheckItem 是 check 报告中的一行。
type CheckItem struct {
	Provider string
	Version  string
	InSync   bool
}

// Engine 编排所有 provider。
type Engine struct {
	cfg       *config.Config
	git       *provider.GitProvider
	providers []provider.Provider
}

// New 创建引擎；extras 为额外注册的 provider（内置项目类型、Lua 插件等）。
func New(cfg *config.Config, extras ...provider.Provider) *Engine {
	e := &Engine{cfg: cfg}
	e.git = provider.NewGitProvider(cfg.TagPrefix)
	all := append([]provider.Provider{e.git}, extras...)
	sort.SliceStable(all, func(i, j int) bool { return all[i].Priority() > all[j].Priority() })
	e.providers = all
	return e
}

// Git 返回权威源 provider，供命令层调用 git 操作。
func (e *Engine) Git() *provider.GitProvider { return e.git }

// activeProviders 返回配置启用且 detect 通过的 provider。
func (e *Engine) activeProviders(ctx *provider.Context) []provider.Provider {
	var out []provider.Provider
	for _, p := range e.providers {
		if e.cfg.ProviderEnabled(p.Name()) && p.Detect(ctx) {
			out = append(out, p)
		}
	}
	return out
}

// ActiveProviders 返回当前激活的 provider（不含被禁用的）。
func (e *Engine) ActiveProviders(ctx *provider.Context) []provider.Provider {
	return e.activeProviders(ctx)
}

// ListTags 列出所有 tag。
func (e *Engine) ListTags() error {
	out, err := e.git.ListTags()
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

// LatestTag 返回最新 tag 名（带前缀）。
func (e *Engine) LatestTag() string { return e.git.LatestTag() }

// PushTag 推送最新 tag 到远程分支。
func (e *Engine) PushTag(branch string) error {
	return e.git.PushTag(branch, e.git.LatestTag())
}

// DeleteLatestTag 删除本地与远程最新 tag。
func (e *Engine) DeleteLatestTag(branch string) error {
	tag := e.git.LatestTag()
	if err := e.git.DeleteLocalTag(tag); err != nil {
		return err
	}
	return e.git.DeleteRemoteTag(branch, tag)
}

// Check 读取所有激活 provider 的版本并与权威源 git tag 比对，返回一致性报告。
func (e *Engine) Check(ctx *provider.Context) ([]CheckItem, error) {
	canonical, err := e.git.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取权威版本失败: %w", err)
	}
	items := []CheckItem{{Provider: "git", Version: canonical, InSync: true}}
	for _, p := range e.activeProviders(ctx) {
		if p == e.git {
			continue
		}
		v, err := p.Read(ctx)
		if err != nil {
			return nil, fmt.Errorf("读取 %s 版本失败: %w", p.Name(), err)
		}
		items = append(items, CheckItem{Provider: p.Name(), Version: v, InSync: v == canonical})
	}
	return items, nil
}

// EnsureInSync 校验所有激活 provider 与权威源一致；不一致时返回错误并提示 sync。
func (e *Engine) EnsureInSync(ctx *provider.Context) error {
	canonical, err := e.git.Read(ctx)
	if err != nil {
		return err
	}
	for _, p := range e.activeProviders(ctx) {
		if p == e.git {
			continue
		}
		v, err := p.Read(ctx)
		if err != nil {
			return fmt.Errorf("读取 %s 版本失败: %w", p.Name(), err)
		}
		if v != canonical {
			return fmt.Errorf("版本不同步：%s = %s，权威源 git tag = %s；请先运行 git-tags sync", p.Name(), v, canonical)
		}
	}
	return nil
}

// Sync 以权威源版本写回所有激活 provider 的可写 target。
func (e *Engine) Sync(ctx *provider.Context, dryRun bool) error {
	canonical, err := e.git.Read(ctx)
	if err != nil {
		return err
	}
	for _, p := range e.activeProviders(ctx) {
		if p == e.git {
			continue
		}
		if !e.cfg.ProviderWritable(p.Name()) {
			ctx.Logf("%s: 配置为只读，跳过写入", p.Name())
			continue
		}
		if dryRun {
			ctx.Logf("[dry-run] %s: 将写入版本 %s", p.Name(), canonical)
			continue
		}
		if err := p.Write(ctx, canonical); err != nil {
			return fmt.Errorf("写入 %s 失败: %w", p.Name(), err)
		}
		ctx.Logf("%s: 已同步到 %s", p.Name(), canonical)
	}
	return nil
}

// Bump 按 level（patch/minor/major）递增版本：先校验一致性，再写回所有
// provider、创建 tag，可选提交与推送。
func (e *Engine) Bump(ctx *provider.Context, level string, opts Options) error {
	canonical, err := e.git.Read(ctx)
	if err != nil {
		return err
	}
	cur, err := semver.NewVersion(canonical)
	if err != nil {
		return fmt.Errorf("解析当前版本 %q 失败: %w", canonical, err)
	}

	var next semver.Version
	switch level {
	case "patch":
		next = cur.IncPatch()
	case "minor":
		next = cur.IncMinor()
	case "major":
		next = cur.IncMajor()
	default:
		return fmt.Errorf("invalid version level: %s", level)
	}
	newVersion := next.String()
	tag := e.git.TagFor(newVersion)

	if err := e.EnsureInSync(ctx); err != nil {
		return err
	}

	if opts.DryRun {
		ctx.Logf("[dry-run] 版本 %s → %s", canonical, newVersion)
		for _, p := range e.activeProviders(ctx) {
			if p != e.git && e.cfg.ProviderWritable(p.Name()) {
				ctx.Logf("[dry-run] %s: 将写入 %s", p.Name(), newVersion)
			}
		}
		if !opts.NoTag {
			ctx.Logf("[dry-run] 将创建 tag %s", tag)
		}
		return nil
	}

	for _, p := range e.activeProviders(ctx) {
		if p == e.git {
			continue
		}
		if !e.cfg.ProviderWritable(p.Name()) {
			ctx.Logf("%s: 配置为只读，跳过写入", p.Name())
			continue
		}
		if err := p.Write(ctx, newVersion); err != nil {
			return fmt.Errorf("写入 %s 失败: %w", p.Name(), err)
		}
		ctx.Logf("%s: 已更新到 %s", p.Name(), newVersion)
	}

	if opts.Commit {
		if err := e.git.CommitVersionChange(fmt.Sprintf("chore(release): bump to %s", tag)); err != nil {
			return err
		}
	}
	if !opts.NoTag {
		if err := e.git.CreateTag(tag); err != nil {
			return err
		}
	}
	if opts.Push && !opts.NoTag {
		branch := opts.Branch
		if branch == "" {
			branch = "origin"
		}
		return e.git.PushTag(branch, tag)
	}
	return nil
}

// Set 显式设置版本：校验 semver、写回所有 provider，可选建 tag/推送。
func (e *Engine) Set(ctx *provider.Context, version string, opts Options) error {
	v, err := semver.NewVersion(version)
	if err != nil {
		return fmt.Errorf("无效版本号 %q: %w", version, err)
	}
	normalized := v.String()
	tag := e.git.TagFor(normalized)

	if opts.DryRun {
		ctx.Logf("[dry-run] 将设置版本 %s", normalized)
		for _, p := range e.activeProviders(ctx) {
			if p != e.git && e.cfg.ProviderWritable(p.Name()) {
				ctx.Logf("[dry-run] %s: 将写入 %s", p.Name(), normalized)
			}
		}
		if !opts.NoTag {
			ctx.Logf("[dry-run] 将创建 tag %s", tag)
		}
		return nil
	}

	for _, p := range e.activeProviders(ctx) {
		if p == e.git {
			continue
		}
		if !e.cfg.ProviderWritable(p.Name()) {
			ctx.Logf("%s: 配置为只读，跳过写入", p.Name())
			continue
		}
		if err := p.Write(ctx, normalized); err != nil {
			return fmt.Errorf("写入 %s 失败: %w", p.Name(), err)
		}
		ctx.Logf("%s: 已更新到 %s", p.Name(), normalized)
	}

	if opts.Commit {
		if err := e.git.CommitVersionChange(fmt.Sprintf("chore(release): bump to %s", tag)); err != nil {
			return err
		}
	}
	if !opts.NoTag {
		if err := e.git.CreateTag(tag); err != nil {
			return err
		}
	}
	if opts.Push && !opts.NoTag {
		branch := opts.Branch
		if branch == "" {
			branch = "origin"
		}
		return e.git.PushTag(branch, tag)
	}
	return nil
}
