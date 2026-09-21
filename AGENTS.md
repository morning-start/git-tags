# AGENTS.md — git-tags 仓库协作指南

给后续 AI Agent / 开发者：先读本文再动手。**最常见的任务是"给某种项目类型加版本同步支持"——那通常只需要在 `plugins/` 加一个 `.lua` 文件，不用改 Go 代码**，按下面「添加 Lua Provider 插件」章节走即可。

## 1. 项目是什么

`git-tags` 是一个 Go CLI：**git tag 是唯一权威版本源**，一条命令把版本写进项目的所有版本文件并打 tag。

```text
git-tags check      # 校验每个 provider 与最新 tag 是否一致
git-tags sync       # 把 tag 版本写回所有可写文件
git-tags patch/minor/major   # 递增版本 + 同步 + 提交 + 打 tag
git-tags set 1.4.0 [--framework <name>]  # 显式设版本（可只写一个 provider）
git-tags plugins list|validate            # Lua 插件管理
```

## 2. 架构速览

版本流：**读最新 git tag → 每个激活的 provider 把版本写入自己的文件 → commit（`chore(release): bump to <tag>`）→ 打 tag →（可选 push）**。release 流程强制要求工作区干净；hook 在各阶段插入（`pre_bump` → 写文件 → `post_bump` → 提交 → `pre_tag` → 建 tag → `post_tag`）。

| 路径 | 职责 |
|------|------|
| `main.go` | `go:embed plugins/*.lua`，以**文件名**（去 `.lua`）为 provider 名注册内嵌插件。**新增插件只需把文件放进 `plugins/`，这里零改动** |
| `cmd/root.go` | 组装 engine；内嵌/用户插件的按名覆盖逻辑（用户 > 内嵌，同名后者被跳过）；cobra 命令 |
| `internal/core/engine.go` | 编排：Check / Sync / Bump / Set / EnsureInSync / 定向 setFramework |
| `internal/provider/provider.go` | Provider / Hook / Context / Target 接口定义 |
| `internal/lua/` | Lua 插件子系统：`runtime.go`（沙箱 Runner）、`api.go`（`gt` 宿主 API）、`plugin.go`（发现/校验/内嵌加载）、`provider.go` 与 `hook.go`（适配器） |
| `internal/config/config.go` | `.git-tags.toml` 解析 |
| `plugins/*.lua` | 内嵌 provider（开箱即用），可被用户插件覆盖 |
| `internal/lua/lua_test.go` | Lua 子系统的全部测试（含各插件 E2E） |

**Provider 解析优先级**（同名覆盖）：用户插件（项目 `.git-tags/plugins/` > 全局 `%APPDATA%\git-tags\plugins\`）> 内嵌插件 > git provider（权威源，固定不注册）。

## 3. 添加 Lua Provider 插件（核心流程）

### 3.1 契约与最小骨架

```lua
plugin = {
  name = "myapp",            -- 必须与文件名一致（内嵌注册用文件名；用户插件用此名覆盖）
  type = "provider",         -- "provider" | "hook"
  priority = 70,             -- 多 provider 同时激活时的顺序；参照下表
  description = "...",
}

function plugin.detect(project)   -- project = { root = "<绝对路径>" }
  local content = read("myapp.toml")
  return content ~= nil
end

function plugin.read(project)
  -- 返回不带前缀的版本串，如 "1.2.3"；失败用 error("中文信息")
end

function plugin.write(project, version)
  -- 把 version 写进文件；只改版本值，保留其余内容
