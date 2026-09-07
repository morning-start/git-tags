package provider

// fileProvider 是文件型 provider 的公共骨架：tauri/uv/flutter/node 等内置
// 项目类型复用同一实现，仅通过闭包注入 detect/read/write 逻辑。
type fileProvider struct {
	name     string
	priority int
	targets  []Target
	detect   func(root string) bool
	read     func(root string) (string, error)
	write    func(root string, version string) error
}

func (f *fileProvider) Name() string { return f.name }

func (f *fileProvider) Priority() int { return f.priority }

func (f *fileProvider) Targets() []Target { return f.targets }

func (f *fileProvider) Detect(ctx *Context) bool { return f.detect(ctx.Project.Root) }

func (f *fileProvider) Read(ctx *Context) (string, error) { return f.read(ctx.Project.Root) }

func (f *fileProvider) Write(ctx *Context, version string) error {
	return f.write(ctx.Project.Root, version)
}

// Preview 实现 provider.Previewer：对每个可写 target 给出「旧值 → 新值」预览。
func (f *fileProvider) Preview(ctx *Context, version string) ([]TargetChange, error) {
	var out []TargetChange
	root := ctx.Project.Root
	for _, t := range f.targets {
		if !t.Writable {
			continue
		}
		content, err := readFileAt(root, t.Path)
		if err != nil {
			continue // 文件不存在（可能由插件/工具链创建）：跳过
		}
		old, ok := readTargetValue(content, t)
		if !ok {
			continue
		}
		out = append(out, TargetChange{Path: t.Path, Old: old, New: version})
	}
	return out, nil
}
