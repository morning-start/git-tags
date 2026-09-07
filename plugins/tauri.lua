-- tauri.lua —— tauri 项目完整版本同步插件
-- 参考: silk 项目 scripts/bump-version.ts（bun 脚本 → Lua 插件，行为对齐）
--
-- 同步范围:
--   package.json              "version": "..."
--   src-tauri/tauri.conf.json "version": "..."
--   src-tauri/Cargo.toml      [package].version（只改 package 段，不动依赖）
--   src-tauri/Cargo.lock      只读校验（由 cargo build 重新生成，本插件不修改）
--   README.md                 shields.io version 徽章（可选，存在才改）
--
-- 用法: 已内嵌进 git-tags 二进制开箱即用；想自定义时把本文件放进
--   项目 .git-tags/plugins/ 或全局插件目录（%APPDATA%\git-tags\plugins\），
--   同名插件会覆盖内嵌/内置实现。
-- 注意: 与内置 Go provider 同名 "tauri"，Lua 版本优先（用户 > 内嵌 > 内置）。

plugin = {
  name = "tauri",
  type = "provider",
  priority = 85,
  description = "tauri 全量同步: package.json + tauri.conf.json + Cargo.toml + README 徽章（Cargo.lock 只读校验）",
}

local FILES = {
  pkg    = "package.json",
  conf   = "src-tauri/tauri.conf.json",
  cargo  = "src-tauri/Cargo.toml",
  lock   = "src-tauri/Cargo.lock",
  readme = "README.md",
}

-- ---------- 工具 ----------

local function read(path)
  local ok, content = pcall(gt.read_file, path)
  if not ok then return nil end
  return content
end

-- 转义 gsub 模式中的特殊字符
local function escape(s)
  return (s:gsub("[%^%$%(%)%%%.%[%]%*%+%-%?]", "%%%1"))
end

-- 全局字面替换（仿 TS 的 replaceAll），返回 (新内容, 命中次数)
-- 注意：内层 to:gsub(...) 必须用括号包住 —— 作为最后一个实参的函数调用会把
-- 第二个返回值（替换计数）泄漏给外层 gsub 的 n（替换上限），导致替换被限制为 0 次。
local function replace_all(content, from, to)
  return content:gsub(escape(from), (to:gsub("%%", "%%%%")))
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

-- 读取 Cargo.toml [package] 段，返回 (name, version)
local function read_cargo_package(content)
  local lines = split_lines(content)
  local start = -1
  for i, ln in ipairs(lines) do
    if trim(ln) == "[package]" then
      start = i
      break
    end
  end
  if start < 0 then return nil, nil end
  local name, ver
  for i = start + 1, #lines do
    local t = trim(lines[i])
    if t:sub(1, 1) == "[" then break end
    local n = t:match('^name%s*=%s*"([^"]+)"')
    if n then name = n end
    local v = t:match('^version%s*=%s*"([^"]+)"')
    if v then ver = v end
  end
  return name, ver
end

-- 只替换 [package] 段内的 version 行（保留缩进，不碰依赖）
local function replace_cargo_package_version(content, old, new)
  local lines = split_lines(content)
  local start = -1
  for i, ln in ipairs(lines) do
    if trim(ln) == "[package]" then
      start = i
      break
    end
  end
  if start < 0 then error("Cargo.toml: 未找到 [package] 段") end
  local pat = '^%s*version%s*=%s*"' .. escape(old) .. '"'
  for i = start + 1, #lines do
    if trim(lines[i]):sub(1, 1) == "[" then break end
    if lines[i]:match(pat) then
      local indent = lines[i]:match("^(%s*)")
      lines[i] = indent .. 'version = "' .. new .. '"'
      return table.concat(lines, "\n")
    end
  end
  error('Cargo.toml: [package] 段内未找到 version = "' .. old .. '"')
end

-- 定位 Cargo.lock 中根包条目（name 匹配后紧跟的 version 行），返回其版本（只读）
local function find_lock_version(content, app_name)
  local lines = split_lines(content)
  for i, ln in ipairs(lines) do
    if trim(ln) == 'name = "' .. app_name .. '"' then
      for j = i + 1, #lines do
        if lines[j]:match("^%s*%[%[") then break end -- 进入下一个 [[package]] 块
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
  local pkg = read(FILES.pkg)
  if not pkg or not pkg:match('"version"%s*:%s*"') then return false end
  local conf = read(FILES.conf)
  local cargo = read(FILES.cargo)
  return conf ~= nil or cargo ~= nil
end

function plugin.read(project)
  local pkg = read(FILES.pkg)
  if not pkg then error("未找到 package.json（无法读取当前版本）") end
  local old = pkg:match('"version"%s*:%s*"([^"]+)"')
  if not old then error("package.json 未找到 version 字段") end

  -- 一致性校验（对齐内置 provider 的语义，报错带文件名）
  local conf = read(FILES.conf)
  if conf then
    local cv = conf:match('"version"%s*:%s*"([^"]+)"')
    if cv and cv ~= old then
      error(FILES.conf .. " version(" .. cv .. ") 与 package.json(" .. old .. ") 不一致")
    end
  end
  local cargo = read(FILES.cargo)
  if cargo then
    local name, cv = read_cargo_package(cargo)
    if cv and cv ~= old then
      error(FILES.cargo .. " version(" .. cv .. ") 与 package.json(" .. old .. ") 不一致")
    end
    -- Cargo.lock 只读校验（根包 name 取自 Cargo.toml [package].name）
    if name then
      local lock = read(FILES.lock)
      if lock then
        local lv = find_lock_version(lock, name)
        if lv and lv ~= old then
          error(FILES.lock .. " 根包 version(" .. lv .. ") 与 package.json(" .. old .. ") 不一致")
        end
      end
    end
  end
  return old
end

function plugin.write(project, version)
  local pkg = read(FILES.pkg)
  if not pkg then error("未找到 package.json（无法同步版本）") end
  local old = pkg:match('"version"%s*:%s*"([^"]+)"')
  if not old then error("package.json 未找到 version 字段") end
  if old == version then
    gt.log("版本已是 " .. version .. "，跳过写入")
    return
  end

  -- 全部替换成功后再一次性写入（仿 bump-version.ts 的原子语义）
  local outputs = {}

  -- 1. package.json
  local updated = replace_all(pkg, '"version": "' .. old .. '"', '"version": "' .. version .. '"')
  outputs[#outputs + 1] = { FILES.pkg, updated }

  -- 2. tauri.conf.json
  local conf = read(FILES.conf)
  if conf then
    local updated, hit = replace_all(conf, '"version": "' .. old .. '"', '"version": "' .. version .. '"')
    if hit == 0 then error(FILES.conf .. ' 未找到 "version": "' .. old .. '"') end
    outputs[#outputs + 1] = { FILES.conf, updated }
  end

  -- 3. Cargo.toml [package].version（Cargo.lock 只读校验，不写入）
  local cargo = read(FILES.cargo)
  if cargo then
    outputs[#outputs + 1] = { FILES.cargo, replace_cargo_package_version(cargo, old, version) }
  end

  -- 5. README shields 徽章（可选：存在才改；比 TS 的严格报错更宽容）
  local readme = read(FILES.readme)
  if readme then
    local updated = readme:gsub(escape("version-" .. old .. "-blue"), "version-" .. version .. "-blue")
    if updated ~= readme then
      outputs[#outputs + 1] = { FILES.readme, updated }
    end
  end

  for _, o in ipairs(outputs) do
    gt.write_file(o[1], o[2])
    gt.log("已同步 " .. o[1])
  end
end
