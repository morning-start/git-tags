package provider

import (
	"os"
	"path/filepath"
	"testing"
)

// providerCase 描述一个内置 provider 的 fixture/golden 测试用例。
type providerCase struct {
	name         string
	newProvider  func() Provider
	fixture      string
	origVersion  string
	newVersion   string
	writable     []string // 写入后应与 golden 对比的文件
	readOnly     []string // 只读文件：不应被修改（应仍含 origVersion）
}

func TestFileProviders(t *testing.T) {
	cases := []providerCase{
		{
			name: "tauri", newProvider: NewTauri, fixture: "tauri",
			origVersion: "1.3.0", newVersion: "1.4.0",
			writable: []string{"Cargo.toml", "tauri.conf.json"},
			readOnly: []string{"Cargo.lock"},
		},
		{
			name: "uv", newProvider: NewUV, fixture: "uv",
			origVersion: "0.2.0", newVersion: "0.3.1",
			writable: []string{"pyproject.toml"},
			readOnly: []string{"uv.lock"},
		},
		{
			name: "flutter", newProvider: NewFlutter, fixture: "flutter",
			origVersion: "1.0.0", newVersion: "2.1.0",
			writable: []string{"pubspec.yaml"},
			readOnly: []string{"pubspec.lock"},
		},
		{
			name: "node", newProvider: NewNode, fixture: "node",
			origVersion: "0.5.0", newVersion: "1.9.9",
			writable: []string{"package.json"},
			readOnly: []string{"package-lock.json"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := copyFixture(t, tc.fixture)
			p := tc.newProvider()
			ctx := &Context{Project: &Project{Root: root}}

			if !p.Detect(ctx) {
				t.Fatalf("Detect(%s) = false, want true", tc.fixture)
			}

			got, err := p.Read(ctx)
			if err != nil {
				t.Fatalf("Read() error: %v", err)
			}
			if got != tc.origVersion {
				t.Fatalf("Read() = %q, want %q", got, tc.origVersion)
			}

			if err := p.Write(ctx, tc.newVersion); err != nil {
				t.Fatalf("Write() error: %v", err)
			}

			// 可写文件与 golden 一致
			for _, f := range tc.writable {
				got := readTestFile(t, filepath.Join(root, f))
				want := readTestFile(t, filepath.Join("testdata/golden", tc.fixture, f))
				if got != want {
					t.Errorf("%s 内容与 golden 不一致:\n--- got ---\n%s\n--- want ---\n%s", f, got, want)
				}
			}

			// 只读文件未被修改（仍含原版本）
			for _, f := range tc.readOnly {
				content := readTestFile(t, filepath.Join(root, f))
				if !containsVersion(content, tc.origVersion) {
					t.Errorf("%s 应保持只读且仍含版本 %s", f, tc.origVersion)
				}
			}
		})
	}
}

func TestDetectNegative(t *testing.T) {
	empty := t.TempDir()
	ctx := &Context{Project: &Project{Root: empty}}
	for _, newProvider := range []func() Provider{NewTauri, NewUV, NewFlutter, NewNode} {
		if newProvider().Detect(ctx) {
			t.Errorf("空目录上 Detect() = true, want false")
		}
	}
}

// copyFixture 把 fixture 目录复制到临时目录，返回临时目录路径。
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join("testdata/fixtures", name)
	dst := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("读取 fixture %s 失败: %v", name, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644); err != nil {
			t.Fatalf("写入 %s 失败: %v", e.Name(), err)
		}
	}
	return dst
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return string(data)
}

func containsVersion(content, version string) bool {
	for i := 0; i+len(version) <= len(content); i++ {
		if content[i:i+len(version)] == version {
			return true
		}
	}
	return false
}
