package lua

import (
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/yuin/gopher-lua"
)

// registerAPI 向 LState 注册宿主 API 表 gt：
//   - gt.project        项目上下文 { root = <绝对路径> }
//   - gt.read_file(path)        读取项目内文件（路径白名单）
//   - gt.write_file(path, c)    写入项目内文件（路径白名单）
//   - gt.log(msg)               输出用户可见日志
//   - gt.semver.parse/inc/compare  版本运算
//   - gt.git.latest_tag()       查询最新 git tag
func (r *Runner) registerAPI(L *lua.LState) {
	gt := L.NewTable()

	L.SetField(gt, "log", L.NewFunction(func(L *lua.LState) int {
		fmt.Println("[lua]", L.CheckString(1))
		return 0
	}))

	L.SetField(gt, "read_file", L.NewFunction(func(L *lua.LState) int {
		content, err := r.readFile(L.CheckString(1))
		if err != nil {
			L.RaiseError("%v", err)
			return 0
		}
		L.Push(lua.LString(content))
		return 1
	}))

	L.SetField(gt, "write_file", L.NewFunction(func(L *lua.LState) int {
		path := L.CheckString(1)
		content := L.CheckString(2)
		if err := r.writeFile(path, content); err != nil {
			L.RaiseError("%v", err)
			return 0
		}
		return 0
	}))

	proj := L.NewTable()
	L.SetField(proj, "root", lua.LString(r.root))
	L.SetField(gt, "project", proj)

	L.SetField(gt, "semver", r.semverTable(L))
	L.SetField(gt, "git", r.gitTable(L))

	L.SetGlobal("gt", gt)
}

func (r *Runner) semverTable(L *lua.LState) *lua.LTable {
	t := L.NewTable()

	L.SetField(t, "parse", L.NewFunction(func(L *lua.LState) int {
		v, err := semver.NewVersion(L.CheckString(1))
		if err != nil {
			L.Push(lua.LNil)
			return 1
		}
		info := L.NewTable()
		L.SetField(info, "major", lua.LNumber(v.Major()))
		L.SetField(info, "minor", lua.LNumber(v.Minor()))
		L.SetField(info, "patch", lua.LNumber(v.Patch()))
		L.SetField(info, "prerelease", lua.LString(v.Prerelease()))
		L.SetField(info, "metadata", lua.LString(v.Metadata()))
		L.SetField(info, "str", lua.LString(v.String()))
		L.Push(info)
		return 1
	}))

	L.SetField(t, "inc", L.NewFunction(func(L *lua.LState) int {
		version := L.CheckString(1)
		level := L.CheckString(2)
		v, err := semver.NewVersion(version)
		if err != nil {
			L.RaiseError("无效版本 %q: %v", version, err)
			return 0
		}
		var next semver.Version
		switch level {
		case "patch":
			next = v.IncPatch()
		case "minor":
			next = v.IncMinor()
		case "major":
			next = v.IncMajor()
		default:
			L.RaiseError("无效递增级别 %q（应为 patch/minor/major）", level)
			return 0
		}
		L.Push(lua.LString(next.String()))
		return 1
	}))

	L.SetField(t, "compare", L.NewFunction(func(L *lua.LState) int {
		a, errA := semver.NewVersion(L.CheckString(1))
		b, errB := semver.NewVersion(L.CheckString(2))
		if errA != nil || errB != nil {
			L.RaiseError("无效版本比较参数")
			return 0
		}
		L.Push(lua.LNumber(a.Compare(b)))
		return 1
	}))

	return t
}

func (r *Runner) gitTable(L *lua.LState) *lua.LTable {
	t := L.NewTable()
	L.SetField(t, "latest_tag", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LString(r.gitLatest()))
		return 1
	}))
	return t
}
