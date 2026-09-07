package provider

import (
	"fmt"
	"os"
	"path/filepath"
)

// tauriLayout 解析 tauri 文件的实际位置：标准布局为 src-tauri/ 子目录，
// 兼容根目录布局；按文件逐一判断（存在即采用）。
func tauriLayout(root string) map[string]string {
	m := map[string]string{}
	for _, n := range []string{"Cargo.toml", "tauri.conf.json", "Cargo.lock"} {
		if fileExists(root, filepath.Join("src-tauri", n)) {
			m[n] = filepath.Join("src-tauri", n)
		} else {
			m[n] = n
		}
	}
	return m
}

// NewTauri 管理 tauri 项目的版本：Cargo.toml [package].version 与
// tauri.conf.json 顶层 version 同步；Cargo.lock 只读校验。
// 支持标准 src-tauri/ 子目录布局与根目录布局。
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
			layout := tauriLayout(root)
			content, err := readFileAt(root, layout["Cargo.toml"])
			if err != nil {
				return false
			}
			_, ok := readTOMLSectionKey(content, "package", "version")
			return ok
		},
		read: func(root string) (string, error) {
			layout := tauriLayout(root)
			content, err := readFileAt(root, layout["Cargo.toml"])
			if err != nil {
				return "", err
			}
			ver, ok := readTOMLSectionKey(content, "package", "version")
			if !ok {
				return "", fmt.Errorf("%s 未找到 [package].version", layout["Cargo.toml"])
			}

			if b, err := os.ReadFile(join(root, layout["tauri.conf.json"])); err == nil {
				if cv, ok := readJSONKey(string(b), "version"); ok && cv != ver {
					return "", fmt.Errorf("%s version(%s) 与 %s(%s) 不一致", layout["tauri.conf.json"], cv, layout["Cargo.toml"], ver)
				}
			}
			if b, err := os.ReadFile(join(root, layout["Cargo.lock"])); err == nil {
				if lv, ok := readTOMLSectionKey(string(b), "package", "version"); ok && lv != ver {
					return "", fmt.Errorf("%s version(%s) 与 %s(%s) 不一致", layout["Cargo.lock"], lv, layout["Cargo.toml"], ver)
				}
			}
			return ver, nil
		},
		write: func(root, version string) error {
			layout := tauriLayout(root)
			cargoPath := join(root, layout["Cargo.toml"])
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

			confPath := join(root, layout["tauri.conf.json"])
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
		previewFunc: func(ctx *Context, version string) ([]TargetChange, error) {
			layout := tauriLayout(ctx.Project.Root)
			var out []TargetChange
			for _, t := range []Target{
				{Path: layout["Cargo.toml"], Field: "package.version"},
				{Path: layout["tauri.conf.json"], Field: "version"},
			} {
				content, err := readFileAt(ctx.Project.Root, t.Path)
				if err != nil {
					continue
				}
				old, ok := readTargetValue(content, t)
				if !ok {
					continue
				}
				out = append(out, TargetChange{Path: t.Path, Old: old, New: version})
			}
			return out, nil
		},
	}
}
