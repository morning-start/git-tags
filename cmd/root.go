package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"git-tags/internal/config"
	"git-tags/internal/core"
	"git-tags/internal/lua"
	"git-tags/internal/provider"
)

// loadConfig 加载 .git-tags.toml；解析失败时直接退出（配置错误不应静默忽略）。
func loadConfig() *config.Config {
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置错误: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

// absProjectRoot 返回当前目录的绝对路径（Lua 路径白名单与插件发现基准）。
func absProjectRoot() string {
	a, err := filepath.Abs(".")
	if err != nil {
		return "."
	}
	return a
}

var (
	cfg     = loadConfig()
	absRoot = absProjectRoot()
	gitProv = newGitProvider()
	plugins = lua.Discover(absRoot, gitProv.LatestTag)
	engine  = newEngine()
)

// newGitProvider 创建绑定到项目根的 git provider（git 命令在项目内执行）。
func newGitProvider() *provider.GitProvider {
	g := provider.NewGitProvider(cfg.TagPrefix)
	g.SetRoot(absRoot)
	return g
}

// embeddedProviders 是内嵌进二进制的 Lua provider 内容（name → 脚本内容）。
// 优先级：用户插件 > 内嵌插件 > 内置 Go provider（同名时后者被覆盖）。
var embeddedProviders = map[string]string{}

// RegisterEmbeddedProvider 注册一个内嵌的 Lua provider（由 main 包注入）。
// 注意：engine 在包变量初始化时已构建（此时内嵌列表为空），所以注册后必须
// 重建引擎，内嵌插件才能生效（main 在 Execute 前调用本函数，时序安全）。
func RegisterEmbeddedProvider(name, content string) {
	embeddedProviders[name] = content
	engine = newEngine()
}

// newEngine 组装引擎：provider 只来自 Lua 插件（单轨），按名解析、逐级覆盖：
// 用户插件 > 内嵌插件（embeddedProviders 由 main 注入）。git provider 由引擎
// 固定作为权威源（不参与注册）；Lua hook 插件单独挂载到 bump 流程。
func newEngine() *core.Engine {
	// 用户 Lua provider 名（最高优先级）
	userProviders := map[string]bool{}
	for _, p := range plugins {
		if p.Err == nil && p.Kind == "provider" {
			userProviders[p.Name] = true
		}
	}

	// 按名解析最终 provider 列表：内嵌插件 → 用户插件（逐级覆盖）
	byName := map[string]provider.Provider{}
	for name, content := range embeddedProviders {
		if userProviders[name] {
			continue // 用户插件已覆盖，内嵌不再注册
		}
		r := lua.NewRunner(absRoot, gitProv.LatestTag)
		r.SetConfig(cfg)
		ep, err := lua.LoadEmbeddedProvider(name, content, r)
		if err != nil {
			fmt.Fprintf(os.Stderr, "警告: 内嵌插件 %s 加载失败: %v\n", name, err)
			continue
		}
		byName[name] = ep
	}
	for _, p := range plugins {
		if p.Err != nil || p.Kind != "provider" {
			continue
		}
		r := lua.NewRunner(absRoot, gitProv.LatestTag)
		r.SetConfig(cfg)
		byName[p.Name] = lua.NewProvider(p, r)
	}

	providers := make([]provider.Provider, 0, len(byName))
	for _, p := range byName {
		providers = append(providers, p)
	}

	var hooks []provider.Hook
	for _, p := range plugins {
		if p.Err != nil {
			continue
		}
		if p.Kind == "hook" {
			r := lua.NewRunner(absRoot, gitProv.LatestTag)
			r.SetConfig(cfg)
			hooks = append(hooks, lua.NewHook(p, r))
		}
	}
	e := core.New(cfg, providers...)
	e.AddHooks(hooks...)
	return e
}

// newContext 构造 provider 执行上下文：项目根为当前目录，日志输出到 stdout。
func newContext() *provider.Context {
	return &provider.Context{
		Project: &provider.Project{Root: "."},
		Log:     func(format string, args ...any) { fmt.Printf(format+"\n", args...) },
	}
}

var RootCmd = &cobra.Command{
	Use:               "git-tags",
	Short:             "Manage git tags",
	Long:              "A tool to manage git tags with version bumping capabilities.",
	CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	Version:           "2.0.1",
}

var ListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "(ls) Show all tags",
	RunE: func(cmd *cobra.Command, args []string) error {
		return engine.ListTags(absRoot)
	},
}

