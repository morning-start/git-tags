package lua

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yuin/gopher-lua"

	"git-tags/internal/config"
	"git-tags/internal/core"
	"git-tags/internal/provider"
)

const testProviderPlugin = `plugin = {
  name = "myapp",
  type = "provider",
  priority = 60,
  description = "test provider",
}

function plugin.detect(project)
  local ok, _ = pcall(gt.read_file, "VERSION")
  return ok
end

function plugin.read(project)
  local c = gt.read_file("VERSION")
  return c:match("^%s*([^%s\n]+)")
end

function plugin.write(project, version)
  gt.write_file("VERSION", version .. "\n")
end
`

// initTestRepo 在目录中初始化 git 仓库并打 tag。
func initTestRepo(t *testing.T, dir, tag string) {
	t.Helper()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "init")
	if tag != "" {
		runGit(t, dir, "tag", tag)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v 失败: %v\n%s", args, err, out)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入 %s 失败: %v", path, err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return string(data)
}

func TestProviderPluginE2E(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "VERSION"), "1.2.3\n")
	writeTestFile(t, filepath.Join(root, ".git-tags", "plugins", "myapp.lua"), testProviderPlugin)
	initTestRepo(t, root, "v1.2.3")

	plugins := Discover(root, nil)
	if len(plugins) != 1 {
		t.Fatalf("Discover 应找到 1 个插件，实际 %d（%+v）", len(plugins), plugins)
	}
	p := plugins[0]
	if p.Err != nil {
		t.Fatalf("插件加载失败: %v", p.Err)
	}

	engine := core.New(config.Default(), NewProvider(p, NewRunner(root, nil)))
	ctx := &provider.Context{Project: &provider.Project{Root: root}, Log: t.Logf}

	items, err := engine.Check(ctx)
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	var found bool
	for _, it := range items {
		if it.Provider == "myapp" {
			found = true
			if it.Version != "1.2.3" || !it.InSync {
				t.Errorf("myapp check = %+v, want 1.2.3 in sync", it)
			}
		}
	}
	if !found {
		t.Fatalf("check 报告未包含 myapp: %+v", items)
	}

	if err := engine.Bump(ctx, "patch", core.Options{}); err != nil {
		t.Fatalf("Bump error: %v", err)
	}
	content := readTestFile(t, filepath.Join(root, "VERSION"))
	if content != "1.2.4\n" {
		t.Errorf("VERSION = %q, want 1.2.4", content)
	}
	if got := engine.LatestTag(root); got != "v1.2.4" {
		t.Errorf("最新 tag = %s, want v1.2.4", got)
	}
}

func TestHookPluginE2E(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "VERSION"), "1.2.3\n")
	writeTestFile(t, filepath.Join(root, ".git-tags", "plugins", "marker.lua"), `plugin = {
  name = "marker",
  type = "hook",
  priority = 50,
}
function plugin.pre_bump(from, to)
  gt.write_file("BUMP_LOG", from .. " -> " .. to .. "\n")
end
function plugin.pre_tag(version)
  gt.write_file("TAG_LOG", version .. "\n")
end
`)
	initTestRepo(t, root, "v1.2.3")

	plugins := Discover(root, nil)
	engine := core.New(config.Default())
	for _, p := range plugins {
		if p.Err == nil && p.Kind == "hook" {
			engine.AddHooks(NewHook(p, NewRunner(root, nil)))
		}
	}
	ctx := &provider.Context{Project: &provider.Project{Root: root}, Log: t.Logf}
	if err := engine.Bump(ctx, "minor", core.Options{}); err != nil {
		t.Fatalf("Bump error: %v", err)
	}
	if got := readTestFile(t, filepath.Join(root, "BUMP_LOG")); got != "1.2.3 -> 1.3.0\n" {
		t.Errorf("BUMP_LOG = %q, want 1.2.3 -> 1.3.0", got)
	}
	if got := readTestFile(t, filepath.Join(root, "TAG_LOG")); got != "1.3.0\n" {
		t.Errorf("TAG_LOG = %q, want 1.3.0", got)
	}
}

