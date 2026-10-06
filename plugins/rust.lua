-- rust.lua —— Rust (Cargo) 项目版本同步插件
-- 参考: link-disk 项目（clap CLI 的版本单一来源设计）：
--   clap 的 #[command(version)] 引用 crate::version::VERSION（env! 编译期注入），
--   build.rs 运行时从 git tags 推导版本、回退 Cargo.toml 的 [package].version，
--   源码里没有硬编码版本串 —— 因此同步目标就是 Cargo.toml 与 Cargo.lock。
--
-- 同步范围:
--   Cargo.toml  [package].version（只改 package 段，不动依赖与 profile）
--   Cargo.lock  根包条目 version 同步（read 时做一致性校验）
--
-- 用法: 已内嵌进 git-tags 二进制开箱即用；想自定义时把本文件放进
--   项目 .git-tags/plugins/ 或全局插件目录（%APPDATA%\git-tags\plugins\），
--   同名插件会覆盖内嵌/内置实现。
-- 注意: tauri 项目的 Cargo.toml 在 src-tauri/ 下，由 tauri 插件负责，本插件只认
--   根目录 Cargo.toml；workspace 虚拟根（只有 [workspace] 段）不激活。

plugin = {
  name = "rust",
  type = "provider",
  priority = 80,
  description = "Rust (Cargo) 版本同步: Cargo.toml [package].version + Cargo.lock 根包条目",
}

local FILES = {
  cargo = "Cargo.toml",
  lock  = "Cargo.lock",
}

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

-- 转义 gsub 模式中的特殊字符
local function escape(s)
  return (s:gsub("[%^%$%(%)%%%.%[%]%*%+%-%?]", "%%%1"))
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

-- 同步 Cargo.lock 根包条目（name 匹配后紧跟的 version 行）的版本，保留缩进；
-- 值已是新版本则跳过。返回 (新内容, 是否变更)
local function sync_lock_version(content, app_name, new_val)
  local lines = split_lines(content)
  for i, ln in ipairs(lines) do
    if trim(ln) == 'name = "' .. app_name .. '"' then
      for j = i + 1, #lines do
        if lines[j]:match("^%s*%[%[") then break end -- 进入下一个 [[package]] 块
        local cur = lines[j]:match('^%s*version%s*=%s*"([^"]*)"')
        if cur then
          if cur == new_val then return content, false end
          local indent_str = lines[j]:match("^(%s*)")
          lines[j] = indent_str .. 'version = "' .. new_val .. '"'
          return table.concat(lines, "\n"), true
        end
      end
      return content, false
    end
  end
  return content, false
end

-- ---------- Provider 契约 ----------

function plugin.detect(project)
  local cargo = read(FILES.cargo)
  if not cargo then return false end
  local _, ver = read_cargo_package(cargo)
  return ver ~= nil
end

function plugin.read(project)
  local cargo = read(FILES.cargo)
  if not cargo then error("未找到 Cargo.toml（无法读取当前版本）") end
  local name, old = read_cargo_package(cargo)
  if not old then error("Cargo.toml: [package] 段内未找到 version 字段") end

  -- Cargo.lock 只读校验（根包 name 取自 Cargo.toml [package].name）
  if name then
    local lock = read(FILES.lock)
    if lock then
      local lv = find_lock_version(lock, name)
      if lv and lv ~= old then
        error(FILES.lock .. " 根包 version(" .. lv .. ") 与 Cargo.toml(" .. old .. ") 不一致")
      end
    end
  end
  return old
end

function plugin.write(project, version)
  local cargo = read(FILES.cargo)
  if not cargo then error("未找到 Cargo.toml（无法同步版本）") end
  local name, old = read_cargo_package(cargo)
  if not old then error("Cargo.toml: [package] 段内未找到 version 字段") end
  if old == version then
    gt.log("版本已是 " .. version .. "，跳过写入")
    return
  end

  -- 全部替换成功后再一次性写入（Cargo.toml 失败则不碰 Cargo.lock）
  local outputs = {}

  -- 1. Cargo.toml [package].version
  outputs[#outputs + 1] = { FILES.cargo, replace_cargo_package_version(cargo, old, version) }

  -- 2. Cargo.lock 根包条目版本同步（name 取自 [package].name；存在才处理）
  if name then
    local lock = read(FILES.lock)
    if lock then
      local updated_lock, changed = sync_lock_version(lock, name, version)
      if changed then
        outputs[#outputs + 1] = { FILES.lock, updated_lock }
      end
    end
  end

  for _, o in ipairs(outputs) do
    gt.write_file(o[1], o[2])
    gt.log("已同步 " .. o[1])
  end
end
