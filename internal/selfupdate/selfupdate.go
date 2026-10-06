// Package selfupdate 实现 git-tags 的自我升级：查询 GitHub 最新 release，
// 下载对应平台的 zip 资产并原子替换当前二进制。
//
// 查询优先使用 gh CLI（gh api repos/<repo>/releases/latest，继承本机登录态、
// 规避匿名限流），gh 不可用或调用失败时回退 GitHub REST API。
package selfupdate

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DefaultRepo 是仓库推断失败时的兜底仓库。
const DefaultRepo = "morning-start/git-tags"

// apiTimeout 限制单次 HTTP 请求时长，避免网络挂起卡住整个命令。
const apiTimeout = 30 * time.Second

// DownloadTimeout 限制资产下载时长（资产约 4~5 MB，30s 内可完成，留足余量）。
const DownloadTimeout = 5 * time.Minute

// githubToken 返回 GitHub 访问令牌，供 REST 兜底通道提升限流额度：
// 依次尝试 gh CLI 登录态（gh auth token）、GITHUB_TOKEN 环境变量、
// git 凭据管理器（git credential fill，gh 登录时令牌通常存于此）；
// 全部不可用时返回空串，请求退回匿名额度（60 次/小时/IP）。
func githubToken() string {
	if out, err := exec.Command("gh", "auth", "token").Output(); err == nil {
		if tok := strings.TrimSpace(string(out)); tok != "" {
			return tok
		}
	}
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		return tok
	}
	return tokenFromGitCredential()
}

// tokenFromGitCredential 通过 git credential fill 向凭据管理器请求
// github.com 的令牌（gh auth login 默认把令牌交给 git credential manager 托管，
// hosts.yml 里不落盘，因此 gh 不在 PATH 时这是最可靠的兜底）。
func tokenFromGitCredential() string {
	cmd := exec.Command("git", "credential", "fill")
	cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\n\n")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "password=") {
			return strings.TrimSpace(line[len("password="):])
		}
	}
	return ""
}

// Release 是 releases/latest 返回中本工具关心的字段子集。
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset 是 release 的一个可下载资产。
type Asset struct {
	Name              string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// LatestRelease 查询仓库最新 release：优先 gh CLI，失败回退 REST API。
func LatestRelease(repo string) (*Release, error) {
	if rel, err := latestViaGH(repo); err == nil {
		return rel, nil
	}
	return latestViaREST(repo)
}

// latestViaGH 通过 gh CLI 查询（用户要求：gh 可用时优先）。
func latestViaGH(repo string) (*Release, error) {
	cmd := exec.Command("gh", "api", "repos/"+repo+"/releases/latest")
	cmd.Env = append(os.Environ(), "GH_REPO="+repo)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh api 查询失败: %w", err)
	}
	return parseRelease(output)
}

