package lua

import (
	"fmt"

	"github.com/yuin/gopher-lua"

	"git-tags/internal/provider"
)

// Hook 把 Lua hook 插件包装为 provider.Hook，由引擎在 bump 流程的
// pre_bump / post_bump / pre_tag / post_tag 阶段调用。
type Hook struct {
	plugin Plugin
	runner *Runner
}

// NewHook 用已发现的插件与 Runner 构造 Lua hook。
func NewHook(p Plugin, r *Runner) *Hook {
	return &Hook{plugin: p, runner: r}
}

func (h *Hook) Name() string { return h.plugin.Name }

func (h *Hook) Priority() int { return h.plugin.Priority }

// Run 调用插件中对应 stage 的函数；未实现该函数则静默跳过。
func (h *Hook) Run(ctx *provider.Context, stage string, from, to string) error {
	err := h.runner.exec(func(L *lua.LState) error {
		tbl, err := loadPlugin(L, h.plugin.Path)
		if err != nil {
			return err
		}
		fn := L.GetField(tbl, stage)
		if fn == lua.LNil {
			return nil // 该 hook 未实现此阶段
		}
		L.Push(fn)
		switch stage {
		case "pre_bump", "post_bump":
			L.Push(lua.LString(from))
			L.Push(lua.LString(to))
			return L.PCall(2, 0, nil)
		case "pre_tag", "post_tag":
			L.Push(lua.LString(to))
			return L.PCall(1, 0, nil)
		default:
			return fmt.Errorf("未知 hook 阶段 %s", stage)
		}
	})
	if err != nil {
		return fmt.Errorf("%s: %w", h.plugin.Name, err)
	}
	return nil
}
