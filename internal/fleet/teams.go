package fleet

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fleetlint/fleetlint/internal/model"
)

// noTeam labels repositories without a team in the team summary.
const noTeam = "(none)"

// TeamSummary aggregates the repositories of one team.
type TeamSummary struct {
	Team string `json:"team"`
	// Repos counts every repository of the team; Unchecked those that could
	// not be checked and are left out of the other numbers.
	Repos     int `json:"repos"`
	Unchecked int `json:"unchecked"`
	// Compliance is the mean compliance of the checked repositories.
	Compliance float64 `json:"compliance"`
	Errors     int     `json:"errors"`
	Warnings   int     `json:"warnings"`
	Excepted   int     `json:"excepted"`
	Disabled   int     `json:"disabled"`
	Lowered    int     `json:"lowered"`
	// Expiring counts exceptions that have expired or will within 30 days.
	Expiring int `json:"expiring"`
}

// expiryWindowDays is how far ahead an exception counts as expiring.
const expiryWindowDays = 30

// daysLeft returns the whole days until an exception's deadline; dated is
// false when it has none or the date does not parse.
func daysLeft(ex model.Exception, now time.Time) (days int, dated bool) {
	until, err := time.Parse("2006-01-02", ex.Until)
	if err != nil {
		return 0, false
	}
	return int(until.Sub(now.Truncate(24*time.Hour)).Hours() / 24), true
}

func expiring(exs []model.Exception, now time.Time) int {
	n := 0
	for _, ex := range exs {
		if days, dated := daysLeft(ex, now); ex.Expired || (dated && days <= expiryWindowDays) {
			n++
		}
	}
	return n
}

// summarizeTeams groups repositories by team. It returns nil when no
// repository names a team, so single-team fleets keep the plain report.
func summarizeTeams(repos []RepoReport, now time.Time) []TeamSummary {
	byTeam := map[string]*TeamSummary{}
	sums := map[string]float64{}
	named := false
	for _, r := range repos {
		team := r.Team
		if team == "" {
			team = noTeam
		} else {
			named = true
		}
		t := byTeam[team]
		if t == nil {
			t = &TeamSummary{Team: team}
			byTeam[team] = t
		}
		t.Repos++
		if r.Err != "" {
			t.Unchecked++
			continue
		}
		sums[team] += r.Compliance()
		t.Errors += r.Summary.Errors
		t.Warnings += r.Summary.Warnings
		t.Excepted += r.Summary.Excepted
		t.Disabled += len(r.Disabled)
		t.Lowered += len(r.Weakened)
		t.Expiring += expiring(r.Exceptions, now)
	}
	if !named {
		return nil
	}
	out := make([]TeamSummary, 0, len(byTeam))
	for team, t := range byTeam {
		if checked := t.Repos - t.Unchecked; checked > 0 {
			t.Compliance = sums[team] / float64(checked)
		}
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Team < out[j].Team })
	return out
}

func writeTeamsMarkdown(b *strings.Builder, teams []TeamSummary) {
	if len(teams) == 0 {
		return
	}
	b.WriteString("\n## Teams\n\n| Team | Repositories | Unchecked | Compliance | Errors | Warnings | Excepted | Disabled | Lowered | Exceptions expiring |\n|---|---|---|---|---|---|---|---|---|---|\n")
	for _, t := range teams {
		fmt.Fprintf(b, "| %s | %d | %d | %.0f%% | %d | %d | %d | %d | %d | %d |\n", escapeCell(t.Team), t.Repos, t.Unchecked, t.Compliance*100, t.Errors, t.Warnings, t.Excepted, t.Disabled, t.Lowered, t.Expiring)
	}
}

func writeTeamsHTML(b *strings.Builder, teams []TeamSummary) {
	if len(teams) == 0 {
		return
	}
	b.WriteString("<h2>Teams</h2><div class=wrap><table><tr><th>Team</th><th>Repositories</th><th>Unchecked</th><th>Compliance</th><th>Errors</th><th>Warnings</th><th>Excepted</th><th>Disabled</th><th>Lowered</th><th>Exceptions expiring</th></tr>")
	for _, t := range teams {
		pct := t.Compliance * 100
		fmt.Fprintf(b, "<tr><td>%s</td><td class=c>%d</td><td class=\"c %s\">%d</td><td><span class=bar style=\"width:%.0fpx\"></span>%.0f%%</td><td class=\"c %s\">%d</td><td class=\"c %s\">%d</td><td class=c>%d</td><td class=c>%d</td><td class=c>%d</td><td class=\"c %s\">%d</td></tr>",
			html.EscapeString(t.Team), t.Repos, classIf(t.Unchecked > 0, "err"), t.Unchecked, pct, pct,
			classIf(t.Errors > 0, "err"), t.Errors, classIf(t.Warnings > 0, "warn"), t.Warnings, t.Excepted, t.Disabled, t.Lowered, classIf(t.Expiring > 0, "warn"), t.Expiring)
	}
	b.WriteString("</table></div>")
}

// renderCSV is one row per repository, for spreadsheets and whatever
// reporting an organization already has.
func renderCSV(rep *Report) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	rows := [][]string{{"repository", "team", "source", "visibility", "tier", "stacks", "compliance", "errors", "warnings", "excepted", "disabled", "lowered", "error"}}
	for _, r := range rep.Repos {
		if r.Err != "" {
			rows = append(rows, []string{r.Name, r.Team, r.Source, "", "", "", "", "", "", "", "", "", r.Err})
			continue
		}
		rows = append(rows, []string{
			r.Name, r.Team, r.Source, string(r.Facts.Visibility), strconv.Itoa(r.Facts.Tier), strings.Join(r.Facts.Stacks, " "),
			strconv.FormatFloat(r.Compliance(), 'f', 3, 64), strconv.Itoa(r.Summary.Errors), strconv.Itoa(r.Summary.Warnings),
			strconv.Itoa(r.Summary.Excepted), strconv.Itoa(len(r.Disabled)), strconv.Itoa(len(r.Weakened)), "",
		})
	}
	if err := w.WriteAll(rows); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
