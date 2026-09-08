-- node.lua —— node / 前端项目版本同步插件
-- 覆盖: Vue、React、Next.js、Svelte、Angular 等所有以 package.json
-- 承载版本的前端项目。
-- 同步范围:
--   package.json          顶层 "version" 字段（保留原有空白/引号格式）
--   package-lock.json     根条目版本同步（顶层 "version" 与 packages.""；npm-shrinkwrap.json 同）
--                         （yarn.lock / pnpm-lock.yaml 无版本字段；read 仍做一致性校验）
--
-- 用法: 已内嵌进 git-tags 二进制开箱即用；想自定义时把本文件放进
--   项目 .git-tags/plugins/ 或全局插件目录（%APPDATA%\git-tags\plugins\），
--   同名插件会覆盖内嵌/内置实现。
-- 注意: 与内置 Go provider 同名 "node"，Lua 版本优先（用户 > 内嵌 > 内置）。

plugin = {
  name = "node",
  type = "provider",
  priority = 65,
  description = "node/前端项目版本同步: package.json + lock 根条目版本同步",
}

local FILES = {
  pkg  = "package.json",
  lock = "package-lock.json",
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

-- 替换顶层 "version" 的值（保留原有空白与引号格式，只替换第一处 = 顶层字段）
local function replace_json_version(content, new_val)
  return content:gsub('("version"%s*:%s*")[^"]*(")', "%1" .. new_val .. "%2", 1)
end

-- 定位 "packages" 段起始。跳过依赖条目中名为 "packages" 的键（形如
-- "node_modules/packages" 的键其引号前是 "/"，顶层键前是空白/换行）。
local function find_packages_section(content)
  local s = content:find('"packages"%s*:%s*{')
  while s do
    if s == 1 or content:sub(s - 1, s - 1) ~= "/" then
      return s
    end
    s = content:find('"packages"%s*:%s*{', s + 1)
  end
  return nil
end

-- 同步 lock 根条目版本：顶层 "version"（v1/v2/v3 均在文件首位），lockfileVersion 2/3
-- 另有 packages."" 条目。只改根条目，值已是新版本则跳过；返回 (新内容, 是否变更)。
-- 不做全局替换，避免误伤同版本号的依赖条目。
local function sync_lock_version(content, new_val)
  local changed = false

  -- 1) 顶层 "version"（首个出现的 version 字段 = 根项目版本）
  local root_ver = content:match('"version"%s*:%s*"([^"]+)"')
  if root_ver and root_ver ~= new_val then
    content = content:gsub('("version"%s*:%s*")[^"]*(")', "%1" .. new_val .. "%2", 1)
    changed = true
  end

  -- 2) packages."" 条目（lockfileVersion 2/3）
  local start = find_packages_section(content)
  if start then
    local root = content:find('""%s*:%s*{', start)
    if root then
      local vs = content:find('"version"%s*:%s*"', root)
      if vs then
        local ve = content:find('"', vs + #'"version": "')
        if ve then
          local cur = content:sub(vs + #'"version": "', ve - 1)
          if cur ~= new_val then
            content = content:sub(1, vs - 1) .. '"version": "' .. new_val .. '"' .. content:sub(ve)
            changed = true
          end
        end
      end
    end
  end

  return content, changed
end

-- ---------- Provider 契约 ----------

function plugin.detect(project)
  local content = read(FILES.pkg)
  return content ~= nil and content:match('"version"%s*:%s*"') ~= nil
end

function plugin.read(project)
  local content = read(FILES.pkg)
  if not content then error("未找到 " .. FILES.pkg) end
  local ver = content:match('"version"%s*:%s*"([^"]+)"')
  if not ver then error(FILES.pkg .. " 未找到顶层 version 字段") end

  -- package-lock.json 只读校验
  local lock = read(FILES.lock)
  if lock then
    local lv = lock:match('"version"%s*:%s*"([^"]+)"')
    if lv and lv ~= ver then
      error(FILES.lock .. " version(" .. lv .. ") 与 " .. FILES.pkg .. "(" .. ver .. ") 不一致")
    end
  end
  return ver
end

function plugin.write(project, version)
  local content = read(FILES.pkg)
  if not content then error("未找到 " .. FILES.pkg) end
  local old = content:match('"version"%s*:%s*"([^"]+)"')
  if not old then error(FILES.pkg .. " 未找到顶层 version 字段") end
  if old == version then
    gt.log("版本已是 " .. version .. "，跳过写入")
    return
  end

  local updated, hit = replace_all(content, '"version": "' .. old .. '"', '"version": "' .. version .. '"')
  if hit == 0 then error(FILES.pkg .. ' 未找到 "version": "' .. old .. '"') end
  gt.write_file(FILES.pkg, updated)
  gt.log("已同步 " .. FILES.pkg .. " → " .. version)

  -- lock 根条目版本同步（npm-shrinkwrap.json 优先于 package-lock.json，存在才处理；
  -- yarn.lock / pnpm-lock.yaml 无版本字段，不处理）
  for _, lockpath in ipairs({ "npm-shrinkwrap.json", FILES.lock }) do
    local lock = read(lockpath)
    if lock then
      local updated_lock, changed = sync_lock_version(lock, version)
      if changed then
        gt.write_file(lockpath, updated_lock)
        gt.log("已同步 " .. lockpath .. " 根条目 → " .. version)
      end
    end
  end
end