// latestViaREST 通过 GitHub REST API 查询；带 gh 登录态令牌（如有）提升限流额度。
func latestViaREST(repo string) (*Release, error) {
	url := "https://api.github.com/repos/" + repo + "/releases/latest"
	client := &http.Client{Timeout: apiTimeout}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	// 显式 Accept 与 API 版本头，与 gh api 的默认行为保持一致
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if tok := githubToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("REST API 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("REST API 返回 %s（%s）", resp.Status, url)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	return parseRelease(body)
}

// parseRelease 解析 releases/latest JSON，校验必要字段存在。
func parseRelease(data []byte) (*Release, error) {
	var rel Release
	if err := json.Unmarshal(data, &rel); err != nil {
		return nil, fmt.Errorf("解析 release JSON 失败: %w", err)
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("release JSON 缺少 tag_name")
	}
	return &rel, nil
}

// AssetName 生成平台资产名，与 .github/workflows/release.yaml 的
// "git-tags-${{ matrix.goos }}-${{ github.ref_name }}.zip" 严格对齐。
// goos 空缺时用 runtime.GOOS。
func AssetName(goos, tag string) string {
	if goos == "" {
		goos = runtime.GOOS
	}
	return "git-tags-" + goos + "-" + tag + ".zip"
}

// SelectAsset 从资产列表中选出当前平台对应的 zip；找不到时返回明确错误。
func SelectAsset(assets []Asset, goos, tag string) (*Asset, error) {
	want := AssetName(goos, tag)
	for i := range assets {
		if assets[i].Name == want {
			return &assets[i], nil
		}
	}
	names := make([]string, 0, len(assets))
	for _, a := range assets {
		names = append(names, a.Name)
	}
	return nil, fmt.Errorf("未找到平台资产 %s（release 资产: %s）", want, strings.Join(names, ", "))
}

// ParseRepo 校验 "owner/name" 形式的仓库标识。
func ParseRepo(s string) (string, error) {
	s = strings.TrimSpace(strings.TrimSuffix(s, ".git"))
	parts := strings.Split(s, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("仓库标识格式应为 owner/name，得到 %q", s)
	}
	return parts[0] + "/" + parts[1], nil
}

// CompareVersions 比较两个不带 v 前缀的语义化版本（缺失段按 0 补齐）。
// 返回 -1/0/1；解析失败时退回字符串比较。
func CompareVersions(a, b string) int {
	pa, poka := parseSemver(a)
	pb, pokb := parseSemver(b)
	if !poka || !pokb {
		return strings.Compare(a, b)
	}
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

// parseSemver 提取 major.minor.patch 三段数字；容许前缀（如 "v"）与后缀（如 "-beta"）。
func parseSemver(s string) ([3]int, bool) {
	var out [3]int
	// 跳过开头的非数字前缀（v、V 等）
	i := 0
	for i < len(s) && (s[i] < '0' || s[i] > '9') {
		i++
	}
	s = s[i:]
	// 截掉语义化版本后的附加段（-beta、+build）
	if j := strings.IndexAny(s, "-+"); j >= 0 {
		s = s[:j]
	}
	parts := strings.Split(s, ".")
	if len(parts) == 0 || parts[0] == "" {
		return out, false
	}
	for k := 0; k < 3 && k < len(parts); k++ {
		n := 0
		for _, c := range parts[k] {
			if c < '0' || c > '9' {
				return out, false
			}
			n = n*10 + int(c-'0')
		}
		out[k] = n
	}
	return out, true
}

// exeSuffix 返回当前平台可执行文件后缀（windows 为 ".exe"，其余为空）。
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// findExecutable 在解包内容中找到主二进制（唯一可执行文件，windows 下为 .exe）。
func findExecutable(files map[string][]byte) (string, []byte, error) {
	for name, data := range files {
		base := filepath.Base(name)
		if runtime.GOOS == "windows" {
			if strings.EqualFold(filepath.Ext(base), ".exe") && len(data) > 0 {
				return base, data, nil
			}
			continue
		}
		// Unix：zip 不保留权限位时按平台缺省名兜底，其次任意非空无后缀文件
		if base == "git-tags" && len(data) > 0 {
			return base, data, nil
		}
		if filepath.Ext(base) == "" && !strings.Contains(base, ".") && len(data) > 0 {
			return base, data, nil
		}
	}
	return "", nil, fmt.Errorf("zip 包内未找到可执行文件")
}

// ConfirmDownload 下载资产 zip、解包、校验并返回 (二进制文件名, 内容)。
// 只做下载与内存校验，不触碰磁盘上的现有二进制。
func ConfirmDownload(a *Asset) (string, []byte, error) {
	client := &http.Client{Timeout: DownloadTimeout}
	resp, err := client.Get(a.BrowserDownloadURL)
	if err != nil {
		return "", nil, fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("下载返回 %s（%s）", resp.Status, a.BrowserDownloadURL)
	}
	// 限制读入大小（release 资产 ~4.5MB，64MB 上限防异常巨包撑爆内存）
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return "", nil, fmt.Errorf("读取下载内容失败: %w", err)
	}
	files, err := unzip(data)
	if err != nil {
		return "", nil, err
	}
	name, bin, err := findExecutable(files)
	if err != nil {
		return "", nil, err
	}
	return name, bin, nil
}

// unzip 解析 zip 字节流为 name → 内容表；拒绝条目名含 ".."（路径穿越）。
func unzip(data []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("解析 zip 失败: %w", err)
	}
	files := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		if strings.Contains(f.Name, "..") || filepath.IsAbs(f.Name) {
			continue // 路径穿越防护：跳过可疑条目
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("读取 zip 条目 %s 失败: %w", f.Name, err)
		}
		content, err := io.ReadAll(io.LimitReader(rc, 64<<20))
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("读取 zip 条目 %s 内容失败: %w", f.Name, err)
		}
		files[f.Name] = content
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("zip 包为空")
	}
	return files, nil
}

