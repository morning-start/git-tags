# git-tags 工具

## 简介
`git-tags` 是一个用于管理 Git 标签的工具，具备版本号递增和标签操作功能。借助该工具，你可以轻松管理项目的版本标签，包括显示所有标签、递增版本号、推送标签到远程仓库以及删除标签等操作。

## 安装
本项目依赖 Go 1.24.4 及以下库：
- github.com/Masterminds/semver/v3 v3.3.1
- github.com/spf13/cobra v1.6.1
- github.com/inconshreveable/mousetrap v1.0.1 (间接依赖)
- github.com/spf13/pflag v1.0.5 (间接依赖)

克隆仓库后，在项目根目录下运行以下命令进行构建：
```bash
go build -o git-tags main.go
```

## 命令说明
### ls
显示所有标签。
```bash
./git-tags ls
```

### patch
递增补丁版本号并创建新标签。
```bash
./git-tags patch
```

### minor
递增次版本号并创建新标签。
```bash
./git-tags minor
```

### major
递增主版本号并创建新标签。
```bash
./git-tags major
```

### push
推送标签到远程仓库，可使用 `-b` 参数指定分支，默认为 `origin`。
```bash
./git-tags push -b origin
```

### del
删除最新标签，可使用 `-b` 参数指定远程分支删除远程标签，默认为 `origin`。若不指定 `-b` 参数，则删除本地标签。
```bash
./git-tags del -b origin
```

## 贡献
如果你有任何改进建议或发现了 bug，欢迎提交 issue 或 pull request。

## 许可证
本项目采用 [MIT 许可证](LICENSE)。