func TestSandbox(t *testing.T) {
	root := t.TempDir()
	r := NewRunner(root, nil)

	// 危险库被移除：os 不可用（对 nil 索引报错）
	err := r.exec(func(L *lua.LState) error {
		return L.DoString(`os.execute("echo hi")`)
	})
	if err == nil || !strings.Contains(err.Error(), "execute") {
		t.Errorf("os.execute 应报错，实际: %v", err)
	}

	// 相对路径越界被拒绝
	err = r.exec(func(L *lua.LState) error {
		return L.DoString(`gt.write_file("../evil.txt", "x")`)
	})
	if err == nil || !strings.Contains(err.Error(), "越界") {
		t.Errorf("越界路径应报错，实际: %v", err)
	}

	// 绝对路径被拒绝（Windows/Unix 通用构造；正斜杠避免 Lua 转义）
	evilAbs := "/evil.txt"
	if v := filepath.VolumeName(root); v != "" {
		evilAbs = v + "/evil.txt"
	}
	err = r.exec(func(L *lua.LState) error {
		return L.DoString(`gt.write_file("` + evilAbs + `", "x")`)
	})
	if err == nil || !strings.Contains(err.Error(), "绝对路径") {
		t.Errorf("绝对路径应报错，实际: %v", err)
	}

	// 死循环被超时终止
	r.SetTimeout(200 * time.Millisecond)
	err = r.exec(func(L *lua.LState) error {
		return L.DoString(`while true do end`)
	})
	if err == nil || !strings.Contains(err.Error(), "超时") {
		t.Errorf("死循环应超时，实际: %v", err)
	}

	// 参数类型错误被兜底（table 不能强转字符串）
	err = r.exec(func(L *lua.LState) error {
		return L.DoString(`gt.write_file({}, "x")`)
	})
	if err == nil {
		t.Errorf("类型错误应报错")
	}
}

func TestDiscoveryAndIsolation(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, ".git-tags", "plugins")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(pluginDir, "a-low.lua"), strings.Replace(testProviderPlugin,
		`name = "myapp"`, `name = "lowprio"`, 1))
	writeTestFile(t, filepath.Join(pluginDir, "b-high.lua"), strings.Replace(testProviderPlugin,
		`name = "myapp"`, `name = "highprio"`, 1))
	writeTestFile(t, filepath.Join(pluginDir, "broken.lua"), `plugin = { name = "broken",`)

	plugins := Discover(root, nil)
	// 3 个条目：2 个有效 + 1 个带错误标记（broken 不参与执行但不阻塞发现）
	if len(plugins) != 3 {
		t.Fatalf("应发现 3 个插件（2 有效 + 1 错误标记），实际 %d: %+v", len(plugins), plugins)
	}
	if plugins[0].Name != "highprio" || plugins[1].Name != "lowprio" {
		t.Errorf("按优先级排序结果错误: %s, %s", plugins[0].Name, plugins[1].Name)
	}
	if plugins[2].Name != "broken.lua" || plugins[2].Err == nil {
		t.Errorf("broken.lua 应带错误标记: %+v", plugins[2])
	}
	valid := 0
	for _, p := range plugins {
		if p.Err == nil {
			valid++
		}
	}
	if valid != 2 {
		t.Errorf("有效插件应为 2，实际 %d", valid)
	}

	if err := Validate(filepath.Join(pluginDir, "broken.lua")); err == nil {
		t.Errorf("broken.lua 校验应失败")
	}
	if err := Validate(filepath.Join(pluginDir, "a-low.lua")); err != nil {
		t.Errorf("a-low.lua 校验应通过: %v", err)
	}
}

