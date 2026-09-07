package main

import (
	_ "embed"
	"fmt"
	"os"

	"git-tags/cmd"
)

//go:embed plugins/tauri.lua
var embeddedTauriPlugin string

//go:embed plugins/flutter.lua
var embeddedFlutterPlugin string

//go:embed plugins/uv.lua
var embeddedUVPlugin string

//go:embed plugins/node.lua
var embeddedNodePlugin string

//go:embed plugins/go.lua
var embeddedGoPlugin string

func main() {
	// 内嵌 Lua 插件注册为内置 provider：开箱即用、无需安装；
	// 用户可在项目/全局插件目录放置同名插件覆盖内嵌版本。
	cmd.RegisterEmbeddedProvider("tauri", embeddedTauriPlugin)
	cmd.RegisterEmbeddedProvider("flutter", embeddedFlutterPlugin)
	cmd.RegisterEmbeddedProvider("uv", embeddedUVPlugin)
	cmd.RegisterEmbeddedProvider("node", embeddedNodePlugin)
	cmd.RegisterEmbeddedProvider("go", embeddedGoPlugin)
	if err := cmd.RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