// Install 把新二进制写入 exe 所在目录并替换现有文件。
// 策略（Windows 运行中文件无法被覆盖/删除，必须先腾位）：
//  1. 写 <exe>.update-<pid>.new；
//  2. Windows：旧文件改名 <exe>.update-<pid>.old → new 改名为正式名 → 删 old；
//     Unix：直接 overwrite（rename 原子替换）；
//  3. 任一步失败即报错，.new 文件保留供排查，不破坏现有二进制；
//  4. Windows 下 old 清理失败不报错（运行中的进程映像可能短暂锁定），
//     改为启动后自清：每次 Install 前先扫掉同目录残留的历史 .old/.new 文件。
func Install(bin []byte) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位当前二进制失败: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("解析二进制真实路径失败: %w", err)
	}
	cleanupStale(exe)
	newPath := exe + fmt.Sprintf(".update-%d.new", os.Getpid())
	oldPath := exe + fmt.Sprintf(".update-%d.old", os.Getpid())

	if err := os.WriteFile(newPath, bin, 0o755); err != nil {
		return fmt.Errorf("写入新二进制 %s 失败: %w", newPath, err)
	}

	if runtime.GOOS == "windows" {
		// 旧文件改名腾位；改名成功后才动 new
		if err := os.Rename(exe, oldPath); err != nil {
			return fmt.Errorf("旧二进制改名失败（是否被占用？）: %w", err)
		}
	}
	if err := os.Rename(newPath, exe); err != nil {
		// Windows 下尽量回滚：old 改回原名
		if runtime.GOOS == "windows" {
			if rbErr := os.Rename(oldPath, exe); rbErr != nil {
				return fmt.Errorf("新二进制替换失败且回滚失败: %v（回滚错误: %v，旧二进制保留在 %s）", err, rbErr, oldPath)
			}
		}
		return fmt.Errorf("新二进制替换失败: %w", err)
	}
	if runtime.GOOS == "windows" {
		if err := os.Remove(oldPath); err != nil {
			// 清理失败不影响升级结果（通常因本进程映像仍被占用）；
			// 残留文件由下一次升级的 cleanupStale 兜底清除。
			fmt.Fprintf(os.Stderr, "提示: 旧文件清理失败（%v），将在下次升级时自动清理\n", err)
		}
	}
	return nil
}

// cleanupStale 清除 exe 同目录下历史升级残留的 .update-*.old / .update-*.new
// （Windows 上刚被替换的进程映像文件当时可能删不掉，事后可删）。
func cleanupStale(exe string) {
	dir := filepath.Dir(exe)
	base := filepath.Base(exe)
	matches, err := filepath.Glob(filepath.Join(dir, base+".update-*"))
	if err != nil {
		return
	}
	for _, m := range matches {
		if m == exe {
			continue
		}
		if strings.HasSuffix(m, ".old") || strings.HasSuffix(m, ".new") {
			os.Remove(m)
		}
	}
}

// CleanupStaleResidue 清除当前可执行文件目录下的历史升级残留文件，
// 供命令层在确认无需升级（已是最新/本地更新）时也保持目录整洁。
func CleanupStaleResidue() {
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			cleanupStale(resolved)
		}
	}
}
