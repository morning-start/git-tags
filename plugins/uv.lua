-- uv.lua —— uv python 项目版本同步插件
-- 同步范围:
--   pyproject.toml   [project].version（段内替换，不动依赖）
--   uv.lock          根包条目只读校验（由 uv sync 重新生成）
--
-- 用法: 已内嵌进 git-tags 二进制开箱即用；想自定义时把本文件放进
--   项目 .git-tags/plugins/ 或全局插件目录（%APPDATA%\git-tags\plugins\），
--   同名插件会覆盖内嵌/内置实现。
-- 注意: 与内置 Go provider 同名 "uv"，Lua 版本优先。

plugin = {
  name = "uv",
  type = "provider",
  priority = 70,
  description = "uv python 版本同步: pyproject.toml [project].version，uv.lock 只读校验",
}

local FILES = {
  pyproject = "pyproject.toml",
  lock = "uv.lock",
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

local function split_lines(content)
  local lines = {}
  for line in (content .. "\n"):gmatch("(.-)\n") do
    lines[#lines + 1] = line
  end
  return lines
end

local function escape(s)
  return (s:gsub("[%^%$%(%)%%%.%[%]%*%+%-%?]", "%%%1"))
end

-- 读取 TOML 段内字符串字段（如 [project] 的 name/version）
local function read_section_key(content, section, key)
  local lines = split_lines(content)
  local start = -1
  for i, ln in ipairs(lines) do
    if trim(ln) == "[" .. section .. "]" then
      start = i
      break
    end
  end
  if start < 0 then return nil end
  local pat = "^" .. escape(key) .. "%s*=%s*\"([^\"]+)\""
  for i = start + 1, #lines do
    local t = trim(lines[i])
    if t:sub(1, 1) == "[" then break end
    local v = t:match(pat)
    if v then return v end
  end
  return nil
end

-- 替换 TOML 段内字符串字段（保留缩进，不碰其他段）
local function replace_section_key(content, section, key, new_val)
  local lines = split_lines(content)
  local start = -1
  for i, ln in ipairs(lines) do
    if trim(ln) == "[" .. section .. "]" then
      start = i
      break
    end
  end
  if start < 0 then error("未找到 [" .. section .. "] 段") end
  local pat = "^(%s*" .. escape(key) .. "%s*=%s*\")[^\"]*(\")"
  for i = start + 1, #lines do
    local t = trim(lines[i])
    if t:sub(1, 1) == "[" then break end
    if lines[i]:match(pat) then
      lines[i] = lines[i]:gsub(pat, "%1" .. new_val .. "%2")
      return table.concat(lines, "\n")
    end
  end
  error("[" .. section .. "] 段内未找到字段 " .. key)
end

-- 读取 uv.lock 中根包条目（name 匹配后紧跟的 version 行）的版本
local function read_lock_version(content, app_name)
  local lines = split_lines(content)
  for i, ln in ipairs(lines) do
    if trim(ln) == 'name = "' .. app_name .. '"' then
      for j = i + 1, #lines do
        if lines[j]:match("^%s*%[%[") then break end -- 进入下一个 [[package]]
        local v = lines[j]:match('^%s*version%s*=%s*"([^"]*)"')
        if v then return v end
      end
      return nil
    end
  end
  return nil
end

-- ---------- Provider 契约 ----------

function plugin.detect(project)
  local content = read(FILES.pyproject)
  return content ~= nil and read_section_key(content, "project", "version") ~= nil
end

function plugin.read(project)
  local content = read(FILES.pyproject)
  if not content then error("未找到 " .. FILES.pyproject) end
  local ver = read_section_key(content, "project", "version")
  if not ver then error(FILES.pyproject .. " 未找到 [project].version") end

  -- uv.lock 只读校验（根包 name 取自 [project].name）
  local name = read_section_key(content, "project", "name")
  if name then
    local lock = read(FILES.lock)
    if lock then
      local lv = read_lock_version(lock, name)
      if lv and lv ~= ver then
        error(FILES.lock .. " 根包 version(" .. lv .. ") 与 " .. FILES.pyproject .. "(" .. ver .. ") 不一致")
      end
    end
  end
  return ver
end

function plugin.write(project, version)
  local content = read(FILES.pyproject)
  if not content then error("未找到 " .. FILES.pyproject) end
  local updated = replace_section_key(content, "project", "version", version)
  gt.write_file(FILES.pyproject, updated)
  gt.log("已同步 " .. FILES.pyproject .. " → " .. version)
end
