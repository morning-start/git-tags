// Package lua 提供 Lua 插件子系统：沙箱化的 gopher-lua 运行时、宿主 API（gt）、
// 插件发现与 LuaProvider / LuaHook 适配器。Lua 插件与内置 Go provider 地位对等，
// 只是 Provider 接口的另一种实现。
package lua

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yuin/gopher-lua"

	"git-tags/internal/config"
)

// Runner 在沙箱中执行 Lua 脚本：每次调用使用全新 LState，带超时与 panic 兜底。
type Runner struct {
	root      string        // 项目根（绝对路径），路径白名单基准
	timeout   time.Duration // 单次脚本执行超时
	gitLatest func() string // 供宿主 API gt.git.latest_tag() 使用
	carriers  map[string][]config.Carrier // 供宿主 API gt.config.provider.<name>.carriers 使用
}

// NewRunner 创建 Runner；root 转为绝对路径作为路径白名单基准，
// gitLatest 可空（此时 gt.git.latest_tag 返回空串）。
func NewRunner(root string, gitLatest func() string) *Runner {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	if gitLatest == nil {
		gitLatest = func() string { return "" }
	}
	return &Runner{root: abs, timeout: 5 * time.Second, gitLatest: gitLatest}
}

// SetConfig 注入项目配置：把 [provider.<name>] carriers 暴露给 Lua 插件
// （经 gt.config.provider.<name>.carriers 读取）。cfg 可空（视为无配置）。
func (r *Runner) SetConfig(cfg *config.Config) {
	if cfg == nil {
		r.carriers = nil
		return
	}
	r.carriers = cfg.ProviderCarriers
}

// SetTimeout 设置单次脚本执行超时（默认 5s；测试可调短）。
func (r *Runner) SetTimeout(d time.Duration) { r.timeout = d }

// Root 返回路径白名单基准（绝对路径）。
func (r *Runner) Root() string { return r.root }

// newState 创建沙箱化的 LState：只开放 base/table/string/math 库，
// 并移除危险的 base 函数。os/io/package/debug/coroutine 一律不开放。
func (r *Runner) newState() *lua.LState {
	L := lua.NewState(lua.Options{
		SkipOpenLibs: true,
		// 默认 RegistryMaxSize=0 时 registry（VM 值栈）固定 8192 槽，
		// table.concat 拼接大数组（大 Cargo.lock / package-lock.json 等）
		// 会抛 "registry overflow"。放开上限让其按需扩容（约 50 万行安全）。
		RegistryMaxSize:  1024 * 1024,
		RegistryGrowStep: 1024,
	})
	lua.OpenBase(L)
	lua.OpenTable(L)
	lua.OpenString(L)
	lua.OpenMath(L)
	for _, name := range []string{"dofile", "loadfile", "load", "loadstring", "print"} {
		L.SetGlobal(name, lua.LNil)
	}
	return L
}

// exec 执行 fn；每次调用使用独立 LState（用完即关），超时与 panic 都被兜底。
// 超时时不关闭 LState，避免与仍在运行的脚本 goroutine 产生竞态（CLI 进程随即退出回收）。
func (r *Runner) exec(fn func(L *lua.LState) error) (err error) {
	L := r.newState()
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("lua 执行异常: %v", p)
		}
	}()

	r.registerAPI(L)

	done := make(chan error, 1)
	go func() { done <- fn(L) }()
	select {
	case err = <-done:
		L.Close()
	case <-time.After(r.timeout):
		return fmt.Errorf("脚本执行超时（%s）", r.timeout)
	}
	return err
}

// resolvePath 把插件传入的相对路径约束在项目根内。
func (r *Runner) resolvePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("空路径")
	}
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("不允许绝对路径: %s", path)
	}
	full := filepath.Join(r.root, filepath.Clean(path))
	rel, err := filepath.Rel(r.root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("路径越界: %s", path)
	}
	return full, nil
}

// readFile 读取项目根内文件（路径白名单）。
func (r *Runner) readFile(path string) (string, error) {
	full, err := r.resolvePath(path)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// writeFile 写入项目根内文件（路径白名单）。
func (r *Runner) writeFile(path, content string) error {
	full, err := r.resolvePath(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o644)
}

// loadPluginSource 执行插件脚本（文件路径或内嵌内容）并返回全局表 plugin。
func loadPluginSource(L *lua.LState, path, content string) (*lua.LTable, error) {
	if content != "" {
		if err := L.DoString(content); err != nil {
			return nil, err
		}
	} else if err := L.DoFile(path); err != nil {
		return nil, err
	}
	tbl, ok := L.GetGlobal("plugin").(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("插件 %s 未定义全局表 plugin（请用 plugin = {...} 声明）", path)
	}
	return tbl, nil
}

// loadPlugin 执行插件文件并返回全局表 plugin。
func loadPlugin(L *lua.LState, path string) (*lua.LTable, error) {
	return loadPluginSource(L, path, "")
}

// callFn 调用 plugin 表中名为 name 的函数，返回调用错误（函数缺失也算错误）。
func callFn(L *lua.LState, tbl *lua.LTable, name string, nRet int, args ...lua.LValue) error {
	fn := L.GetField(tbl, name)
	if fn == lua.LNil {
		return fmt.Errorf("插件缺少函数 %s", name)
	}
	L.Push(fn)
	for _, a := range args {
		L.Push(a)
	}
	return L.PCall(len(args), nRet, nil)
}
