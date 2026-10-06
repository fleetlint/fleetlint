package fleet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/report"
)

// Write renders the report into dir: fleet.md, fleet.html, fleet.json, fleet.csv,
// actions/<repo>.{md,html} and history/<timestamp>.json; index.html duplicates
// fleet.html so the directory publishes as a static site.
func Write(dir string, rep *Report) error {
	for _, sub := range []string{"", "actions", "history"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o750); err != nil {
			return err
		}
	}
	page := renderHTML(rep)
	files := map[string][]byte{
		"fleet.md":   renderMarkdown(rep),
		"fleet.html": page,
		"index.html": page, // so the directory can be published as-is (GitHub/Gitea Pages)
	}
	js, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	files["fleet.json"] = js
	if files["fleet.csv"], err = renderCSV(rep); err != nil {
		return err
	}
	files[filepath.Join("history", rep.GeneratedAt.UTC().Format("2006-01-02T150405Z")+".json")] = js
	for _, r := range rep.Repos {
		md := renderActions(r)
		files[filepath.Join("actions", r.Name+".md")] = md
		files[filepath.Join("actions", r.Name+".html")] = actionsHTML(r.Name, md)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil { //nolint:gosec // report files
			return err
		}
	}
	return nil
}

type cell struct {
	Mark, Title string
}

// cellFor summarizes a rule across the root and every scope: the worst status wins.
func cellFor(r RepoReport, rule string) cell {
	best := cell{"", ""}
	rank := -1
	for _, res := range r.Results {
		if res.Rule.ID != rule {
			continue
		}
		c, k := cellOf(res)
		if k > rank {
			best, rank = c, k
		}
	}
	return best
}

// cellOf renders one result and ranks it (higher = worse) for aggregation.
func cellOf(res model.Result) (cell, int) {
	prefix := ""
	if res.Scope != "" {
		prefix = res.Scope + ": "
	}
	switch res.Status {
	case model.StatusPass:
		return cell{"ok", prefix + "pass"}, 1
	case model.StatusExcepted:
		reason := res.Evidence
		if len(res.Findings) > 0 && res.Findings[0].Exception != nil {
			reason = "excepted: " + res.Findings[0].Exception.Reason
		}
		return cell{"exc", prefix + reason}, 2
	case model.StatusBaselined:
		return cell{"base", prefix + "baselined"}, 3
	case model.StatusNotApplicable:
		return cell{"-", "n/a"}, 0
	case model.StatusError:
		return cell{"ERR", prefix + "rule error: " + res.Err}, 6
	case model.StatusFail:
		switch res.Severity {
		case model.SeverityError:
			return cell{"FAIL", prefix + leadMessage(res)}, 5
		case model.SeverityWarning:
			return cell{"warn", prefix + leadMessage(res)}, 4
		case model.SeverityInfo:
			return cell{"info", prefix + leadMessage(res)}, 3
		}
	}
	return cell{"", ""}, -1
}

// leadMessage is the first finding's message, or the title when there are none.
func leadMessage(r model.Result) string {
	if len(r.Findings) == 0 {
		return r.Rule.Title
	}
	return r.Findings[0].Message
}

