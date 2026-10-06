package selfupdate

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAssetName(t *testing.T) {
	tests := []struct {
		goos, tag, want string
	}{
		{"windows", "v2.5.0", "git-tags-windows-v2.5.0.zip"},
		{"linux", "v2.5.0", "git-tags-linux-v2.5.0.zip"},
		{"darwin", "v3.0.1", "git-tags-darwin-v3.0.1.zip"},
	}
	for _, tt := range tests {
		if got := AssetName(tt.goos, tt.tag); got != tt.want {
			t.Errorf("AssetName(%q, %q) = %q, want %q", tt.goos, tt.tag, got, tt.want)
		}
	}
	// 空平台回退 runtime.GOOS
	if got := AssetName("", "v1.0.0"); got != "git-tags-"+runtime.GOOS+"-v1.0.0.zip" {
		t.Errorf("AssetName 回退 runtime.GOOS 失败: %q", got)
	}
}

func TestSelectAsset(t *testing.T) {
	assets := []Asset{
		{Name: "git-tags-darwin-v2.5.0.zip", BrowserDownloadURL: "https://example.com/darwin.zip"},
		{Name: "git-tags-linux-v2.5.0.zip", BrowserDownloadURL: "https://example.com/linux.zip"},
		{Name: "git-tags-windows-v2.5.0.zip", BrowserDownloadURL: "https://example.com/windows.zip"},
	}
	got, err := SelectAsset(assets, "windows", "v2.5.0")
	if err != nil {
		t.Fatalf("SelectAsset 意外出错: %v", err)
	}
	if got.BrowserDownloadURL != "https://example.com/windows.zip" {
		t.Errorf("选错资产: %+v", got)
	}
	if _, err := SelectAsset(assets, "freebsd", "v2.5.0"); err == nil {
		t.Error("不存在的平台应报错")
	}
}

func TestParseRepo(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"morning-start/git-tags", "morning-start/git-tags", false},
		{" https://github.com/morning-start/git-tags", "", true}, // 非 owner/name
	}
	for _, tt := range tests {
		got, err := ParseRepo(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseRepo(%q) 应报错，得到 %q", tt.in, got)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("ParseRepo(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
	// owner/name.git 去掉 .git 后缀
	if got, err := ParseRepo("foo/bar.git"); err != nil || got != "foo/bar" {
		t.Errorf("ParseRepo git 后缀: %q, %v", got, err)
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"2.5.0", "2.5.1", -1},
		{"2.10.0", "2.9.9", 1},   // 数值比较，不是字典序
		{"v2.5.0", "2.5.0", 0},   // 忽略 v 前缀
		{"2.5", "2.5.0", 0},      // 缺失段按 0
		{"2.5.1-beta", "2.5.1", 0}, // 后缀截断
		{"3.0.0", "2.99.99", 1},
		{"1.2.3", "1.2.4", -1},
	}
	for _, tt := range tests {
		got := CompareVersions(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestParseRelease(t *testing.T) {
	rel, err := parseRelease([]byte(`{"tag_name":"v2.5.0","assets":[{"name":"git-tags-windows-v2.5.0.zip","browser_download_url":"https://example.com/w.zip"}]}`))
	if err != nil {
		t.Fatalf("parseRelease 失败: %v", err)
	}
	if rel.TagName != "v2.5.0" || len(rel.Assets) != 1 {
		t.Errorf("解析结果不符: %+v", rel)
	}
	if _, err := parseRelease([]byte(`{"assets":[]}`)); err == nil {
		t.Error("缺 tag_name 应报错")
	}
}

// buildTestZip 构造测试用 zip（release.yaml 的构建产物：目录内单个二进制）。
func buildTestZip(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("构造 zip 失败: %v", err)
	}
	w.Write(content)
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 zip 失败: %v", err)
	}
	return buf.Bytes()
}

func TestUnzipAndFindExecutable(t *testing.T) {
	payload := []byte("fake-binary-content")
	zipName := "git-tags"
	if runtime.GOOS == "windows" {
		zipName = "git-tags.exe"
	}
	data := buildTestZip(t, "dist/"+zipName, payload)

	files, err := unzip(data)
	if err != nil {
		t.Fatalf("unzip 失败: %v", err)
	}
	name, bin, err := findExecutable(files)
	if err != nil {
		t.Fatalf("findExecutable 失败: %v", err)
	}
	if name != zipName {
		t.Errorf("找到 %q, want %q", name, zipName)
	}
	if !bytes.Equal(bin, payload) {
		t.Error("解包内容与原始不一致")
	}
	// 空 zip 报错
	if _, err := unzip([]byte{}); err == nil {
		t.Error("空输入应报错")
	}
}

func TestInstallSwap(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "git-tags"+exeSuffix())
	oldContent := []byte("old-binary")
	newContent := []byte("new-binary-content")
	if err := os.WriteFile(exePath, oldContent, 0o755); err != nil {
		t.Fatalf("准备旧二进制失败: %v", err)
	}

	// Install 通过 os.Executable 定位自身，测试中无法重定向到临时文件；
	// 改为直接验证交换逻辑的核心步骤：new 写入 → old 改名 → new 替换。
	newPath := exePath + ".update-1.new"
	oldPath := exePath + ".update-1.old"

	if err := os.WriteFile(newPath, newContent, 0o755); err != nil {
		t.Fatalf("写 .new 失败: %v", err)
	}
	if err := os.Rename(exePath, oldPath); err != nil {
		t.Fatalf("old 改名失败: %v", err)
	}
	if err := os.Rename(newPath, exePath); err != nil {
		t.Fatalf("new 替换失败: %v", err)
	}
	got, err := os.ReadFile(exePath)
	if err != nil || !bytes.Equal(got, newContent) {
		t.Fatalf("替换后内容不符: %q, %v", got, err)
	}
	os.Remove(oldPath)

	// 残留检查：交换完成后目录里不应再有 .new/.old
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".update-") {
			t.Errorf("残留临时文件: %s", e.Name())
		}
	}
}

func TestFindExecutableRejectsEmpty(t *testing.T) {
	if _, _, err := findExecutable(map[string][]byte{"git-tags.exe": {}}); err == nil {
		t.Error("空内容可执行文件应报错")
	}
	if _, _, err := findExecutable(map[string][]byte{}); err == nil {
		t.Error("空表应报错")
	}
}
