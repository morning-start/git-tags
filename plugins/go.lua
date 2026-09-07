-- go.lua —— Go 项目版本同步插件
--
-- Go 项目没有标准版本文件（go.mod 不携带项目版本），本插件按常见约定探测
-- 版本载体（依序取第一个存在的）：
--   1) 根目录 VERSION 文件            —— 单行纯文本版本号，如 "0.6.1"
--   2) 根目录 version.go              —— var/const Version = "0.6.1"（大小写均可）
--   3) internal/version/version.go    —— 同上（许多 Go CLI 用 ldflags 注入的惯例位置）
-- 一个都不存在时插件不激活：该项目仅由 git tag 作为权威源。
-- VERSION 内容按原样比较/写入（不带 v 前缀、单行）。
--
-- 用法: 已内嵌进 git-tags 二进制开箱即用；想自定义载体（如其它路径/变量名）时，
-- 把本文件复制到项目 .git-tags/plugins/ 或全局插件目录
-- （%APPDATA%\git-tags\plugins\）修改后即覆盖内嵌版本。

plugin = {
  name = "go",
  type = "provider",
  priority = 60,
  description = "Go 项目版本同步: VERSION 文件或 version.go 的 Version 字段",
}

-- 版本载体候选：{path, kind}，kind = plain（VERSION 纯文本）| govar（Version 赋值）
local CANDIDATES = {
  { path = "VERSION", kind = "plain" },
  { path = "version.go", kind = "govar" },
  { path = "internal/version/version.go", kind = "govar" },
}

-- ---------- 工具 ----------

local function read(path)
  local ok, content = pcall(gt.read_file, path)
  if not ok then return nil end
  return content
end

local function escape(s)
  return (s:gsub("[%^%$%(%)%%%.%[%]%*%+%-%?]", "%%%1"))
end

-- 全局字面替换，返回 (新内容, 命中次数)
-- 注意：内层 to:gsub(...) 必须用括号包住 —— 作为最后一个实参的函数调用会把
-- 第二个返回值（替换计数）泄漏给外层 gsub 的 n（替换上限），导致替换被限制为 0 次。
local function replace_all(content, from, to)
  return content:gsub(escape(from), (to:gsub("%%", "%%%%")))
end

-- 探测项目实际使用的载体；返回 {path, kind} 或 nil
local function find_carrier()
  for _, c in ipairs(CANDIDATES) do
    local content = read(c.path)
    if content then
      if c.kind == "plain" then
        if content:match("%S") then return c end -- VERSION 存在且有非空内容
      else
        if content:match('[Vv]ersion%s*=%s*"') then return c end
      end
    end
  end
  return nil
end

-- VERSION 文件：版本 = 去首尾空白后的整段内容（约定单行）
local function read_plain(content)
  return content:match("^%s*(.-)%s*$") or ""
end

-- version.go：版本 = 第一个 [Vv]ersion = "..." 的值
local function read_govar(content)
  return content:match('[Vv]ersion%s*=%s*"([^"]*)"') or ""
end

-- ---------- Provider 契约 ----------

function plugin.detect(project)
  if not read("go.mod") then return false end
  return find_carrier() ~= nil
end

function plugin.read(project)
  local carrier = find_carrier()
  if not carrier then error("未找到 Go 版本载体（VERSION / version.go）") end
  local content = read(carrier.path)
  local v
  if carrier.kind == "plain" then
    v = read_plain(content)
  else
    v = read_govar(content)
  end
  if v == "" then error(carrier.path .. " 中未找到版本") end
  return v
end

function plugin.write(project, version)
  local carrier = find_carrier()
  if not carrier then error("未找到 Go 版本载体（VERSION / version.go）") end
  local content = read(carrier.path)

  if carrier.kind == "plain" then
    if read_plain(content) == version then
      gt.log("版本已是 " .. version .. "，跳过写入")
      return
    end
    gt.write_file(carrier.path, version .. "\n")
    gt.log("已同步 " .. carrier.path .. " → " .. version)
    return
  end

  -- govar：只替换赋值行的版本值，保留变量名、空格与其余源码原文
  local old = read_govar(content)
  if old == version then
    gt.log("版本已是 " .. version .. "，跳过写入")
    return
  end
  local updated, hit = replace_all(content, '"' .. old .. '"', '"' .. version .. '"')
  if hit == 0 then
    error(carrier.path .. ' 未找到 Version = "' .. old .. '"')
  end
  gt.write_file(carrier.path, updated)
  gt.log("已同步 " .. carrier.path .. " → " .. version)
end
