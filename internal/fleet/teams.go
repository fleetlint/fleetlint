package fleet

import (
	"bytes"
	"encoding/csv"
	"fmt"
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

// summarize groups repositories by the key (team or group). It returns nil
// when no repository has one, so a flat fleet keeps the plain report.
func summarize(repos []RepoReport, now time.Time, key func(RepoReport) string) []TeamSummary {
	byTeam := map[string]*TeamSummary{}
	sums := map[string]float64{}
	named := false
	for _, r := range repos {
		team := key(r)
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

// summarizeGroups is summarize over groups with every parent group rolled
// up: a repository in `incyde/tooling` counts for `incyde/tooling` and for
// `incyde`.
func summarizeGroups(repos []RepoReport, now time.Time) []TeamSummary {
	var expanded []RepoReport
	for _, r := range repos {
		expanded = append(expanded, r)
		for g := parentGroup(r.Group); g != ""; g = parentGroup(g) {
			up := r
			up.Group = g
			expanded = append(expanded, up)
		}
	}
	return summarize(expanded, now, func(r RepoReport) string { return r.Group })
}

// teamsTable renders a team or group summary; empty when there is none.
func teamsTable(title, key string, teams []TeamSummary) table {
	t := table{Title: title, Sortable: true, Head: []string{key, "Repositories", "Unchecked", "Compliance", "Errors", "Warnings", "Excepted", "Disabled", "Lowered", "Exceptions expiring"}}
	for _, s := range teams {
		pct := s.Compliance * 100
		t.Rows = append(t.Rows, []tcell{
			txt(s.Team), num(s.Repos), numIf(s.Unchecked, "err"),
			{Text: fmt.Sprintf("%.0f%%", pct), Bar: &pct, Sort: fmt.Sprintf("%.3f", pct)},
			numIf(s.Errors, "err"), numIf(s.Warnings, "warn"), num(s.Excepted), num(s.Disabled), num(s.Lowered), numIf(s.Expiring, "warn"),
		})
	}
	return t
}

// renderCSV is one row per repository, for spreadsheets and whatever
// reporting an organization already has.
func renderCSV(rep *Report) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	// New columns go after "error" so existing consumers keep their indices.
	rows := [][]string{{
		"repository", "team", "source", "visibility", "tier", "stacks", "compliance", "errors", "warnings", "excepted", "disabled", "lowered", "error",
		"group", "tier_source", "fixable", "rule_errors", "branch", "commit", "dirty", "last_commit", "stale",
	}}
	for _, r := range rep.Repos {
		if r.Err != "" {
			rows = append(rows, []string{r.Name, r.Team, r.Source, "", "", "", "", "", "", "", "", "", r.Err, r.Group, "", "", "", "", "", "", "", ""})
			continue
		}
		branch, commit, dirty, last, stale := "", "", "", "", ""
		if g := r.Git; g != nil {
			branch, commit, dirty, stale = g.Branch, g.Commit, strconv.Itoa(g.Dirty), strconv.FormatBool(g.Stale(rep.GeneratedAt))
			if !g.LastCommit.IsZero() {
				last = g.LastCommit.Format("2006-01-02")
			}
		}
		rows = append(rows, []string{
			r.Name, r.Team, r.Source, string(r.Facts.Visibility), strconv.Itoa(r.Facts.Tier), strings.Join(r.Facts.Stacks, " "),
			strconv.FormatFloat(r.Compliance(), 'f', 3, 64), strconv.Itoa(r.Summary.Errors), strconv.Itoa(r.Summary.Warnings),
			strconv.Itoa(r.Summary.Excepted), strconv.Itoa(len(r.Disabled)), strconv.Itoa(len(r.Weakened)), "",
			r.Group, string(r.Facts.Sources["tier"]), strconv.Itoa(r.Fixable()), strconv.Itoa(r.Summary.Error), branch, commit, dirty, last, stale,
		})
	}
	if err := w.WriteAll(rows); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
