// Package provider 定义版本源的统一抽象：任何项目类型（git tag、tauri、uv、flutter
// 等）通过 Provider 接口接入版本同步引擎；Lua 插件最终也是该接口的一种实现。
package provider

// Project 描述被管理的项目。
type Project struct {
	// Root 是项目根目录，所有文件路径相对它解析。
	Root string
}

// Target 描述版本在项目文件中的一个位置。
type Target struct {
	// Path 是相对项目根的文件路径，如 "Cargo.toml"、"pyproject.toml"。
	Path string
	// Field 是文件内版本字段的定位，如 "package.version"、"version"。
	Field string
	// Writable 表示该位置是否参与写入。锁文件（Cargo.lock、uv.lock 等）
	// 只用于一致性校验，Writable 为 false。
	Writable bool
}

// Context 是传给 Provider 各方法的执行上下文。
type Context struct {
	Project *Project
	// Log 供 provider 输出用户可见信息。
	Log func(format string, args ...any)
}

// Logf 输出用户可见信息；Log 未设置时静默。
func (c *Context) Logf(format string, args ...any) {
	if c.Log != nil {
		c.Log(format, args...)
	}
}

// Provider 是一个版本源：负责检测项目类型、读取与写入版本。
type Provider interface {
	// Name 返回唯一标识，如 "git"、"tauri"、"uv"、"flutter"。
	Name() string
	// Detect 判断当前项目是否属于本 provider 支持的类型。
	Detect(ctx *Context) bool
	// Read 返回当前版本字符串（不带前缀，如 "1.2.3"）。
	Read(ctx *Context) (string, error)
	// Write 把版本写入所有 Writable target。
	Write(ctx *Context, version string) error
	// Targets 返回该 provider 涉及的版本位置（含只读校验位置）。
	Targets() []Target
	// Priority 决定多个 provider 同时激活时的顺序，数值大的优先。
	Priority() int
}

// Hook 是 bump 流程的扩展点，由引擎在对应阶段调用。
// stage 取值：pre_bump / post_bump（参数 from → to）、pre_tag / post_tag（参数 to）。
type Hook interface {
	Name() string
	Priority() int
	Run(ctx *Context, stage string, from, to string) error
}

// TargetChange 描述 dry-run 预览中单个文件的版本改动。
type TargetChange struct {
	Path string // 相对项目根的文件路径
	Old  string
	New  string
}

// Previewer 可选接口：dry-run 时提供 target 级「旧值 → 新值」预览。
// 未实现该接口的 provider 在 dry-run 中只输出 provider 级提示。
type Previewer interface {
	Preview(ctx *Context, version string) ([]TargetChange, error)
}
