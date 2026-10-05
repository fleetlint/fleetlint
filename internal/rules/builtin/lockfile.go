package builtin

import (
	"fmt"
	"strings"

	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/rules"
)

// lockfiles maps a stack to the lockfiles that satisfy it. One match is enough.
var lockfiles = map[string][]string{
	"go":      {"go.sum"},
	"python":  {"uv.lock", "poetry.lock", "pdm.lock", "Pipfile.lock", "requirements.lock", "requirements.txt"},
	"rust":    {"Cargo.lock"},
	"node":    {"package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb", "bun.lock"},
	"flutter": {"pubspec.lock"},
	"kotlin":  {"gradle.lockfile", "gradle/verification-metadata.xml"},
}

type lockfileCommitted struct{}

func (lockfileCommitted) ID() string { return "repo/lockfile-committed" }

func (lockfileCommitted) Check(ctx rules.Context) (rules.Outcome, error) {
	tracked := map[string]bool{}
	for _, f := range ctx.Repo.TrackedInScope() {
		tracked[f] = true
	}
	var out rules.Outcome
	var found []string
	for _, stack := range ctx.Facts.Stacks {
		candidates, known := lockfiles[stack]
		if !known {
			continue
		}
		if name := firstTracked(candidates, tracked); name != "" {
			found = append(found, name)
			continue
		}
		if !hasDependencies(ctx, stack) {
			continue
		}
		out.Findings = append(out.Findings, model.Finding{
			Path:     candidates[0],
			Message:  fmt.Sprintf("%s: no lockfile is tracked (expected one of %s)", stack, strings.Join(candidates, ", ")),
			Evidence: stack,
		})
	}
	out.Evidence = strings.Join(found, ", ")
	return out, nil
}

func firstTracked(candidates []string, tracked map[string]bool) string {
	for _, c := range candidates {
		if tracked[c] {
			return c
		}
	}
	return ""
}

// hasDependencies avoids demanding a lockfile from a project with nothing to
// lock, such as a Go module with no requirements.
func hasDependencies(ctx rules.Context, stack string) bool {
	switch stack {
	case "go":
		text, _ := ctx.Repo.Text("go.mod")
		return strings.Contains(text, "require")
	case "rust":
		text, _ := ctx.Repo.Text("Cargo.toml")
		return strings.Contains(text, "[dependencies]") || strings.Contains(text, "[workspace.dependencies]")
	}
	return true
}
