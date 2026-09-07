package provider

import (
	"fmt"
	"os"
)

// NewUV 管理 uv python 项目的版本：pyproject.toml [project].version；
// uv.lock 只读校验。
func NewUV() Provider {
	return &fileProvider{
		name:     "uv",
		priority: 70,
		targets: []Target{
			{Path: "pyproject.toml", Field: "project.version", Writable: true},
			{Path: "uv.lock", Field: "package.version", Writable: false},
		},
		detect: func(root string) bool {
			content, err := readFileAt(root, "pyproject.toml")
			if err != nil {
				return false
			}
			_, ok := readTOMLSectionKey(content, "project", "version")
			return ok
		},
		read: func(root string) (string, error) {
			content, err := readFileAt(root, "pyproject.toml")
			if err != nil {
				return "", err
			}
			ver, ok := readTOMLSectionKey(content, "project", "version")
			if !ok {
				return "", fmt.Errorf("pyproject.toml 未找到 [project].version")
			}
			if b, err := os.ReadFile(join(root, "uv.lock")); err == nil {
				if lv, ok := readTOMLSectionKey(string(b), "package", "version"); ok && lv != ver {
					return "", fmt.Errorf("uv.lock version(%s) 与 pyproject.toml(%s) 不一致", lv, ver)
				}
			}
			return ver, nil
		},
		write: func(root, version string) error {
			path := join(root, "pyproject.toml")
			content, err := readFile(path)
			if err != nil {
				return err
			}
			updated, err := replaceTOMLSectionKey(content, "project", "version", version)
			if err != nil {
				return err
			}
			return writeFile(path, updated)
		},
	}
}
