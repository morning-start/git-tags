package provider

import (
	"fmt"
	"os"
)

// NewNode 管理 node 项目的版本：package.json 顶层 version；
// package-lock.json 只读校验。
func NewNode() Provider {
	return &fileProvider{
		name:     "node",
		priority: 65,
		targets: []Target{
			{Path: "package.json", Field: "version", Writable: true},
			{Path: "package-lock.json", Field: "version", Writable: false},
		},
		detect: func(root string) bool {
			content, err := readFileAt(root, "package.json")
			if err != nil {
				return false
			}
			_, ok := readJSONKey(content, "version")
			return ok
		},
		read: func(root string) (string, error) {
			content, err := readFileAt(root, "package.json")
			if err != nil {
				return "", err
			}
			ver, ok := readJSONKey(content, "version")
			if !ok {
				return "", fmt.Errorf("package.json 未找到顶层 version")
			}
			if b, err := os.ReadFile(join(root, "package-lock.json")); err == nil {
				if lv, ok := readJSONKey(string(b), "version"); ok && lv != ver {
					return "", fmt.Errorf("package-lock.json version(%s) 与 package.json(%s) 不一致", lv, ver)
				}
			}
			return ver, nil
		},
		write: func(root, version string) error {
			path := join(root, "package.json")
			content, err := readFile(path)
			if err != nil {
				return err
			}
			updated, err := replaceJSONKey(content, "version", version)
			if err != nil {
				return err
			}
			return writeFile(path, updated)
		},
	}
}
