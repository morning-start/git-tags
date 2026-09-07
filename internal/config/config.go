// Package config 加载 git-tags 配置（.git-tags.toml）。
package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// Carrier 描述一个版本载体（Lua provider 探测/写入的目标）。
// Kind 取值：plain（纯文本文件，如 VERSION）| govar（Go var/const 赋值，如 Version = "x"）。
type Carrier struct {
	Kind  string // "plain" | "govar"
	Path  string // 项目内相对路径
	Field string // govar 的变量名（如 "Version"）；plain 为空
}

// Config 是运行配置。
type Config struct {
	// EnabledProviders 是启用的 provider 名单；为空表示全部启用。
	EnabledProviders []string
	// TagPrefix 是 git tag 前缀，默认 "v"。
	TagPrefix string
	// ReadOnlyProviders 记录被配置为只读（writable=false）的 provider，同步时跳过写入。
	ReadOnlyProviders map[string]bool
	// ProviderCarriers 记录 [provider.<name>] 配置的自定义版本载体；键为 provider 名。
	// 未配置的 provider 不在此表内（Lua 插件回退到自己的内置默认）。
	ProviderCarriers map[string][]Carrier
}

// rawConfig 是 .git-tags.toml 的磁盘结构。
type rawConfig struct {
	Core coreConfig            `toml:"core"`
	Prov map[string]provConfig `toml:"provider"`
}

type coreConfig struct {
	Enabled   []string `toml:"enabled"`
	TagPrefix string   `toml:"tag_prefix"`
}

type provConfig struct {
	Writable *bool    `toml:"writable"`
	Carriers []string `toml:"carriers"`
}

// Default 返回默认配置：全部 provider 启用、tag 前缀 "v"。
func Default() *Config {
	return &Config{
		TagPrefix:        "v",
		ReadOnlyProviders: map[string]bool{},
		ProviderCarriers: map[string][]Carrier{},
	}
}

// Load 读取配置文件；文件不存在时返回默认配置。
// path 为空时默认读取当前目录下的 .git-tags.toml。
func Load(path string) (*Config, error) {
	if path == "" {
		path = ".git-tags.toml"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return nil, fmt.Errorf("读取配置 %s 失败: %w", path, err)
	}

	var raw rawConfig
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("解析配置 %s 失败: %w", path, err)
	}

	cfg := Default()
	if raw.Core.TagPrefix != "" {
		cfg.TagPrefix = raw.Core.TagPrefix
	}
	if raw.Core.Enabled != nil {
		cfg.EnabledProviders = raw.Core.Enabled
	}
	for name, pc := range raw.Prov {
		if pc.Writable != nil && !*pc.Writable {
			cfg.ReadOnlyProviders[name] = true
		}
		if len(pc.Carriers) > 0 {
			carriers, err := parseCarriers(pc.Carriers)
			if err != nil {
				return nil, fmt.Errorf("provider %s 的 carriers 配置错误: %w", name, err)
			}
			cfg.ProviderCarriers[name] = carriers
		}
	}
	return cfg, nil
}

// parseCarriers 解析 carriers 配置项。每项格式 "kind:path" 或 "kind:path:field"：
//   - kind 为 plain（纯文本）或 govar（Go 变量赋值）
//   - path 为项目内相对路径
//   - field 为 govar 的变量名，缺省 "Version"
func parseCarriers(items []string) ([]Carrier, error) {
	out := make([]Carrier, 0, len(items))
	for _, item := range items {
		parts := strings.SplitN(item, ":", 3)
		if len(parts) < 2 {
			return nil, fmt.Errorf("格式应为 kind:path[:field]，得到 %q", item)
		}
		kind, path := parts[0], parts[1]
		if kind != "plain" && kind != "govar" {
			return nil, fmt.Errorf("kind %q 无效（应为 plain 或 govar）", kind)
		}
		if path == "" {
			return nil, fmt.Errorf("路径不能为空: %q", item)
		}
		c := Carrier{Kind: kind, Path: path}
		if kind == "govar" {
			c.Field = "Version"
			if len(parts) == 3 && parts[2] != "" {
				c.Field = parts[2]
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// ProviderEnabled 判断 provider 是否被启用。
func (c *Config) ProviderEnabled(name string) bool {
	if len(c.EnabledProviders) == 0 {
		return true
	}
	for _, n := range c.EnabledProviders {
		if n == name {
			return true
		}
	}
	return false
}

// ProviderWritable 判断 provider 是否可写（配置为只读时返回 false）。
func (c *Config) ProviderWritable(name string) bool {
	return !c.ReadOnlyProviders[name]
}
