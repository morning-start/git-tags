package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// 本文件提供文件型 provider 的公共读写工具：按字段路径做行级定位与替换，
// 保留文件原有格式与注释（不整文件重写）。

func join(root, path string) string {
	if root == "" || root == "." {
		return path
	}
	return filepath.Join(root, path)
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// readFileAt 读取项目根下相对路径的文件。
func readFileAt(root, path string) (string, error) {
	return readFile(join(root, path))
}

// ---- TOML 段内字段 ----

// tomlSectionRange 返回 section 段的行范围 [start, end)。
// 兼容 [package] 与 [[package]] 两种表头。
func tomlSectionRange(lines []string, section string) (int, int) {
	start := -1
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "["+section+"]" || trimmed == "[["+section+"]]" {
			start = i
			break
		}
	}
	if start < 0 {
		return -1, -1
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			end = i
			break
		}
	}
	return start, end
}

func tomlKeyRe(key string) *regexp.Regexp {
	// 捕获组：1=前缀（含开引号），2=值，3=闭合引号
	return regexp.MustCompile(`^(\s*` + regexp.QuoteMeta(key) + `\s*=\s*")([^"]*)(")`)
}

// readTOMLSectionKey 读取 TOML 段内字符串字段的值。
func readTOMLSectionKey(content, section, key string) (string, bool) {
	lines := strings.Split(content, "\n")
	start, end := tomlSectionRange(lines, section)
	if start < 0 {
		return "", false
	}
	re := tomlKeyRe(key)
	for i := start + 1; i < end; i++ {
		m := re.FindStringSubmatch(lines[i])
		if m != nil {
			return m[2], true
		}
	}
	return "", false
}
func replaceTOMLSectionKey(content, section, key, newVal string) (string, error) {
	lines := strings.Split(content, "\n")
	start, end := tomlSectionRange(lines, section)
	if start < 0 {
		return "", fmt.Errorf("未找到 [%s] 段", section)
	}
	re := tomlKeyRe(key)
	for i := start + 1; i < end; i++ {
		if re.MatchString(lines[i]) {
			lines[i] = re.ReplaceAllString(lines[i], `${1}`+newVal+`${3}`)
			return strings.Join(lines, "\n"), nil
		}
	}
	return "", fmt.Errorf("[%s] 段内未找到字段 %s", section, key)
}

// ---- JSON 顶层字段 ----

func readJSONKey(content, key string) (string, bool) {
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `"\s*:\s*"([^"]*)"`)
	m := re.FindStringSubmatch(content)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func replaceJSONKey(content, key, newVal string) (string, error) {
	re := regexp.MustCompile(`("` + regexp.QuoteMeta(key) + `"\s*:\s*")[^"]*(")`)
	if !re.MatchString(content) {
		return "", fmt.Errorf("未找到 JSON 字段 %q", key)
	}
	return re.ReplaceAllString(content, `${1}`+newVal+`${2}`), nil
}

// ---- YAML 顶层字段 ----

func readYAMLTopKey(content, key string) (string, bool) {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `\s*:\s*["']?([^"'\s#]+)`)
	m := re.FindStringSubmatch(content)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func replaceYAMLTopKey(content, key, newVal string) (string, error) {
	re := regexp.MustCompile(`(?m)^(\s*` + regexp.QuoteMeta(key) + `\s*:\s*)["']?[^"'\s#]+`)
	if !re.MatchString(content) {
		return "", fmt.Errorf("未找到 YAML 顶层字段 %q", key)
	}
	return re.ReplaceAllString(content, `${1}`+newVal), nil
}

// ---- pubspec.lock 的 packages.root.version（嵌套字段） ----

// readYAMLRootVersion 读取 pubspec.lock 中 packages.root 块下的 version。
func readYAMLRootVersion(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	rootIdx := -1
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "root:" {
			rootIdx = i
			break
		}
	}
	if rootIdx < 0 {
		return "", false
	}
	re := regexp.MustCompile(`^(\s*)version\s*:\s*["']?([^"'\s#]+)`)
	for i := rootIdx + 1; i < len(lines); i++ {
		indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		if indent < 2 {
			break
		}
		if m := re.FindStringSubmatch(lines[i]); m != nil && len(m[1]) >= 4 {
			return m[2], true
		}
	}
	return "", false
}

// readTargetValue 按 Target 的字段路径通用读取文件内的版本值，
// 供 dry-run 预览等场景使用（与各 provider 的专有逻辑等价）。
func readTargetValue(content string, t Target) (string, bool) {
	switch filepath.Ext(t.Path) {
	case ".toml":
		parts := strings.SplitN(t.Field, ".", 2)
		if len(parts) != 2 {
			return "", false
		}
		return readTOMLSectionKey(content, parts[0], parts[1])
	case ".json":
		return readJSONKey(content, t.Field)
	case ".yaml", ".yml":
		if strings.Contains(t.Field, ".") {
			return "", false // 嵌套字段（如 pubspec.lock）仅顶层支持
		}
		return readYAMLTopKey(content, t.Field)
	}
	return "", false
}
