package builtin

import (
	"regexp"
	"strings"

	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/rules"
)

var (
	usesRe = regexp.MustCompile(`^\s*-?\s*uses:\s*["']?([^\s"'#]+)`)
	shaRe  = regexp.MustCompile(`@[0-9a-f]{40}$`)
)

type actionsPinned struct{}

func (actionsPinned) ID() string { return "ci/actions-pinned" }

// Check scans workflow files line by line so findings carry line numbers.
// Local actions (./path), docker:// references and reusable workflows in the
// same repository are exempt; everything else must end in a 40-hex SHA.
func (actionsPinned) Check(ctx rules.Context) (rules.Outcome, error) {
	var out rules.Outcome
	files := append(ctx.Repo.Glob(".github/workflows/*.y*ml"), ctx.Repo.Glob(".gitea/workflows/*.y*ml")...)
	total := 0
	for _, wf := range files {
		text, ok := ctx.Repo.Text(wf)
		if !ok {
			continue
		}
		for i, line := range strings.Split(text, "\n") {
			m := usesRe.FindStringSubmatch(line)
			if m == nil || exemptUses(m[1]) {
				continue
			}
			total++
			if !shaRe.MatchString(m[1]) {
				out.Findings = append(out.Findings, model.Finding{
					Path: wf, Line: i + 1, Message: m[1] + " is not pinned to a commit SHA", Evidence: "uses",
				})
			}
		}
	}
	if len(out.Findings) == 0 && total > 0 {
		out.Evidence = "all action references pinned"
	}
	return out, nil
}

func exemptUses(ref string) bool {
	return strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "docker://")
}
