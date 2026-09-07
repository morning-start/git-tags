package main

import (
	"embed"
	"fmt"
	"os"
	"strings"

	"git-tags/cmd"
)

//go:embed plugins/*.lua
var embeddedPlugins embed.FS

func main() {
	registerEmbeddedPlugins()
	if err := cmd.RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// registerEmbeddedPlugins 遍历内嵌的 plugins/*.lua，以文件名（去掉 .lua 后缀）
// 作为 provider 名逐个注册。新增插件只需把 .lua 文件放进 plugins/ 目录即可，
// 无需再改注册代码；内嵌插件开箱即用，用户可在项目/全局插件目录放置同名
// 插件覆盖内嵌版本。
func registerEmbeddedPlugins() {
	entries, err := embeddedPlugins.ReadDir("plugins")
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取内嵌插件目录失败: %v\n", err)
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".lua") {
			continue
		}
		content, err := embeddedPlugins.ReadFile("plugins/" + entry.Name())
		if err != nil {
			fmt.Fprintf(os.Stderr, "读取内嵌插件 %s 失败: %v\n", entry.Name(), err)
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".lua")
		cmd.RegisterEmbeddedProvider(name, string(content))
	}
}
