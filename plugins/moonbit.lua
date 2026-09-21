-- moonbit.lua —— MoonBit 项目版本同步插件
-- 同步范围:
--   moon.mod    顶层 version = "X.Y.Z"（TOML 顶格键，位于第一个段标题之前）
--
-- moon.mod 是 TOML 格式：项目版本是顶层 version 键，出现在第一个段标题（如
-- [dependencies]）之前。文件里还有别的版本长相的东西——import { ... } 块中的
-- "moonbitlang/quickcheck@0.14.0" 依赖串、段内嵌套的 version 字段——都不是
-- 项目版本，本插件只定位并替换第一个段标题之前的顶层 version 键，其余内容
-- 原样保留（含缩进、引号与注释）。
--
-- 用法: 已内嵌进 git-tags 二进制开箱即用；想自定义时把本文件放进
--   项目 .git-tags/plugins/ 或全局插件目录（%APPDATA%\git-tags\plugins\），
--   同名插件会覆盖内嵌/内置实现。
-- 注意: 与内置 Go provider 同名 "moonbit"，Lua 版本优先。

plugin = {
  name = "moonbit",
  type = "provider",
  priority = 55,
  description = "MoonBit 项目版本同步: moon.mod 顶层 version 字段",
}

local FILE = "moon.mod"

-- ---------- 工具 ----------

local function read(path)
  local ok, content = pcall(gt.read_file, path)
  if not ok then return nil end
  return content
end

local function split_lines(content)
  local lines = {}
  for line in (content .. "\n"):gmatch("(.-)\n") do
    lines[#lines + 1] = line
  end
  return lines
end

local function trim(s)
  return (s:gsub("^%s+", ""):gsub("%s+$", ""))
end

local function escape(s)
  return (s:gsub("[%^%$%(%)%%%.%[%]%*%+%-%?]", "%%%1"))
end

-- 顶层 version 键匹配串：行首（可含缩进）version = "..."
-- 行首锚定天然排除注释行（# version = "..."）与段内嵌套字段。
local VERSION_KEY = '^%s*version%s*=%s*"([^"]*)"'

-- 在顶层区（第一个段标题行之前，含无段标题的整个文件）查找 version 键；
-- 返回 (行号, 版本) 或 nil。段标题以行首 [ 识别（[table] / [[array]]）。
local function find_top_version(lines)
  for i, ln in ipairs(lines) do
    if ln:match("^%s*%[") then break end
    local v = ln:match(VERSION_KEY)
    if v then return i, v end
  end
  return nil
end

-- ---------- Provider 契约 ----------

function plugin.detect(project)
  local content = read(FILE)
  if not content then return false end
  return find_top_version(split_lines(content)) ~= nil
end

function plugin.read(project)
  local content = read(FILE)
  if not content then error("未找到 " .. FILE) end
  local _, v = find_top_version(split_lines(content))
  if not v then error(FILE .. " 未找到顶层 version 字段") end
  return v
end

function plugin.write(project, version)
  local content = read(FILE)
  if not content then error("未找到 " .. FILE) end
  local lines = split_lines(content)
  local idx, old = find_top_version(lines)
  if not idx then error(FILE .. " 未找到顶层 version 字段") end
  if old == version then
    gt.log("版本已是 " .. version .. "，跳过写入")
    return
  end

  -- 只替换该行 version 键的引号内值，保留缩进与行内其余内容
  local pat = '^(%s*version%s*=%s*")[^"]*(")'
  if not lines[idx]:match(pat) then
    error(FILE .. " 第 " .. idx .. " 行不是可替换的 version 键")
  end
  lines[idx] = lines[idx]:gsub(pat, "%1" .. (version:gsub("%%", "%%%%")) .. "%2")
  gt.write_file(FILE, table.concat(lines, "\n"))
  gt.log("已同步 " .. FILE .. " → " .. version)
end
