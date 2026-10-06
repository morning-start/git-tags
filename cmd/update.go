package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"git-tags/internal/selfupdate"
)

// defaultRepoFallback 是 git remote 推断失败时的兜底仓库。
const defaultRepoFallback = selfupdate.DefaultRepo

// inferRepo 推断要升级的仓库：--repo 显式指定 > git remote origin（仅 github.com）
// > 内置默认仓库。
func inferRepo(flagRepo string) (string, error) {
	if flagRepo != "" {
		return selfupdate.ParseRepo(flagRepo)
	}
	if out, err := gitRemoteOriginURL(); err == nil {
		if repo, ok := githubRepoFromRemote(out); ok {
			return repo, nil
		}
	}
	return defaultRepoFallback, nil
}

// gitRemoteOriginURL 返回 origin remote 的 URL（不在 git 仓库或无 origin 时报错）。
func gitRemoteOriginURL() (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("读取 git remote origin 失败: %s", strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// githubRepoFromRemote 从 git remote URL 提取 owner/name（仅支持 github.com）。
// 兼容 ssh（git@github.com:owner/name.git）与 https 两种形式。
func githubRepoFromRemote(url string) (string, bool) {
	url = strings.TrimSpace(url)
	if i := strings.Index(url, "github.com"); i >= 0 {
		rest := strings.TrimPrefix(url[i:], "github.com")
		rest = strings.Trim(rest, ":/")
		rest = strings.TrimSuffix(rest, ".git")
		parts := strings.Split(rest, "/")
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return parts[0] + "/" + parts[1], true
		}
	}
	return "", false
}

// confirm 输出提示并等待 stdin 输入 y/yes 确认；非交互（EOF）时视为拒绝。
func confirm(prompt string) bool {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

var UpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Self-update from the latest GitHub release",
	Long: "Check the latest release on GitHub and replace the current binary if a newer version exists.\n" +
		"Version lookup prefers the gh CLI and falls back to the GitHub REST API.",
	RunE: func(cmd *cobra.Command, args []string) error {
		checkOnly, _ := cmd.Flags().GetBool("check")
		assumeYes, _ := cmd.Flags().GetBool("yes")
		flagRepo, _ := cmd.Flags().GetString("repo")

		repo, err := inferRepo(flagRepo)
		if err != nil {
			return err
		}

		fmt.Printf("当前版本: %s\n", RootCmd.Version)
		fmt.Printf("查询 %s 最新 release（优先 gh）...\n", repo)
		rel, err := selfupdate.LatestRelease(repo)
		if err != nil {
			return fmt.Errorf("查询最新版本失败: %w", err)
		}
		latest := strings.TrimPrefix(rel.TagName, "v")
		fmt.Printf("最新版本: %s\n", rel.TagName)

		switch selfupdate.CompareVersions(latest, RootCmd.Version) {
		case 0:
			fmt.Println("已是最新版本。")
			selfupdate.CleanupStaleResidue()
			return nil
		case -1:
			fmt.Println("本地版本比最新 release 还新（可能是未发布的开发构建），不执行升级。")
			selfupdate.CleanupStaleResidue()
			return nil
		}

		fmt.Printf("发现新版本 %s → %s\n", RootCmd.Version, rel.TagName)
		if checkOnly {
			fmt.Println("（--check 只检查，未下载。去掉 --check 或加 --yes 执行升级。）")
			return nil
		}
		if !assumeYes && !confirm("是否下载并安装？[y/N] ") {
			fmt.Println("已取消。")
			return nil
		}

		asset, err := selfupdate.SelectAsset(rel.Assets, "", rel.TagName)
		if err != nil {
			return err
		}
		fmt.Printf("下载 %s ...\n", asset.Name)
		_, bin, err := selfupdate.ConfirmDownload(asset)
		if err != nil {
			return err
		}
		fmt.Printf("校验通过（%d 字节），替换当前二进制...\n", len(bin))
		if err := selfupdate.Install(bin); err != nil {
			return err
		}
		fmt.Printf("升级完成：%s → %s\n", RootCmd.Version, rel.TagName)
		if exe, err := os.Executable(); err == nil {
			fmt.Printf("新版本位于 %s（路径已缓存时重开终端生效）\n", filepath.Clean(exe))
		}
		return nil
	},
}

func init() {
	UpdateCmd.Flags().Bool("check", false, "Only check for a newer release; do not download or install")
	UpdateCmd.Flags().BoolP("yes", "y", false, "Skip the confirmation prompt and install directly")
	UpdateCmd.Flags().String("repo", "", "GitHub repository to upgrade from (owner/name); default inferred from git remote")
	UpdateCmd.GroupID = "plugin"
	RootCmd.AddCommand(UpdateCmd)
}