func newBumpCmd(name, level string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: fmt.Sprintf("Increment %s version", level),
		RunE: func(cmd *cobra.Command, args []string) error {
			push, _ := cmd.Flags().GetBool("push")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			noTag, _ := cmd.Flags().GetBool("no-tag")
			return engine.Bump(newContext(), level, core.Options{
				Push: push, DryRun: dryRun, NoTag: noTag,
			})
		},
	}
}

var PatchCmd = newBumpCmd("patch", "patch")
var MinorCmd = newBumpCmd("minor", "minor")
var MajorCmd = newBumpCmd("major", "major")

var PushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push tags to remote",
	RunE: func(cmd *cobra.Command, args []string) error {
		branch, _ := cmd.Flags().GetString("branch")
		return engine.PushTag(branch, absRoot)
	},
}

var CheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check version consistency across project providers",
	Long:  "Compare every active provider's version with the canonical git tag and report inconsistencies.",
	RunE: func(cmd *cobra.Command, args []string) error {
		items, err := engine.Check(newContext())
		if err != nil {
			return err
		}
		inSync := true
		for _, it := range items {
			status := "✓"
			if !it.InSync {
				status = "✗"
				inSync = false
			}
			fmt.Printf("%-10s %-12s %s\n", it.Provider, it.Version, status)
		}
		if !inSync {
			return fmt.Errorf("版本不同步：请运行 git-tags sync 以权威源版本写回各 provider")
		}
		return nil
	},
}

var SyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync canonical git tag version to all providers",
	Long:  "Write the canonical version (latest git tag) to every writable target of all active providers.",
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		return engine.Sync(newContext(), dryRun)
	},
}

var SetCmd = &cobra.Command{
	Use:   "set <version>",
	Short: "Set the project version explicitly",
	Long: "Set a new version, sync it to all providers, and optionally create a tag.\n" +
		"Use --framework to update only one provider without creating a tag.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		noTag, _ := cmd.Flags().GetBool("no-tag")
		push, _ := cmd.Flags().GetBool("push")
		framework, _ := cmd.Flags().GetString("framework")
		return engine.Set(newContext(), args[0], core.Options{
			DryRun: dryRun, NoTag: noTag, Push: push, TargetFramework: framework,
		})
	},
}

var DeleteCmd = &cobra.Command{
	Use:     "delete",
	Aliases: []string{"del"},
	Short:   "(del) Delete the latest tag, remote and local",
	RunE: func(cmd *cobra.Command, args []string) error {
		branch, _ := cmd.Flags().GetString("branch")
		return engine.DeleteLatestTag(branch, absRoot)
	},
}

func init() {
	PushCmd.Flags().StringP("branch", "b", "origin", "Specify the branch to push tags to")
	DeleteCmd.Flags().StringP("branch", "b", "origin", "Specify the remote branch to delete tags")
	SyncCmd.Flags().Bool("dry-run", false, "Preview changes without applying them")
	SetCmd.Flags().BoolP("push", "p", false, "Push tag to remote after creating")
	SetCmd.Flags().String("framework", "", "Only update the specified provider (e.g. flutter); no tag is created")
	SetCmd.Flags().Bool("dry-run", false, "Preview changes without applying them")
	SetCmd.Flags().Bool("no-tag", false, "Only update version files, do not create a tag")
	for _, c := range []*cobra.Command{PatchCmd, MinorCmd, MajorCmd} {
		c.Flags().BoolP("push", "p", false, "Push tag to remote after creating")
		c.Flags().Bool("dry-run", false, "Preview changes without applying them")
		c.Flags().Bool("no-tag", false, "Only update version files, do not create a tag")
	}

	// 命令分组：Git 管理（原 git tag 命令集）vs 插件与版本管理
	RootCmd.AddGroup(
		&cobra.Group{ID: "git", Title: "Git 管理"},
		&cobra.Group{ID: "plugin", Title: "插件与版本管理"},
	)
	ListCmd.GroupID = "git"
	PatchCmd.GroupID = "git"
	MinorCmd.GroupID = "git"
	MajorCmd.GroupID = "git"
	PushCmd.GroupID = "git"
	DeleteCmd.GroupID = "git"
	CheckCmd.GroupID = "plugin"
	SyncCmd.GroupID = "plugin"
	SetCmd.GroupID = "plugin"

	RootCmd.AddCommand(ListCmd, PatchCmd, MinorCmd, MajorCmd, PushCmd, DeleteCmd, CheckCmd, SyncCmd, SetCmd)
}
