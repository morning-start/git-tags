package provider

import (
	"fmt"
	"os"
)

// NewTauri 管理 tauri 项目的版本：Cargo.toml [package].version 与
// tauri.conf.json 顶层 version 同步；Cargo.lock 只读校验。
func NewTauri() Provider {
	return &fileProvider{
		name:     "tauri",
		priority: 80,
		targets: []Target{
			{Path: "Cargo.toml", Field: "package.version", Writable: true},
			{Path: "tauri.conf.json", Field: "version", Writable: true},
			{Path: "Cargo.lock", Field: "package.version", Writable: false},
		},
		detect: func(root string) bool {
			content, err := readFileAt(root, "Cargo.toml")
			if err != nil {
				return false
			}
			_, ok := readTOMLSectionKey(content, "package", "version")
			return ok
		},
		read: func(root string) (string, error) {
			content, err := readFileAt(root, "Cargo.toml")
			if err != nil {
				return "", err
			}
			ver, ok := readTOMLSectionKey(content, "package", "version")
			if !ok {
				return "", fmt.Errorf("Cargo.toml 未找到 [package].version")
			}

			confPath := join(root, "tauri.conf.json")
			if b, err := os.ReadFile(confPath); err == nil {
				if cv, ok := readJSONKey(string(b), "version"); ok && cv != ver {
					return "", fmt.Errorf("tauri.conf.json version(%s) 与 Cargo.toml(%s) 不一致", cv, ver)
				}
			}
			if b, err := os.ReadFile(join(root, "Cargo.lock")); err == nil {
				if lv, ok := readTOMLSectionKey(string(b), "package", "version"); ok && lv != ver {
					return "", fmt.Errorf("Cargo.lock version(%s) 与 Cargo.toml(%s) 不一致", lv, ver)
				}
			}
			return ver, nil
		},
		write: func(root, version string) error {
			cargoPath := join(root, "Cargo.toml")
			content, err := readFile(cargoPath)
			if err != nil {
				return err
			}
			updated, err := replaceTOMLSectionKey(content, "package", "version", version)
			if err != nil {
				return err
			}
			if err := writeFile(cargoPath, updated); err != nil {
				return err
			}

			confPath := join(root, "tauri.conf.json")
			if b, err := os.ReadFile(confPath); err == nil {
				updated, err := replaceJSONKey(string(b), "version", version)
				if err != nil {
					return err
				}
				if err := writeFile(confPath, updated); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
