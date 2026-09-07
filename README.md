# Git Tags Tool

## Introduction

`git-tags` is a tool for managing project versions and Git tags. It synchronizes a single version across multiple project files, so you no longer have to edit them by hand:

- **git tag** is the single source of truth (canonical version)
- Built-in support for common project types: **tauri** (Cargo.toml + tauri.conf.json), **uv python** (pyproject.toml), **flutter** (pubspec.yaml), **node** (package.json)
- **Lua plugins** let you extend version sync to any project type without touching Go code
- Hook plugins can run custom logic around a version bump (e.g. update CHANGELOG)

## Installation

This project depends on Go 1.24.4 and the following libraries:

- github.com/Masterminds/semver/v3 v3.3.1
- github.com/spf13/cobra v1.6.1
- github.com/BurntSushi/toml v1.4.0
- github.com/yuin/gopher-lua v1.1.1

After cloning the repository, run the following command in the project root directory to build:

```bash
go build -o git-tags main.go
```

## Commands

### ls
Display all tags.
```bash
./git-tags ls
```

### patch / minor / major
Increment the version and sync it to all project files, then create a new tag.
```bash
./git-tags patch
./git-tags minor
./git-tags major
```

Extra flags:

| Flag | Description |
|------|-------------|
| `-p, --push` | Push the tag to the remote after creating it |
| `--dry-run` | Preview every file change (`old → new`) without applying anything |
| `--commit` | Commit all version file changes together with the tag |
| `--no-tag` | Only update version files, do not create a tag |

### check
Compare every active provider's version with the canonical git tag and report inconsistencies.
```bash
./git-tags check
```

### sync
Write the canonical version (latest git tag) back to all writable files of every provider.
```bash
./git-tags sync          # apply
./git-tags sync --dry-run  # preview
```

### set
Set an explicit version, sync it to all providers and optionally create a tag.
```bash
./git-tags set 1.4.0
./git-tags set 1.4.0 --no-tag
```

### push
Push tags to a remote repository. You can use the `-b` parameter to specify the branch, with a default value of `origin`.
```bash
./git-tags push -b origin
```

### del
Delete the latest tag. You can use the `-b` parameter to specify a remote branch to delete the remote tag, with a default value of `origin`. If the `-b` parameter is not specified, the local tag will be deleted.
```bash
./git-tags del -b origin
```

### plugins
List discovered Lua plugins and validate plugin scripts.
```bash
./git-tags plugins list
./git-tags plugins validate            # validate all discovered plugins
./git-tags plugins validate my.lua     # validate a specific file
```

## Configuration

Optional `.git-tags.toml` in the project root:

```toml
[core]
# enabled = ["git", "tauri", "uv", "flutter", "node"]  # default: all
tag_prefix = "v"

# mark a provider read-only: checked for consistency but never written
[provider.uv]
writable = false
```

Notes:

- Lock files (`Cargo.lock`, `uv.lock`, `pubspec.lock`, `package-lock.json`) are **read-only checks**: they are verified for consistency but never written by this tool — let your toolchain (`cargo build`, `uv sync`, `flutter pub get`) regenerate them, then run `check` again.
- Flutter versions (`X.Y.Z+build`): the build number is preserved on write; bump compares only the base version.

## Lua Plugins

Plugins live in:

- Global: `%APPDATA%/git-tags/plugins/` (Windows) or `~/.config/git-tags/plugins/` (Linux/macOS)
- Project: `<repo>/.git-tags/plugins/`

Each plugin is a single `.lua` file declaring a global `plugin` table. Same-named project plugins override global ones; plugins are ordered by `priority` (higher first). A broken plugin is skipped with an error (visible in `plugins list`) and never blocks others.

### Provider plugin

Extends version sync to a new project type:

```lua
plugin = {
  name = "myapp",            -- unique id
  type = "provider",
  priority = 50,             -- default 50
  description = "VERSION file provider",
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

### Hook plugin

Runs around the bump pipeline (`pre_bump` → write files → `post_bump` → `pre_tag` → create tag → `post_tag`):

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

### Host API (`gt`)

| API | Description |
|-----|-------------|
| `gt.project` | table with `root` (absolute project path) |
| `gt.read_file(path)` / `gt.write_file(path, content)` | read/write files **inside the project only** (path whitelist) |
| `gt.log(msg)` | print a user-visible log line |
| `gt.semver.parse(v)` | parse → `{major, minor, patch, prerelease, metadata, str}` or `nil` |
| `gt.semver.inc(v, "patch"\|"minor"\|"major")` | increment → new version string |
| `gt.semver.compare(a, b)` | `-1` / `0` / `1` |
| `gt.git.latest_tag()` | latest git tag (with prefix) |

The sandbox removes `os`, `io`, `package`, `debug` libraries and dangerous base functions (`dofile`, `loadfile`, `load`, `loadstring`); scripts are killed after a 5 second timeout, and panics never crash the tool.

## Contribution

If you have any suggestions for improvement or find a bug, please feel free to submit an issue or a pull request.

## License

This project is licensed under the [MIT License](LICENSE).
