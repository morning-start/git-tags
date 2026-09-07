package lua

import (
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
