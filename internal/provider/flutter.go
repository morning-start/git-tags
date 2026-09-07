package provider

import (
	"fmt"
	"os"
	"strings"
)

// NewFlutter 管理 flutter 项目的版本：pubspec.yaml 顶层 version（X.Y.Z+build）。
// 与权威源比较时剥离 +build（git tag 不带 build 号）；写入时保留原 build 号，
// 只有显式传入带 build 的版本才整体替换。
func NewFlutter() Provider {
	return &fileProvider{
		name:     "flutter",
		priority: 75,
		targets: []Target{
			{Path: "pubspec.yaml", Field: "version", Writable: true},
			{Path: "pubspec.lock", Field: "packages.root.version", Writable: false},
		},
		detect: func(root string) bool {
			content, err := readFileAt(root, "pubspec.yaml")
			if err != nil {
				return false
			}
			_, ok := readYAMLTopKey(content, "version")
			return ok
		},
		read: func(root string) (string, error) {
			content, err := readFileAt(root, "pubspec.yaml")
			if err != nil {
				return "", err
			}
			ver, ok := readYAMLTopKey(content, "version")
			if !ok {
				return "", fmt.Errorf("pubspec.yaml 未找到顶层 version")
			}
			base := strings.SplitN(ver, "+", 2)[0]
			if b, err := os.ReadFile(join(root, "pubspec.lock")); err == nil {
				if lv, ok := readYAMLRootVersion(string(b)); ok {
					lbase := strings.SplitN(lv, "+", 2)[0]
					if lbase != base {
						return "", fmt.Errorf("pubspec.lock version(%s) 与 pubspec.yaml(%s) 不一致", lv, ver)
					}
				}
			}
			return base, nil
		},
		write: func(root, version string) error {
			path := join(root, "pubspec.yaml")
			content, err := readFile(path)
			if err != nil {
				return err
			}
			newVal := version
			if old, ok := readYAMLTopKey(content, "version"); ok {
				// 新版本未带 build 号时，沿用旧 build 号（flutter 约定 build 号独立递增）
				if !strings.Contains(version, "+") && strings.Contains(old, "+") {
					newVal = version + "+" + strings.SplitN(old, "+", 2)[1]
				}
			}
			updated, err := replaceYAMLTopKey(content, "version", newVal)
			if err != nil {
				return err
			}
			return writeFile(path, updated)
		},
	}
}
