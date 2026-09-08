-- flutter.lua —— flutter 项目版本同步插件
-- 同步范围:
--   pubspec.yaml    顶层 version: X.Y.Z+build（写入时保留 build 号，仅替换基础版本）
--   pubspec.lock    root: 块 version 同步（写入时保留 build 号；read 仍做一致性校验）
--
-- 用法: 已内嵌进 git-tags 二进制开箱即用；想自定义时把本文件放进
--   项目 .git-tags/plugins/ 或全局插件目录（%APPDATA%\git-tags\plugins\），
--   同名插件会覆盖内嵌/内置实现。
-- 注意: 与内置 Go provider 同名 "flutter"，Lua 版本优先。

plugin = {
  name = "flutter",
  type = "provider",
  priority = 75,
  description = "flutter 版本同步: pubspec.yaml（保留 +build）+ pubspec.lock root 版本",
}

local FILES = {
  yaml = "pubspec.yaml",
  lock = "pubspec.lock",
}

-- ---------- 工具 ----------

local function read(path)
  local ok, content = pcall(gt.read_file, path)
  if not ok then return nil end
  return content
end

local function trim(s)
  return (s:gsub("^%s+", ""):gsub("%s+$", ""))
end

local function indent(s)
  local nz = s:find("%S")
  return (nz and nz - 1) or 0
end

local function split_lines(content)
  local lines = {}
  for line in (content .. "\n"):gmatch("(.-)\n") do
    lines[#lines + 1] = line
  end
  return lines
end

local function base_version(v)
  return (v:match("^([^%+]+)"))
end

-- 读取顶层（无缩进）version: 字段
local function read_yaml_version(content)
  for _, ln in ipairs(split_lines(content)) do
    if indent(ln) == 0 then
      local v = ln:match("^version%s*:%s*[\"']?([^%s\"'#]+)")
      if v then return v end
    end
  end
  return nil
end

-- 替换顶层 version: 字段（保留原缩进）
local function replace_yaml_version(content, new_val)
  local lines = split_lines(content)
  for i, ln in ipairs(lines) do
    if indent(ln) == 0 and ln:match("^version%s*:%s*") then
      lines[i] = "version: " .. new_val
      return table.concat(lines, "\n"), true
    end
  end
  return content, false
end

-- 读取 pubspec.lock 中 packages.root 块下的 version（嵌套缩进）
local function read_lock_root_version(content)
  local lines = split_lines(content)
  local root = -1
  for i, ln in ipairs(lines) do
    if trim(ln) == "root:" then
      root = i
      break
    end
  end
  if root < 0 then return nil end
  for i = root + 1, #lines do
    local t = trim(lines[i])
    if t ~= "" then
      if indent(lines[i]) < 2 then
        break -- 已离开 root 块
      end
      if indent(lines[i]) >= 4 then
        local v = t:match("^version%s*:%s*[\"']?([^%s\"'#]+)")
        if v then return v end
      end
    end
  end
  return nil
end

-- 同步 pubspec.lock root: 块内的 version 行（保留原缩进与引号格式）；
-- 值已是新版本则跳过。返回 (新内容, 是否变更)
local function sync_lock_root_version(content, new_val)
  local lines = split_lines(content)
  local root = -1
  for i, ln in ipairs(lines) do
    if trim(ln) == "root:" then
      root = i
      break
    end
  end
  if root < 0 then return content, false end
  for i = root + 1, #lines do
    local t = trim(lines[i])
    if t ~= "" then
      if indent(lines[i]) < 2 then break end
      if indent(lines[i]) >= 4 then
        local cur = t:match("^version%s*:%s*[\"']?([^%s\"'#]+)")
        if cur then
          if cur == new_val then return content, false end
          local indent_str = lines[i]:match("^(%s*)")
          lines[i] = indent_str .. 'version: "' .. new_val .. '"'
          return table.concat(lines, "\n"), true
        end
      end
    end
  end
  return content, false
end

-- ---------- Provider 契约 ----------

function plugin.detect(project)
  local content = read(FILES.yaml)
  return content ~= nil and read_yaml_version(content) ~= nil
end

function plugin.read(project)
  local content = read(FILES.yaml)
  if not content then error("未找到 " .. FILES.yaml) end
  local ver = read_yaml_version(content)
  if not ver then error(FILES.yaml .. " 未找到顶层 version 字段") end
  local base = base_version(ver)

  -- pubspec.lock 只读校验（比较基础版本，build 号差异可接受）
  local lock = read(FILES.lock)
  if lock then
    local lv = read_lock_root_version(lock)
    if lv and base_version(lv) ~= base then
      error(FILES.lock .. " version(" .. lv .. ") 与 " .. FILES.yaml .. "(" .. ver .. ") 不一致")
    end
  end
  return base
end

function plugin.write(project, version)
  local content = read(FILES.yaml)
  if not content then error("未找到 " .. FILES.yaml) end
  local old = read_yaml_version(content)
  if not old then error(FILES.yaml .. " 未找到顶层 version 字段") end

  -- 新版本未带 build 号时沿用旧 build 号（flutter 约定 build 号独立递增）
  local new_val = version
  if not version:find("%+") and old:find("%+") then
    new_val = version .. "+" .. old:match("%+(.+)")
  end

  local updated, hit = replace_yaml_version(content, new_val)
  if not hit then error(FILES.yaml .. " 未找到可替换的 version: 字段") end
  gt.write_file(FILES.yaml, updated)
  gt.log("已同步 " .. FILES.yaml .. " → " .. new_val)

  -- pubspec.lock 根条目版本同步（存在才处理；root 块 version 写入完整版本含 build 号）
  local lock = read(FILES.lock)
  if lock then
    local updated_lock, changed = sync_lock_root_version(lock, new_val)
    if changed then
      gt.write_file(FILES.lock, updated_lock)
      gt.log("已同步 " .. FILES.lock .. " 根条目 → " .. new_val)
    end
  end
end
