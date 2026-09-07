package lua

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/yuin/gopher-lua"
)

// Plugin 描述一个已发现的 Lua 插件。
type Plugin struct {
	Name     string
	Kind     string // "provider" | "hook"
	Priority int
	Desc     string
	Path     string // 插件文件绝对路径（Content 非空时可为空）
	Content  string // 内嵌脚本内容（空 = 从 Path 加载）
	Source   string // "global" | "project" | "embedded"
	Err      error  // 加载错误（非法脚本仍被列出，但不参与执行）
}

// Discover 扫描全局（用户配置目录）与项目级插件目录，解析 manifest。
// 同名插件项目内覆盖全局；返回按优先级降序（同级按名称）排序的插件列表。
// 非法脚本以 Err 标记，不阻塞其他插件。
// gitLatest 供宿主 API gt.git.latest_tag() 使用，可传 nil。
func Discover(projectRoot string, gitLatest func() string) []Plugin {
	globalDir := ""
	if d, err := os.UserConfigDir(); err == nil {
		globalDir = filepath.Join(d, "git-tags", "plugins")
	}
	projectDir := filepath.Join(projectRoot, ".git-tags", "plugins")

	var plugins []Plugin
	plugins = append(plugins, scanDir(globalDir, "global", gitLatest)...)
	plugins = append(plugins, scanDir(projectDir, "project", gitLatest)...)

	// 同名去重：保留后出现的（项目覆盖全局）
	byName := map[string]int{}
	dedup := make([]Plugin, 0, len(plugins))
	for _, p := range plugins {
		if i, ok := byName[p.Name]; ok {
			dedup[i] = p
			continue
		}
		byName[p.Name] = len(dedup)
		dedup = append(dedup, p)
	}

	sort.SliceStable(dedup, func(i, j int) bool {
		if dedup[i].Priority != dedup[j].Priority {
			return dedup[i].Priority > dedup[j].Priority
		}
		return dedup[i].Name < dedup[j].Name
	})
	return dedup
}

// scanDir 扫描目录下所有 *.lua 文件并解析 manifest。
func scanDir(dir, source string, gitLatest func() string) []Plugin {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // 目录不存在/不可读：视为无插件
	}
	var out []Plugin
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".lua" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		p := Plugin{Name: e.Name(), Source: source, Path: path, Priority: 50}
		name, kind, desc, priority, err := loadManifest(path, gitLatest)
		if err != nil {
			p.Err = err
		} else {
			p.Name = name
			p.Kind = kind
			p.Desc = desc
			p.Priority = priority
		}
		out = append(out, p)
	}
	return out
}

// loadManifestFrom 解析插件脚本的 plugin 全局表（name/type/priority/description）。
func loadManifestFrom(r *Runner, path, content string) (name, kind, desc string, priority int, err error) {
	err = r.exec(func(L *lua.LState) error {
		tbl, err := loadPluginSource(L, path, content)
		if err != nil {
			return err
		}
		name = tableString(L, tbl, "name", filepath.Base(path))
		kind = tableString(L, tbl, "type", "")
		if kind != "provider" && kind != "hook" {
			return fmt.Errorf("插件类型 %q 无效（应为 provider 或 hook）", kind)
		}
		desc = tableString(L, tbl, "description", "")
		priority = tableInt(L, tbl, "priority", 50)
		return nil
	})
	return
}

// loadManifest 解析插件文件的 plugin 全局表（name/type/priority/description）。
func loadManifest(path string, gitLatest func() string) (name, kind, desc string, priority int, err error) {
	return loadManifestFrom(NewRunner(".", gitLatest), path, "")
}

// loadManifestContent 解析内嵌脚本内容的 plugin 全局表。
func loadManifestContent(content string, gitLatest func() string) (name, kind, desc string, priority int, err error) {
	return loadManifestFrom(NewRunner(".", gitLatest), "", content)
}

// LoadEmbeddedProvider 从内嵌内容创建 Lua provider 插件（先解析 manifest 再包装）。
// 内容必须声明 type = "provider" 并实现 detect/read/write。
func LoadEmbeddedProvider(name, content string, r *Runner) (*Provider, error) {
	manifestName, kind, _, priority, err := loadManifestContent(content, nil)
	if err != nil {
		return nil, fmt.Errorf("内嵌插件 %s 解析失败: %w", name, err)
	}
	if kind != "provider" {
		return nil, fmt.Errorf("内嵌插件 %s 类型为 %q，应为 provider", name, kind)
	}
	if manifestName != "" {
		name = manifestName
	}
	return &Provider{
		plugin: Plugin{Name: name, Kind: "provider", Priority: priority, Content: content, Source: "embedded"},
		runner: r,
	}, nil
}

func tableString(L *lua.LState, tbl *lua.LTable, key, def string) string {
	if v, ok := tbl.RawGetString(key).(lua.LString); ok {
		return string(v)
	}
	return def
}

func tableInt(L *lua.LState, tbl *lua.LTable, key string, def int) int {
	if v, ok := tbl.RawGetString(key).(lua.LNumber); ok {
		return int(v)
	}
	return def
}

// Validate 校验插件文件：语法、manifest、类型对应必需函数。
// provider 插件必须有 detect/read/write；hook 插件至少实现一个钩子函数。
func Validate(path string) error {
	r := NewRunner(".", nil)
	return r.exec(func(L *lua.LState) error {
		tbl, err := loadPlugin(L, path)
		if err != nil {
			return err
		}
		kind := tableString(L, tbl, "type", "")
		switch kind {
		case "provider":
			for _, fn := range []string{"detect", "read", "write"} {
				if L.GetField(tbl, fn) == lua.LNil {
					return fmt.Errorf("provider 插件缺少函数 %s", fn)
				}
			}
		case "hook":
			found := false
			for _, fn := range []string{"pre_bump", "post_bump", "pre_tag", "post_tag"} {
				if L.GetField(tbl, fn) != lua.LNil {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("hook 插件至少需要实现 pre_bump/post_bump/pre_tag/post_tag 之一")
			}
		default:
			return fmt.Errorf("插件类型 %q 无效（应为 provider 或 hook）", kind)
		}
		return nil
	})
}