func renderMarkdown(rep *Report) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# Fleet report\n\nGenerated %s · %d repositories · %d rules · fleetlint %s, catalog %s\n\n", rep.GeneratedAt.Format("2006-01-02 15:04 MST"), len(rep.Repos), len(rep.Rules), orUnknown(rep.Tool), orUnknown(rep.Catalog))
	b.WriteString("## Repositories\n\n| Repository | Team | Stacks | Tier | Compliance | Errors | Warnings | Excepted | Disabled |\n|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range rep.Repos {
		if r.Err != "" {
			fmt.Fprintf(&b, "| %s | %s | | | error | | | | |\n", r.Name, r.Team)
			continue
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %d | %.0f%% | %d | %d | %d | %d |\n", r.Name, r.Team, strings.Join(r.Facts.Stacks, ", "), r.Facts.Tier,
			r.Compliance()*100, r.Summary.Errors, r.Summary.Warnings, r.Summary.Excepted, len(r.Disabled))
	}
	writeTeamsMarkdown(&b, rep.Teams)
	b.WriteString("\n## Rules\n\n| Rule | Pass | Fail | Excepted | n/a |\n|---|---|---|---|---|\n")
	for _, rule := range rep.Rules {
		var pass, fail, exc, na int
		for _, r := range rep.Repos {
			switch cellFor(r, rule).Mark {
			case "ok":
				pass++
			case "FAIL", "warn", "info", "ERR":
				fail++
			case "exc", "base":
				exc++
			case "-":
				na++
			}
		}
		fmt.Fprintf(&b, "| `%s` | %d | %d | %d | %d |\n", rule, pass, fail, exc, na)
	}
	// Rules down, repositories across: the rule list is long, the fleet usually is not.
	b.WriteString("\n## Matrix\n\n| Rule |")
	for _, r := range rep.Repos {
		fmt.Fprintf(&b, " %s |", r.Name)
	}
	b.WriteString("\n|---|" + strings.Repeat("---|", len(rep.Repos)) + "\n")
	for _, rule := range rep.Rules {
		fmt.Fprintf(&b, "| `%s` |", rule)
		for _, r := range rep.Repos {
			fmt.Fprintf(&b, " %s |", cellFor(r, rule).Mark)
		}
		b.WriteString("\n")
	}
	writePolicyNotes(&b, rep)
	return []byte(b.String())
}

func writePolicyNotes(b *strings.Builder, rep *Report) {
	section := func(title, header string, rows []string) {
		if len(rows) == 0 {
			return
		}
		sort.Strings(rows)
		fmt.Fprintf(b, "\n## %s\n\n%s\n", title, header)
		for _, row := range rows {
			b.WriteString(row + "\n")
		}
	}
	section("Disabled rules", "| Repository | Rule | Layer | Where | Reason |\n|---|---|---|---|---|", collectDisabled(rep, false))
	section("Lowered severities", "| Repository | Rule | Layer | Where | Reason |\n|---|---|---|---|---|", collectDisabled(rep, true))
	section("Exceptions", "| Repository | Team | Rule | Reason | Until |\n|---|---|---|---|---|", collectExceptions(rep))
}

func collectDisabled(rep *Report, weakened bool) []string {
	var rows []string
	for _, r := range rep.Repos {
		list := r.Disabled
		if weakened {
			list = r.Weakened
		}
		for _, d := range list {
			rows = append(rows, fmt.Sprintf("| %s | `%s` | %s | %s | %s |", r.Name, d.ID, d.Layer, escapeCell(d.Where), escapeCell(d.Reason)))
		}
	}
	return rows
}

// escapeCell makes repository-derived text safe inside a Markdown table cell.
func escapeCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " ")
}

func collectExceptions(rep *Report) []string {
	var rows []string
	for _, r := range rep.Repos {
		for _, ex := range r.Exceptions {
			until := ex.Until
			switch days, dated := daysLeft(ex, rep.GeneratedAt); {
			case until == "":
				until = "no deadline"
			case ex.Expired:
				until += " (expired)"
			case dated && days <= expiryWindowDays:
				until += fmt.Sprintf(" (in %d days)", days)
			}
			rows = append(rows, fmt.Sprintf("| %s | %s | `%s` | %s | %s |", r.Name, escapeCell(r.Team), ex.Rule, escapeCell(ex.Reason), until))
		}
	}
	return rows
}

func renderActions(r RepoReport) []byte {
	if r.Err != "" {
		return []byte(fmt.Sprintf("# fleetlint: actions for %s\n\nThe check could not run: %s\n", r.Name, r.Err))
	}
	var b bytes.Buffer
	_ = report.WriteActions(&b, r.Name, r.Results) // a bytes.Buffer write cannot fail
	return b.Bytes()
}

