package builtin

import (
	"regexp"

	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/rules"
)

// junkPatterns are files that have no business in version control. Each
// pattern names the whole class so the finding can say why.
var junkPatterns = []struct {
	re   *regexp.Regexp
	what string
}{
	{regexp.MustCompile(`(^|/)\.DS_Store$`), "macOS Finder metadata"},
	{regexp.MustCompile(`(^|/)(Thumbs\.db|desktop\.ini)$`), "Windows Explorer metadata"},
	{regexp.MustCompile(`\.(bak|orig|rej)$`), "backup or patch leftover"},
	{regexp.MustCompile(`\.(swp|swo)$|~$`), "editor swap file"},
	{regexp.MustCompile(`(^|/)nohup\.out$`), "nohup output"},
	{regexp.MustCompile(`\.log$`), "log file"},
	{regexp.MustCompile(`(^|/)__pycache__/|\.pyc$`), "Python bytecode"},
	{regexp.MustCompile(`(^|/)(node_modules|\.venv|venv)/`), "dependency directory"},
	{regexp.MustCompile(`(^|/)\.idea/workspace\.xml$|(^|/)\.vscode/settings\.json$`), "personal IDE state"},
}

type trackedJunk struct{}

func (trackedJunk) ID() string { return "repo/no-tracked-junk" }

func (trackedJunk) Check(ctx rules.Context) (rules.Outcome, error) {
	var out rules.Outcome
	for _, f := range ctx.Repo.TrackedInScope() {
		for _, p := range junkPatterns {
			if p.re.MatchString(f) {
				out.Findings = append(out.Findings, model.Finding{
					Path:     f,
					Message:  p.what + " is tracked",
					Evidence: p.re.String(),
				})
				break
			}
		}
	}
	return out, nil
}
