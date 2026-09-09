// Package core 实现版本同步引擎：编排所有激活的 provider，执行
// detect → read → check → compute → write → tag → commit → push 流程。
// git tag 是唯一权威源：bump 的新版本由它计算，其他 provider 只做同步。
package core

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"

	"git-tags/internal/config"
	"git-tags/internal/provider"
)

// Options 控制一次 bump/set 的行为。
type Options struct {
	DryRun bool   // 只预览，不产生任何改动
	NoTag  bool   // 只改文件，不提交、不创建 tag
	Push   bool   // 创建 tag 后推送到远程
	Branch string // push 目标分支，默认 "origin"
	// TargetFramework 定向模式（set --framework）：只把版本写入指定 provider，
	// 不创建 tag、不 commit/push——单 provider 改动若建全局 tag 会立刻与其它
	// provider 不一致；定向设置由后续全量 set/bump 统一收口到 tag。
	TargetFramework string
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
	hooks     []provider.Hook
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

// AddHooks 注册 bump 流程扩展点（按优先级降序执行）。
func (e *Engine) AddHooks(hooks ...provider.Hook) {
	e.hooks = append(e.hooks, hooks...)
	sort.SliceStable(e.hooks, func(i, j int) bool { return e.hooks[i].Priority() > e.hooks[j].Priority() })
}

// runHooks 在指定阶段调用所有 hook；任一失败即中止。
func (e *Engine) runHooks(ctx *provider.Context, stage, from, to string) error {
	for _, h := range e.hooks {
		if err := h.Run(ctx, stage, from, to); err != nil {
			return fmt.Errorf("hook %s(%s) 失败: %w", h.Name(), stage, err)
		}
	}
	return nil
}

// previewWrite 输出 dry-run 时 provider 将要写入的细节：
// 实现 Previewer 的 provider 显示逐文件「旧值 → 新值」，否则回退到 provider 级提示。
func (e *Engine) previewWrite(ctx *provider.Context, p provider.Provider, version string) {
	if pr, ok := p.(provider.Previewer); ok {
		if changes, err := pr.Preview(ctx, version); err == nil && len(changes) > 0 {
			for _, c := range changes {
				ctx.Logf("[dry-run] %s: %s → %s", c.Path, c.Old, c.New)
			}
			return
		}
	}
	ctx.Logf("[dry-run] %s: 将写入 %s", p.Name(), version)
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
func (e *Engine) ListTags(root string) error {
	e.git.SetRoot(root)
	out, err := e.git.ListTags()
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

// LatestTag 返回最新 tag 名（带前缀）。
func (e *Engine) LatestTag(root string) string {
	e.git.SetRoot(root)
	return e.git.LatestTag()
}

// PushTag 推送最新 tag 到远程分支（先推 commit，保证远端存在 tag 指向的提交）。
func (e *Engine) PushTag(branch, root string) error {
	e.git.SetRoot(root)
	return e.git.PushTag(branch, e.git.LatestTag())
}

// DeleteLatestTag 删除最新 tag：先远程、后本地。远程删除只有 not exist
// （尚未推送）被视为合理并跳过，其它错误立即返回、保留本地 tag；本地删除
// 失败同样返回。删除后若存在更早 tag，默认把版本文件回滚同步到新最新 tag
// （写文件但不自动 commit，沿用 opt-in 哲学）；无更早 tag 时只删 tag、不写文件。
func (e *Engine) DeleteLatestTag(branch, root string) error {
	e.git.SetRoot(root)
	old := e.git.LatestTag()
	if err := e.git.DeleteRemoteTag(branch, old); err != nil {
		return err
	}
	if err := e.git.DeleteLocalTag(old); err != nil {
		return err
	}

	cur := e.git.LatestTagOrEmpty()
	if cur == "" {
		fmt.Printf("已删除 tag %s；项目无更早 tag，版本文件保持不变\n", old)
		return nil
	}
	ctx := &provider.Context{
		Project: &provider.Project{Root: root},
		Log:     func(format string, args ...any) { fmt.Printf(format+"\n", args...) },
	}
	fmt.Printf("已删除 tag %s，回滚版本文件到 %s\n", old, cur)
	return e.Sync(ctx, false)
}

// Check 读取所有激活 provider 的版本并与权威源 git tag 比对，返回一致性报告。
func (e *Engine) Check(ctx *provider.Context) ([]CheckItem, error) {
	e.git.SetRoot(ctx.Project.Root)
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
	e.git.SetRoot(ctx.Project.Root)
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

// ensureCleanWorktree 校验工作区干净（无未提交/未跟踪改动）。bump/set 默认
// 会提交版本文件改动，若工作区混有其它改动会被 git add -A 一并卷入发布提交，
// 因此提交前必须先保持工作区干净。
func (e *Engine) ensureCleanWorktree() error {
	dirty, err := e.git.HasUncommittedChanges()
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("工作区有未提交的改动；请先提交或 stash 后再运行（git-tags 会提交版本文件改动并打 tag）")
	}
	return nil
}

// Sync 以权威源版本写回所有激活 provider 的可写 target。
func (e *Engine) Sync(ctx *provider.Context, dryRun bool) error {
	e.git.SetRoot(ctx.Project.Root)
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
			e.previewWrite(ctx, p, canonical)
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
// provider、提交版本改动、创建 tag，可选推送。发布（非 --no-tag）流程要求
// 工作区先保持干净（提交是强制步骤，避免 git add -A 把无关改动卷入发布提交），
// 然后写文件 → 提交 → 建 tag。流程中按序触发 hook：
// pre_bump → 写文件 → post_bump → 提交 → pre_tag → 建 tag → post_tag。
func (e *Engine) Bump(ctx *provider.Context, level string, opts Options) error {
	e.git.SetRoot(ctx.Project.Root)
	if !opts.NoTag && !opts.DryRun {
		if err := e.ensureCleanWorktree(); err != nil {
			return err
		}
	}
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
	if err := e.runHooks(ctx, "pre_bump", canonical, newVersion); err != nil {
		return err
	}

	if opts.DryRun {
		ctx.Logf("[dry-run] 版本 %s → %s", canonical, newVersion)
		for _, p := range e.activeProviders(ctx) {
			if p != e.git && e.cfg.ProviderWritable(p.Name()) {
				e.previewWrite(ctx, p, newVersion)
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

	if err := e.runHooks(ctx, "post_bump", canonical, newVersion); err != nil {
		return err
	}
	if !opts.NoTag {
		// 发布流程强制提交：保证 tag 指向包含新版本号的 commit。
		if err := e.git.CommitVersionChange(fmt.Sprintf("chore(release): bump to %s", tag)); err != nil {
			return err
		}
		if err := e.runHooks(ctx, "pre_tag", "", newVersion); err != nil {
			return err
		}
		if err := e.git.CreateTag(tag); err != nil {
			return err
		}
		if err := e.runHooks(ctx, "post_tag", "", newVersion); err != nil {
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

// setFramework 定向设置：只把版本写入指定 provider，不创建 tag/commit/push
// （单 provider 改动若建全局 tag 会立刻与其它 provider 不一致）。未激活或
// 不存在时报错并列出当前激活的 provider；配置为只读的 provider 拒绝写入。
func (e *Engine) setFramework(ctx *provider.Context, name, version string, opts Options) error {
	var target provider.Provider
	names := []string{}
	for _, p := range e.activeProviders(ctx) {
		if p == e.git {
			continue
		}
		names = append(names, p.Name())
		if p.Name() == name {
			target = p
		}
	}
	if target == nil {
		return fmt.Errorf("framework %q 未激活或不存在（当前激活: %s）", name, strings.Join(names, ", "))
	}
	if !e.cfg.ProviderWritable(name) {
		return fmt.Errorf("provider %s 配置为只读，无法定向设置", name)
	}
	if opts.DryRun {
		e.previewWrite(ctx, target, version)
		return nil
	}
	if err := target.Write(ctx, version); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", name, err)
	}
	ctx.Logf("%s: 已定向更新到 %s", name, version)
	ctx.Logf("仅更新了 %s，未创建 tag；如需发布请运行 git-tags set %s 全量同步", name, version)
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

	// 定向模式（--framework）：只写指定 provider，不建 tag/commit/push
	if opts.TargetFramework != "" {
		return e.setFramework(ctx, opts.TargetFramework, normalized, opts)
	}

	// 与 Bump 一致：发布（非 --no-tag）要求工作区干净，避免无关改动卷入发布提交；
	// dry-run 只是预览，不校验工作区状态。
	if !opts.NoTag && !opts.DryRun {
		if err := e.ensureCleanWorktree(); err != nil {
			return err
		}
	}

	if opts.DryRun {
		ctx.Logf("[dry-run] 将设置版本 %s", normalized)
		for _, p := range e.activeProviders(ctx) {
			if p != e.git && e.cfg.ProviderWritable(p.Name()) {
				e.previewWrite(ctx, p, normalized)
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

	if !opts.NoTag {
		// 发布流程强制提交：保证 tag 指向包含新版本号的 commit。
		if err := e.git.CommitVersionChange(fmt.Sprintf("chore(release): bump to %s", tag)); err != nil {
			return err
		}
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
