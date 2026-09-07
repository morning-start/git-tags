# git-tags 工具

## 简介
`git-tags` 是一个项目版本与 Git 标签管理工具，把**一个版本同步到多个项目文件**，不用再手工逐个修改：

- **git tag 是唯一权威源**（canonical version）
- 内置支持常见项目类型：**tauri**（Cargo.toml + tauri.conf.json）、**uv python**（pyproject.toml）、**flutter**（pubspec.yaml）、**node**（package.json）
- **Lua 插件**让任意项目类型都能接入版本同步，无需改 Go 代码
- **hook 插件**可在版本递增流程前后挂自定义逻辑（如自动更新 CHANGELOG）

## 安装
本项目依赖 Go 1.24.4 及以下库：
- github.com/Masterminds/semver/v3 v3.3.1
- github.com/spf13/cobra v1.6.1
- github.com/BurntSushi/toml v1.4.0
- github.com/yuin/gopher-lua v1.1.1

克隆仓库后，在项目根目录下运行以下命令进行构建：
```bash
go build -o git-tags main.go
```

## 命令说明
### ls
显示所有标签。
```bash
./git-tags ls
```

### patch / minor / major
递增版本号并同步到所有项目文件，然后创建新标签。
```bash
./git-tags patch
./git-tags minor
./git-tags major
```

附加参数：

| 参数 | 说明 |
|------|------|
| `-p, --push` | 创建标签后推送到远程 |
| `--dry-run` | 预览每个文件的改动（旧值 → 新值），不实际写入 |
| `--commit` | 把版本文件改动与标签提交到同一个 commit |
| `--no-tag` | 只改版本文件，不创建标签 |

### check
校验所有激活 provider 的版本与权威源 git tag 是否一致，报告不一致项。
```bash
./git-tags check
```

### sync
以权威源（最新 git tag）版本写回所有 provider 的可写文件。
```bash
./git-tags sync            # 实际写入
./git-tags sync --dry-run  # 预览
```

### set
显式设置版本并同步到所有 provider，可选创建标签。
```bash
./git-tags set 1.4.0
./git-tags set 1.4.0 --no-tag
```

### push
推送标签到远程仓库，可使用 `-b` 参数指定分支，默认为 `origin`。
```bash
./git-tags push -b origin
```

### del
删除最新标签，可使用 `-b` 参数指定远程分支删除远程标签，默认为 `origin`。若不指定 `-b` 参数，则删除本地标签。
```bash
./git-tags del -b origin
```

### plugins
列出发现的 Lua 插件并校验插件脚本。
```bash
./git-tags plugins list
./git-tags plugins validate            # 校验全部已发现插件
./git-tags plugins validate my.lua     # 校验指定文件
```

## 配置文件

项目根目录下可选的 `.git-tags.toml`：

```toml
[core]
# enabled = ["git", "tauri", "uv", "flutter", "node"]  # 默认全部启用
tag_prefix = "v"

# 将某 provider 标记为只读：只校验一致性，不参与写入
[provider.uv]
writable = false
```

注意事项：

- 锁文件（`Cargo.lock`、`uv.lock`、`pubspec.lock`、`package-lock.json`）是**只读校验**：只检查一致性、绝不由本工具改写——请交给工具链（`cargo build`、`uv sync`、`flutter pub get`）重新生成后再 `check`。
- Flutter 版本（`X.Y.Z+build`）：写入时保留 build 号，递增时只比较基础版本。

## Lua 插件

插件存放位置：

- 全局：`%APPDATA%/git-tags/plugins/`（Windows）或 `~/.config/git-tags/plugins/`（Linux/macOS）
- 项目级：`<仓库根>/.git-tags/plugins/`

每个插件是一个声明了全局 `plugin` 表的 `.lua` 文件。同名插件项目级覆盖全局；插件按 `priority` 降序执行；损坏的插件会被跳过并标记错误（`plugins list` 可见），不会阻塞其他插件。

### provider 插件

为新的项目类型接入版本同步：

```lua
plugin = {
  name = "myapp",            -- 唯一标识
  type = "provider",
  priority = 50,             -- 默认 50
  description = "VERSION 文件版本源",
}

function plugin.detect(project)
  local ok, _ = pcall(gt.read_file, "VERSION")
  return ok
end

function plugin.read(project)
  return gt.read_file("VERSION"):match("^%s*([^%s\n]+)")
end

function plugin.write(project, version)
  gt.write_file("VERSION", version .. "\n")
end
```

### hook 插件

在 bump 流程中挂回调（`pre_bump` → 写文件 → `post_bump` → `pre_tag` → 建 tag → `post_tag`）：

```lua
plugin = {
  name = "changelog",
  type = "hook",
  priority = 10,
}

function plugin.pre_bump(from, to)
  local content = gt.read_file("CHANGELOG.md")
  gt.write_file("CHANGELOG.md", string.format("## %s\n\n", to) .. content)
end
```

### 宿主 API（`gt`）

| API | 说明 |
|-----|------|
| `gt.project` | 表，含 `root`（项目根绝对路径） |
| `gt.read_file(path)` / `gt.write_file(path, content)` | 读写**项目内**文件（路径白名单） |
| `gt.log(msg)` | 输出用户可见日志 |
| `gt.semver.parse(v)` | 解析 → `{major, minor, patch, prerelease, metadata, str}` 或 `nil` |
| `gt.semver.inc(v, "patch"\|"minor"\|"major")` | 递增 → 新版本字符串 |
| `gt.semver.compare(a, b)` | `-1` / `0` / `1` |
| `gt.git.latest_tag()` | 最新 git tag（含前缀） |

沙箱移除了 `os`、`io`、`package`、`debug` 库及危险的 base 函数（`dofile`、`loadfile`、`load`、`loadstring`）；脚本超过 5 秒会被终止，panic 不会导致工具崩溃。

## 贡献
如果你有任何改进建议或发现了 bug，欢迎提交 issue 或 pull request。

## 许可证
本项目采用 [MIT 许可证](LICENSE)。