func TestErrorIsolation(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "VERSION"), "1.2.3\n")
	pluginDir := filepath.Join(root, ".git-tags", "plugins")
	writeTestFile(t, filepath.Join(pluginDir, "myapp.lua"), testProviderPlugin)
	writeTestFile(t, filepath.Join(pluginDir, "broken.lua"), `plugin = { name = "broken",`)
	initTestRepo(t, root, "v1.2.3")

	plugins := Discover(root, nil)
	var myapp *Plugin
	for i := range plugins {
		if plugins[i].Name == "myapp" {
			myapp = &plugins[i]
		}
		if plugins[i].Name == "broken.lua" && plugins[i].Err == nil {
			t.Errorf("broken.lua 应带错误标记")
		}
	}
	if myapp == nil {
		t.Fatalf("myapp 应被正常发现: %+v", plugins)
	}

	// 语法错误的插件不阻塞正常插件：引擎只注册有效插件
	engine := core.New(config.Default(), NewProvider(*myapp, NewRunner(root, nil)))
	if _, err := engine.Check(&provider.Context{Project: &provider.Project{Root: root}}); err != nil {
		t.Fatalf("有效插件 Check 应通过: %v", err)
	}

	// 运行时出错的插件：错误信息应标明插件名（可诊断）
	writeTestFile(t, filepath.Join(pluginDir, "bad.lua"), `plugin = {
  name = "bad",
  type = "provider",
  priority = 90,
}
function plugin.detect(project) return true end
function plugin.read(project) error("boom") end
function plugin.write(project, version) end
`)
	plugins2 := Discover(root, nil)
	var bad *Plugin
	for i := range plugins2 {
		if plugins2[i].Name == "bad" {
			bad = &plugins2[i]
		}
	}
	if bad == nil {
		t.Fatalf("bad 插件应被发现: %+v", plugins2)
	}
	engine2 := core.New(config.Default(), NewProvider(*bad, NewRunner(root, nil)))
	_, err := engine2.Check(&provider.Context{Project: &provider.Project{Root: root}})
	if err == nil || !strings.Contains(err.Error(), "bad") {
		t.Errorf("运行时错误应标明插件名 bad，实际: %v", err)
	}
}

// TestEmbeddedProviderE2E 验证内嵌内容创建的 provider 可走引擎全链路
// （对应二进制的内嵌 Lua 插件开箱即用）。
func TestEmbeddedProviderE2E(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "VERSION"), "1.2.3\n")
	initTestRepo(t, root, "v1.2.3")

	ep, err := LoadEmbeddedProvider("myapp", testProviderPlugin, NewRunner(root, nil))
	if err != nil {
		t.Fatalf("LoadEmbeddedProvider error: %v", err)
	}
	if ep.Name() != "myapp" || ep.Priority() != 60 {
		t.Errorf("内嵌 provider 元数据 = %s/%d, want myapp/60", ep.Name(), ep.Priority())
	}

	engine := core.New(config.Default(), ep)
	ctx := &provider.Context{Project: &provider.Project{Root: root}, Log: t.Logf}

	items, err := engine.Check(ctx)
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	var found bool
	for _, it := range items {
		if it.Provider == "myapp" {
			found = true
			if it.Version != "1.2.3" || !it.InSync {
				t.Errorf("myapp check = %+v, want 1.2.3 in sync", it)
			}
		}
	}
	if !found {
		t.Fatalf("check 未包含内嵌 provider myapp: %+v", items)
	}

	if err := engine.Bump(ctx, "patch", core.Options{}); err != nil {
		t.Fatalf("Bump error: %v", err)
	}
	if got := readTestFile(t, filepath.Join(root, "VERSION")); got != "1.2.4\n" {
		t.Errorf("VERSION = %q, want 1.2.4", got)
	}
}

// TestLoadEmbeddedProviderInvalid 验证非 provider 类型的内嵌内容被拒绝。
func TestLoadEmbeddedProviderInvalid(t *testing.T) {
	_, err := LoadEmbeddedProvider("bad", `plugin = { name = "bad", type = "hook" }`, NewRunner(".", nil))
	if err == nil {
		t.Errorf("hook 类型的内嵌内容应被拒绝")
	}
}

