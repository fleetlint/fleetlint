package catalog

import (
	"bytes"
	"encoding/json"

	"github.com/fleetlint/fleetlint/internal/fix"
)

const renovateTemplate = "renovate.json"

// renovateConfig is the Renovate configuration fix writes. Only the settings
// the repository's stacks need are in it: a setting for a package manager the
// repository does not have is noise at best and a surprise at worst.
type renovateConfig struct {
	Schema              string          `json:"$schema"`
	Extends             []string        `json:"extends"`
	PostUpdateOptions   []string        `json:"postUpdateOptions,omitempty"`
	LockFileMaintenance map[string]any  `json:"lockFileMaintenance,omitempty"`
	PreCommit           map[string]bool `json:"pre-commit"`
}

// lockfileStacks are the stacks whose package manager keeps a lockfile that
// Renovate can refresh on a schedule. Go's go.sum is not one.
var lockfileStacks = map[string]bool{"node": true, "python": true, "rust": true, "flutter": true, "kotlin": true, "ruby": true, "php": true, "dotnet": true}

// composeRenovate writes renovate.json for the project's stacks.
func composeRenovate(stack string, p fix.Project) ([]byte, error) {
	cfg := renovateConfig{
		Schema: "https://docs.renovatebot.com/renovate-schema.json",
		// The two customManagers presets update versions in Makefiles and in
		// workflow run steps that carry a `# renovate:` comment.
		Extends: []string{"config:recommended", "helpers:pinGitHubActionDigests", ":semanticCommits", "customManagers:makefileVersions", "customManagers:githubActionsVersions"},
		// The hook revisions fix writes are kept current too.
		PreCommit: map[string]bool{"enabled": true},
	}
	stacks := map[string]bool{stack: true}
	for _, n := range p.Nested {
		stacks[n.Stack] = true
	}
	if stacks["go"] {
		cfg.PostUpdateOptions = []string{"gomodTidy"}
	}
	for s := range stacks {
		if lockfileStacks[s] {
			cfg.LockFileMaintenance = map[string]any{"enabled": true, "schedule": []string{"before 6am on monday"}}
			break
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
