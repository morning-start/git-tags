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