end
```

`type = "hook"` 则实现 `plugin.pre_bump(from, to)` / `plugin.post_bump(from, to)` / `plugin.pre_tag(version)` / `plugin.post_tag(version)` 之一或多个；未实现的阶段自动跳过。

### 3.2 `gt` 沙箱 API（插件能用的全部能力）

| 成员 | 说明 |
|------|------|
| `gt.read_file(path)` | 读项目内文件（相对路径；绝对路径/越界报错） |
| `gt.write_file(path, content)` | 写项目内文件 |
| `gt.log(msg)` | 用户可见日志 |
| `gt.project.root` | 项目根绝对路径 |
| `gt.semver.parse(str)` / `.inc(str, "patch\|minor\|major")` / `.compare(a, b)` | 版本运算 |
| `gt.git.latest_tag()` | 最新 git tag（带前缀） |
| `gt.config.provider.<name>.carriers` | 项目 `.git-tags.toml` 里 `[provider.<name>] carriers` 解析出的载体列表（未配置时该表为空，回退插件内置默认） |

**沙箱限制**：每次调用全新 LState；只开放 base/table/string/math 库；`os`/`io`/`package`/`debug`/`coroutine` 一律不可用，`dofile/loadfile/load/loadstring/print` 已移除；单次执行 5 秒超时；panic 被兜底不崩工具。需要新能力时改 `internal/lua/api.go` 的 `registerAPI`（这是少数必须动 Go 的场景）。

### 3.3 代码风格约定（照抄现有插件即可保持一格）

- 中文注释；文件头部注释块说明「同步范围 / 用法 / 注意事项」，注明"已内嵌进二进制开箱即用"
- 固定分节：`-- ---------- 工具 ----------` 与 `-- ---------- Provider 契约 ----------`
- 读文件统一包一层：`local ok, content = pcall(gt.read_file, path); if not ok then return nil end; return content`
- 行处理用 `split_lines(content)`（`(content .. "\n"):gmatch("(.-)\n")`）+ `table.concat(lines, "\n")` 重组——**原样保留行尾与缩进**（CRLF 文件由行尾 `\r` 自然保住）
- **只替换版本值**：定位到版本所在行后 `gsub` 其引号内内容；新值与旧值相同则 `gt.log("版本已是 ...，跳过写入")` 并 return，不写文件
- 替换目标串含 `%%` 先转义：`lines[i] = lines[i]:gsub(pat, "%1" .. (version:gsub("%%", "%%%%")) .. "%2")`
- 模式串先 `escape()`（`s:gsub("[%^%$%(%)%%%.%[%]%*%+%-%?]", "%%%1")`）；gsub 替换来源若是函数调用必须用括号包住（`(to:gsub(...))`），否则函数第二返回值（计数）会泄漏成替换上限导致 0 次替换
- 错误信息用中文 `error("未找到顶层 version 字段")`；provider 名前缀由 Go 层自动附加（`moonbit: <msg>`）
- detect 里脚本出错 = 未命中（返回 false），不影响其它插件

### 3.4 priority 参照

| provider | 优先级 | 文件要点 |
|----------|--------|----------|
| tauri | 85 | 多文件 + README badge + Cargo.lock 根条目 |
| flutter | 75 | `pubspec.yaml` 顶层 + 保留 `+build` 号 + `pubspec.lock` root |
| uv | 70 | pyproject 段内 key 读写 + `uv.lock` 根包条目 |
| node | 65 | `package.json` + lock 根条目 |
| go | 60 | **carriers 配置化**（`gt.config.provider.go.carriers`） |
| moonbit | 55 | `moon.mod` 顶层 TOML key（首个 `[` 段标题前） |

新插件取一个不冲突的值即可（顺序只在多 provider 同时命中时才有意义）。

### 3.5 测试（必做，模式见 `internal/lua/lua_test.go`）

```go
content, err := os.ReadFile(filepath.Join("..", "..", "plugins", "<name>.lua"))
// 1) 写 fixture 文件（复刻真实项目结构，含"不能被动"的邻近版本串）
// 2) initTestRepo(t, root, "v0.1.3")
// 3) ep, _ := LoadEmbeddedProvider("<name>", string(content), NewRunner(root, nil))
// 4) core.New(config.Default(), ep) → engine.Check / engine.Bump
// 5) 断言文件内容：目标行已改、其它行逐字节不变
```

再补一个 Detect 边界表驱动用例（无文件/无字段/段内字段/注释均 false）。跑：`go test ./internal/lua/ -run Xxx -v`。

### 3.6 冒烟验证（二进制级）

```powershell
go build -o git-tags.exe .
# 临时目录：放 fixture + git init + commit + tag vX.Y.Z
.\git-tags.exe plugins list        # 应出现 <name> ... embedded
.\git-tags.exe plugins validate    # 插件脚本静态校验
.\git-tags.exe check               # 读出正确版本并 ✓
.\git-tags.exe patch --dry-run     # 预览
.\git-tags.exe set 0.2.0 --framework <name>   # 定向写入，肉眼核对 diff
```

### 3.7 文档同步

- `README.md`：内置 provider 列表（开头特性 bullet）、「工作原理」第 2 步的文件清单
- `assets/readme/sync-flow.svg`、`hero.svg`：纯手写 SVG，加减 pill/行时**同步调整 canvas 宽度与居中几何**，别只加文本导致溢出

## 4. 环境注意

- 构建/测试会写工作区外的 Go 共享缓存（`go build cache`），沙箱受限时对同一条命令做一次性 `sandbox_permissions` 升级
- **行尾约定**：Go 源文件 CRLF、`.lua` 插件 LF；本地新版 gofmt 会把全仓 CRLF→LF 而误报全仓「未格式化」——不要顺手全量 reformat，保持文件既有行尾
- 大文件（数万行 lock）：Runner 已放开 `RegistryMaxSize`，勿回退默认 8192 槽

## 5. 配置速查（`.git-tags.toml`）

```toml
[core]
tag_prefix = "v"
enabled = ["git", "tauri"]        # 只启用这些 provider

[provider.uv]
writable = false                  # 只校验不写入

[provider.go]
carriers = ["govar:cmd/root.go:Version", "plain:VERSION"]  # 仅 go.lua 使用该配置
```

## 6. 需求流程惯例

历史功能变更在 `docs/cr/CR-NNN.md` 有变更申请单（需求原文 / 影响评估 / 验证）。中小改动可直接实施并在提交信息里说明；涉及行为变更、删除或跨模块重构时，先补 CR 文档。