// TestBigLockFileWrite 回归：gopher-lua 默认 registry（VM 值栈）为固定 8192 槽位，
// 大 Cargo.lock 在 sync_lock_version → table.concat 拼接数万行时会抛
// "registry overflow"（patch/set/bump 全链路复现）。
func TestBigLockFileWrite(t *testing.T) {
	tauriLua, err := os.ReadFile(filepath.Join("..", "..", "plugins", "tauri.lua"))
	if err != nil {
		t.Fatalf("读取 plugins/tauri.lua 失败: %v", err)
	}
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "package.json"), "{\"name\": \"app\", \"version\": \"1.2.3\"}\n")
	writeTestFile(t, filepath.Join(root, "src-tauri", "tauri.conf.json"), "{ \"version\": \"1.2.3\" }\n")
	writeTestFile(t, filepath.Join(root, "src-tauri", "Cargo.toml"), "[package]\nname = \"app\"\nversion = \"1.2.3\"\n")
	writeTestFile(t, filepath.Join(root, "src-tauri", "Cargo.lock"), bigCargoLock("app", "1.2.3", 10000))
	initTestRepo(t, root, "v1.2.3")

	ep, err := LoadEmbeddedProvider("tauri", string(tauriLua), NewRunner(root, nil))
	if err != nil {
		t.Fatalf("LoadEmbeddedProvider error: %v", err)
	}
	engine := core.New(config.Default(), ep)
	ctx := &provider.Context{Project: &provider.Project{Root: root}, Log: t.Logf}

	if err := engine.Bump(ctx, "patch", core.Options{}); err != nil {
		t.Fatalf("Bump 大 Cargo.lock 失败: %v", err)
	}

	lock := readTestFile(t, filepath.Join(root, "src-tauri", "Cargo.lock"))
	if !strings.Contains(lock, "name = \"app\"\nversion = \"1.2.4\"") {
		t.Errorf("Cargo.lock 根包版本未同步到 1.2.4")
	}
}

// TestMoonbitProviderE2E 验证 moonbit 插件全链路：moon.mod 顶层 version 的
// 检测、check 一致性与 bump 写入；import { ... } 依赖串与其它顶层键不受影响。
// moon.mod 结构复刻真实 MoonBit 项目（prism）的 manifest。
func TestMoonbitProviderE2E(t *testing.T) {
	moonbitLua, err := os.ReadFile(filepath.Join("..", "..", "plugins", "moonbit.lua"))
	if err != nil {
		t.Fatalf("读取 plugins/moonbit.lua 失败: %v", err)
	}
	root := t.TempDir()
	const moonMod = `name = "morning-start/prism"

source = "src"

version = "0.1.3"

readme = "README.mbt.md"

repository = "https://github.com/morning-start/prism"

license = "MIT"

keywords = [ "llm", "wasm", "protocol", "middleware", "adapter" ]

preferred_target = "wasm-gc"

description = "A unified LLM protocol middleware converting between provider formats"

import {
  "moonbitlang/quickcheck@0.14.0",
}
`
	writeTestFile(t, filepath.Join(root, "moon.mod"), moonMod)
	initTestRepo(t, root, "v0.1.3")

	ep, err := LoadEmbeddedProvider("moonbit", string(moonbitLua), NewRunner(root, nil))
	if err != nil {
		t.Fatalf("LoadEmbeddedProvider error: %v", err)
	}
	if ep.Name() != "moonbit" || ep.Priority() != 55 {
		t.Errorf("内嵌 provider 元数据 = %s/%d, want moonbit/55", ep.Name(), ep.Priority())
	}

	ctx := &provider.Context{Project: &provider.Project{Root: root}, Log: t.Logf}
	if !ep.Detect(ctx) {
		t.Fatalf("含顶层 version 的 moon.mod 应被 moonbit 插件检测到")
	}

	engine := core.New(config.Default(), ep)

	items, err := engine.Check(ctx)
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	var found bool
	for _, it := range items {
		if it.Provider == "moonbit" {
			found = true
			if it.Version != "0.1.3" || !it.InSync {
				t.Errorf("moonbit check = %+v, want 0.1.3 in sync", it)
			}
		}
	}
	if !found {
		t.Fatalf("check 报告未包含 moonbit: %+v", items)
	}

	if err := engine.Bump(ctx, "patch", core.Options{}); err != nil {
		t.Fatalf("Bump error: %v", err)
	}

	updated := readTestFile(t, filepath.Join(root, "moon.mod"))
	if !strings.Contains(updated, "version = \"0.1.4\"") {
		t.Errorf("moon.mod 顶层 version 未同步到 0.1.4:\n%s", updated)
	}
	// import 依赖串（pkg@version）不是项目版本，必须原样保留
	if !strings.Contains(updated, "\"moonbitlang/quickcheck@0.14.0\",") {
		t.Errorf("moon.mod import 依赖串被误改:\n%s", updated)
	}
	// 其余顶层键与空行结构保持不变
	for _, keep := range []string{
		"name = \"morning-start/prism\"", "source = \"src\"", "readme = \"README.mbt.md\"",
		"preferred_target = \"wasm-gc\"", "license = \"MIT\"",
	} {
		if !strings.Contains(updated, keep) {
			t.Errorf("moon.mod 关键行 %q 丢失:\n%s", keep, updated)
		}
	}

	v, err := ep.Read(ctx)
	if err != nil {
		t.Fatalf("bump 后 Read error: %v", err)
	}
	if v != "0.1.4" {
		t.Errorf("bump 后 Read = %s, want 0.1.4", v)
	}
}

