# Git Tags Tool

## Introduction
`git-tags` is a tool for managing Git tags, with version number increment and tag operation capabilities. With this tool, you can easily manage the version tags of your project, including displaying all tags, incrementing version numbers, pushing tags to a remote repository, and deleting tags.

## Installation
This project depends on Go 1.24.4 and the following libraries:
- github.com/Masterminds/semver/v3 v3.3.1
- github.com/spf13/cobra v1.6.1
- github.com/inconshreveable/mousetrap v1.0.1 (indirect dependency)
- github.com/spf13/pflag v1.0.5 (indirect dependency)

After cloning the repository, run the following command in the project root directory to build:
```bash
go build -o git-tags main.go
```

## Command Description
### ls
Display all tags.
```bash
./git-tags ls
```

### patch
Increment the patch version number and create a new tag.
```bash
./git-tags patch
```

### minor
Increment the minor version number and create a new tag.
```bash
./git-tags minor
```

### major
Increment the major version number and create a new tag.
```bash
./git-tags major
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

## Contribution
If you have any suggestions for improvement or find a bug, please feel free to submit an issue or a pull request.

## License
This project is licensed under the [MIT License](LICENSE).