package lua

import (
	"fmt"

	"github.com/yuin/gopher-lua"

	"git-tags/internal/provider"
)

// Provider 把 Lua provider 插件包装为 provider.Provider，与内置 Go provider
// 地位对等，可被引擎以相同方式调用。
type Provider struct {
	plugin Plugin
	runner *Runner
}

// NewProvider 用已发现的插件与 Runner 构造 Lua provider。
func NewProvider(p Plugin, r *Runner) *Provider {
	return &Provider{plugin: p, runner: r}
}

func (p *Provider) Name() string { return p.plugin.Name }

func (p *Provider) Priority() int { return p.plugin.Priority }

func (p *Provider) Targets() []provider.Target { return nil }

// load 执行插件脚本（文件或内嵌内容）并返回 plugin 全局表。
func (p *Provider) load(L *lua.LState) (*lua.LTable, error) {
	return loadPluginSource(L, p.plugin.Path, p.plugin.Content)
}

// Detect 调用插件 detect(project)；脚本错误视为未命中（返回 false）。
func (p *Provider) Detect(ctx *provider.Context) bool {
	ok := false
	err := p.runner.exec(func(L *lua.LState) error {
		tbl, err := p.load(L)
		if err != nil {
			return err
		}
		if err := callFn(L, tbl, "detect", 1, projectTable(L, p.runner.root)); err != nil {
			return err
		}
		ok = lua.LVAsBool(L.Get(-1))
		return nil
	})
	if err != nil {
		return false
	}
	return ok
}

// Read 调用插件 read(project)，返回版本字符串。
func (p *Provider) Read(ctx *provider.Context) (string, error) {
	var ver string
	err := p.runner.exec(func(L *lua.LState) error {
		tbl, err := p.load(L)
		if err != nil {
			return err
		}
		if err := callFn(L, tbl, "read", 1, projectTable(L, p.runner.root)); err != nil {
			return err
		}
		ver = L.Get(-1).String()
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("%s: %w", p.plugin.Name, err)
	}
	return ver, nil
}

// Write 调用插件 write(project, version)。
func (p *Provider) Write(ctx *provider.Context, version string) error {
	err := p.runner.exec(func(L *lua.LState) error {
		tbl, err := p.load(L)
		if err != nil {
			return err
		}
		return callFn(L, tbl, "write", 0, projectTable(L, p.runner.root), lua.LString(version))
	})
	if err != nil {
		return fmt.Errorf("%s: %w", p.plugin.Name, err)
	}
	return nil
}

// projectTable 构造传给插件的 project 表。
func projectTable(L *lua.LState, root string) *lua.LTable {
	t := L.NewTable()
	L.SetField(t, "root", lua.LString(root))
	return t
}
