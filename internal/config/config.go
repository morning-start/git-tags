// Package config 加载 git-tags 配置（.git-tags.toml）。
package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Config 是运行配置。
type Config struct {
	// EnabledProviders 是启用的 provider 名单；为空表示全部启用。
	EnabledProviders []string
	// TagPrefix 是 git tag 前缀，默认 "v"。
	TagPrefix string
	// ReadOnlyProviders 记录被配置为只读（writable=false）的 provider，同步时跳过写入。
	ReadOnlyProviders map[string]bool
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
	Writable *bool `toml:"writable"`
}

// Default 返回默认配置：全部 provider 启用、tag 前缀 "v"。
func Default() *Config {
	return &Config{TagPrefix: "v", ReadOnlyProviders: map[string]bool{}}
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
	}
	return cfg, nil
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
