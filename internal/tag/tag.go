package tag

import (
	"fmt"
	"strings"

	"git-tags/internal/git"
	"github.com/Masterminds/semver/v3"
)

type Manager struct {
	gitRunner *git.Runner
}

func NewManager() *Manager {
	return &Manager{
		gitRunner: git.NewRunner(),
	}
}

func (m *Manager) ListTags() error {
	output, err := m.gitRunner.ListTags()
	if err != nil {
		return fmt.Errorf("error listing tags: %w", err)
	}
	fmt.Println(output)
	return nil
}

func (m *Manager) BumpVersion(level string, push bool) error {
	latestTag := m.gitRunner.GetLatestTag()
	cleanedTag := strings.TrimPrefix(latestTag, "v")

	v, err := semver.NewVersion(cleanedTag)
	if err != nil {
		v, _ = semver.NewVersion("0.0.0")
	}

	var newVersion semver.Version
	switch level {
	case "patch":
		newVersion = v.IncPatch()
	case "minor":
		newVersion = v.IncMinor()
	case "major":
		newVersion = v.IncMajor()
	default:
		return fmt.Errorf("invalid version level: %s", level)
	}

	newTag := "v" + newVersion.String()

	if err := m.gitRunner.CreateTag(newTag); err != nil {
		return err
	}

	if push {
		return m.PushTag("origin")
	}

	return nil
}

func (m *Manager) PushTag(branch string) error {
	latestTag := m.gitRunner.GetLatestTag()
	return m.gitRunner.PushTag(branch, latestTag)
}

func (m *Manager) DeleteLatestTag(branch string) error {
	latestTag := m.gitRunner.GetLatestTag()

	if err := m.gitRunner.DeleteLocalTag(latestTag); err != nil {
		return err
	}

	return m.gitRunner.DeleteRemoteTag(branch, latestTag)
}