// TestMoonbitPluginDetect 验证检测边界：moon.mod 缺失或没有顶层 version 键时
// 插件不激活（段内/依赖串里的 version 不算项目版本）。
func TestMoonbitPluginDetect(t *testing.T) {
	moonbitLua, err := os.ReadFile(filepath.Join("..", "..", "plugins", "moonbit.lua"))
	if err != nil {
		t.Fatalf("读取 plugins/moonbit.lua 失败: %v", err)
	}
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"顶层 version", "name = \"a/b\"\nversion = \"1.2.3\"\n", true},
		{"version 在段内", "name = \"a/b\"\n\n[dependencies]\nversion = \"1.2.3\"\n", false},
		{"无 version 键", "name = \"a/b\"\n\nimport {\n  \"moonbitlang/core@0.1.0\",\n}\n", false},
		{"注释中的 version", "name = \"a/b\"\n# version = \"1.2.3\"\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, filepath.Join(root, "moon.mod"), tc.content)
			ep, err := LoadEmbeddedProvider("moonbit", string(moonbitLua), NewRunner(root, nil))
			if err != nil {
				t.Fatalf("LoadEmbeddedProvider error: %v", err)
			}
			ctx := &provider.Context{Project: &provider.Project{Root: root}}
			if got := ep.Detect(ctx); got != tc.want {
				t.Errorf("Detect = %v, want %v", got, tc.want)
			}
		})
	}

	// moon.mod 不存在时同样不激活
	root := t.TempDir()
	ep, err := LoadEmbeddedProvider("moonbit", string(moonbitLua), NewRunner(root, nil))
	if err != nil {
		t.Fatalf("LoadEmbeddedProvider error: %v", err)
	}
	if ep.Detect(&provider.Context{Project: &provider.Project{Root: root}}) {
		t.Errorf("无 moon.mod 时 Detect 应为 false")
	}
}

// bigCargoLock 生成根包条目 + extraCrates 个填充 crate 的 Cargo.lock，
// 总行数远超 gopher-lua 默认 8192 槽位 registry（每行约 2 个栈槽）。
func bigCargoLock(appName, ver string, extraCrates int) string {
	var b strings.Builder
	b.WriteString("# This file is automatically @generated by Cargo.\n")
	b.WriteString("# It is not intended for manual editing.\n")
	b.WriteString("version = 3\n\n")
	fmt.Fprintf(&b, "[[package]]\nname = %q\nversion = %q\nchecksum = \"root\"\n\n", appName, ver)
	for i := 0; i < extraCrates; i++ {
		fmt.Fprintf(&b, "[[package]]\nname = \"crate-%d\"\nversion = \"0.1.%d\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\nchecksum = \"%08x\"\n\n", i, i, i)
	}
	return b.String()
}