func renderHTML(rep *Report) []byte {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Fleet report</title><style>
:root{--bg:#fff;--fg:#1a1a1a;--muted:#666;--line:#ddd;--ok:#2e7d32;--warn:#b26a00;--err:#c62828}
@media(prefers-color-scheme:dark){:root{--bg:#121212;--fg:#eee;--muted:#9a9a9a;--line:#333;--ok:#81c784;--warn:#ffb74d;--err:#ef5350}}
body{font:14px/1.45 system-ui,sans-serif;background:var(--bg);color:var(--fg);margin:0;padding:16px;max-width:1400px}
h1{font-size:20px;margin:0 0 4px}h2{font-size:16px;margin:28px 0 8px}.muted{color:var(--muted)}
table{border-collapse:collapse;width:100%;font-variant-numeric:tabular-nums}th,td{border:1px solid var(--line);padding:4px 8px;text-align:left;white-space:nowrap}
th{position:sticky;top:0;background:var(--bg)}td.c{text-align:center}
.ok{color:var(--ok)}.warn{color:var(--warn)}.err{color:var(--err)}.na{color:var(--muted)}
.bar{display:inline-block;height:10px;background:var(--ok);vertical-align:middle;margin-right:6px}
.wrap{overflow-x:auto}abbr{text-decoration:none;cursor:help}
th.rot{height:170px;vertical-align:bottom;padding:4px 2px}th.rot div{writing-mode:vertical-rl;transform:rotate(180deg);font-weight:normal;font-size:12px}
</style></head><body>`)
	fmt.Fprintf(&b, "<h1>Fleet report</h1><p class=muted>Generated %s · %d repositories · %d rules · fleetlint %s, catalog %s</p>", rep.GeneratedAt.Format("2006-01-02 15:04 MST"), len(rep.Repos), len(rep.Rules), html.EscapeString(orUnknown(rep.Tool)), html.EscapeString(orUnknown(rep.Catalog)))
	b.WriteString("<h2>Repositories</h2><div class=wrap><table><tr><th>Repository</th><th>Team</th><th>Stacks</th><th>Tier</th><th>Compliance</th><th>Errors</th><th>Warnings</th><th>Excepted</th><th>Disabled</th></tr>")
	for _, r := range rep.Repos {
		if r.Err != "" {
			fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td colspan=7 class=err>%s</td></tr>", html.EscapeString(r.Name), html.EscapeString(r.Team), html.EscapeString(r.Err))
			continue
		}
		pct := r.Compliance() * 100
		fmt.Fprintf(&b, "<tr><td><a href=\"actions/%s.html\">%s</a></td><td>%s</td><td>%s</td><td class=c>%d</td><td><span class=bar style=\"width:%.0fpx\"></span>%.0f%%</td><td class=\"c %s\">%d</td><td class=\"c %s\">%d</td><td class=c>%d</td><td class=c>%d</td></tr>",
			html.EscapeString(r.Name), html.EscapeString(r.Name), html.EscapeString(r.Team), html.EscapeString(strings.Join(r.Facts.Stacks, ", ")), r.Facts.Tier,
			pct, pct, classIf(r.Summary.Errors > 0, "err"), r.Summary.Errors, classIf(r.Summary.Warnings > 0, "warn"), r.Summary.Warnings, r.Summary.Excepted, len(r.Disabled))
	}
	b.WriteString("</table></div>")
	writeTeamsHTML(&b, rep.Teams)
	b.WriteString("<h2>Matrix</h2><div class=wrap><table><tr><th>Rule</th>")
	for _, r := range rep.Repos {
		fmt.Fprintf(&b, "<th class=rot><div>%s</div></th>", html.EscapeString(r.Name))
	}
	b.WriteString("</tr>")
	for _, rule := range rep.Rules {
		fmt.Fprintf(&b, "<tr><td><code>%s</code></td>", html.EscapeString(rule))
		for _, r := range rep.Repos {
			c := cellFor(r, rule)
			fmt.Fprintf(&b, "<td class=\"c %s\"><abbr title=\"%s\">%s</abbr></td>", markClass(c.Mark), html.EscapeString(c.Title), c.Mark)
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</table></div>")
	var notes strings.Builder
	writePolicyNotes(&notes, rep)
	if notes.Len() > 0 {
		b.WriteString("<h2>Policy notes</h2><pre>" + html.EscapeString(notes.String()) + "</pre>")
	}
	b.WriteString("<p class=muted>ok pass, exc excepted, base baselined, warn warning, FAIL error, info info, ERR rule error, - not applicable</p></body></html>\n")
	return []byte(b.String())
}

func classIf(cond bool, class string) string {
	if cond {
		return class
	}
	return ""
}

func markClass(mark string) string {
	switch mark {
	case "ok", "exc":
		return "ok"
	case "warn", "info", "base":
		return "warn"
	case "FAIL", "ERR":
		return "err"
	}
	return "na"
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
