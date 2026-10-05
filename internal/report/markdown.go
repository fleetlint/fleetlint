package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/model"
)

// writeMarkdown renders a pull-request comment: a summary line, one table
// row per failing rule, and a collapsed section with the agent instructions.
func writeMarkdown(w io.Writer, run *engine.Run, opts Options) error {
	var b strings.Builder
	c := model.Summarize(run.Results)
	f := run.Facts
	fmt.Fprintf(&b, "### fleetlint: %s\n\n", f.Name)
	fmt.Fprintf(&b, "%d passed · %d warnings · %d errors · %d excepted · stacks: %s · tier %d\n\n",
		c.Pass, c.Warnings, c.Errors, c.Excepted, orNone(strings.Join(f.Stacks, ", ")), f.Tier)
	fmt.Fprintf(&b, "Checked with %s.\n\n", builtWith(opts))
	fails := failing(run.Results)
	if len(fails) == 0 {
		b.WriteString("No findings.\n")
		_, err := io.WriteString(w, b.String())
		return err
	}
	b.WriteString("| | Rule | Finding | Fix |\n|---|---|---|---|\n")
	for _, r := range fails {
		id := r.Rule.ID
		if r.Scope != "" {
			id = r.Scope + ": " + id
		}
		fixText := cellText(r.Rule.Fix.Human)
		if r.Fixable {
			fixText = "`fleetlint fix --apply` \\* " + fixText
		}
		fmt.Fprintf(&b, "| %s | `%s` | %s | %s |\n", mdMark(r), id, cellText(markdownFinding(r)), fixText)
	}
	if n := countFixable(fails); n > 0 {
		fmt.Fprintf(&b, "\n\\* %d of these can be fixed mechanically with `fleetlint fix --apply`.\n", n)
	}
	b.WriteString("\n<details><summary>Instructions for an agent</summary>\n\n")
	for _, r := range fails {
		instr := r.Rule.Fix.Agent
		if instr == "" {
			instr = r.Rule.Fix.Human
		}
		fmt.Fprintf(&b, "- **%s**: %s\n", r.Rule.ID, instr)
	}
	b.WriteString("\n</details>\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func failing(results []model.Result) []model.Result {
	var out []model.Result
	for _, r := range results {
		if r.Status == model.StatusFail || r.Status == model.StatusError {
			out = append(out, r)
		}
	}
	return sorted(out)
}

func mdMark(r model.Result) string {
	if r.Status == model.StatusError {
		return "ERR"
	}
	switch r.Severity {
	case model.SeverityError:
		return "FAIL"
	case model.SeverityWarning:
		return "warn"
	case model.SeverityInfo:
		return "info"
	}
	return ""
}

func markdownFinding(r model.Result) string {
	if r.Status == model.StatusError {
		return "rule error: " + r.Err
	}
	if len(r.Findings) == 0 {
		return r.Rule.Title
	}
	if len(r.Findings) == 1 {
		return location(r.Findings[0])
	}
	return fmt.Sprintf("%s (%d findings, first: %s)", r.Rule.Title, len(r.Findings), location(r.Findings[0]))
}

// cellText keeps a table cell on one line and escapes pipes, so a finding's
// message can never break the table or inject markdown.
func cellText(s string) string {
	s = strings.NewReplacer("\r", " ", "\n", " ", "|", "\\|").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}
