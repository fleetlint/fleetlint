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

	"github.com/fleetlint/fleetlint/internal/facts"
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
	v := newView(rep)
	page := renderHTML(v)
	files := map[string][]byte{
		"fleet.md":   renderMarkdown(v),
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

// Matrix marks. A rule a repository's configuration does not select is
// different from one it selects that does not apply there.
const (
	markNotSelected = "·"
	markNA          = "-"
)

type cell struct {
	Mark, Title string
}

// cellFor summarizes a rule across the root and every scope: the worst
// status wins, and a failing mark gets `*` when `fix --apply` repairs it.
func cellFor(r RepoReport, rule string) cell {
	best := cell{markNotSelected, "not selected"}
	rank := -1
	fixable := false
	for _, res := range r.Results {
		if res.Rule.ID != rule {
			continue
		}
		c, k := cellOf(res)
		if k > rank {
			best, rank = c, k
		}
		fixable = fixable || (res.Status == model.StatusFail && res.Fixable)
	}
	if fixable && rank >= 3 && rank <= 5 {
		best.Mark += "*"
		best.Title += " (fleetlint fix --apply)"
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
		return cell{"base", prefix + "baselined"}, 2
	case model.StatusNotApplicable:
		return cell{markNA, "n/a"}, 0
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

// ruleStat is one rule across the fleet; counts are repositories.
type ruleStat struct {
	ID                                                            string
	Worst                                                         model.Severity
	Pass, Fail, FailErr, Fixable, Errs, Excepted, NA, NotSelected int
}

func (s ruleStat) applicable() bool { return s.Pass+s.Fail+s.Errs+s.Excepted > 0 }

// view is what every renderer shows, computed once.
type view struct {
	rep          *Report
	stats        []ruleStat // applicable somewhere, in rule order
	inapplicable []string   // selected somewhere, applicable nowhere
}

func newView(rep *Report) *view {
	v := &view{rep: rep}
	for _, rule := range rep.Rules {
		s := statFor(rep, rule)
		if s.applicable() {
			v.stats = append(v.stats, s)
		} else {
			v.inapplicable = append(v.inapplicable, rule)
		}
	}
	return v
}

func statFor(rep *Report, rule string) ruleStat {
	s := ruleStat{ID: rule}
	for _, r := range rep.Repos {
		if r.Err == "" {
			s.add(r, cellFor(r, rule).Mark)
		}
	}
	return s
}

// add counts one repository's matrix mark for the rule.
func (s *ruleStat) add(r RepoReport, mark string) {
	switch strings.TrimSuffix(mark, "*") {
	case "ok":
		s.Pass++
	case "FAIL", "warn", "info":
		s.Fail++
		if strings.HasSuffix(mark, "*") {
			s.Fixable++
		}
		sev := worstFailing(r, s.ID)
		if sev == model.SeverityError {
			s.FailErr++
		}
		if s.Fail == 1 || sev > s.Worst {
			s.Worst = sev
		}
	case "ERR":
		s.Errs++
	case "exc", "base":
		s.Excepted++
	case markNA:
		s.NA++
	default:
		s.NotSelected++
	}
}

func worstFailing(r RepoReport, rule string) model.Severity {
	worst := model.SeverityInfo
	for _, res := range r.Results {
		if res.Rule.ID == rule && res.Status == model.StatusFail && res.Severity > worst {
			worst = res.Severity
		}
	}
	return worst
}

func (v *view) headline() string {
	var withErrors, unchecked, ruleErrs, fixable int
	for _, r := range v.rep.Repos {
		switch {
		case r.Err != "":
			unchecked++
			continue
		case r.Summary.Errors > 0:
			withErrors++
		}
		ruleErrs += r.Summary.Error
		fixable += r.Fixable()
	}
	return fmt.Sprintf("%d repositories: %d with errors, %d could not be checked · %d rule errors · %d findings fixable with `fleetlint fix --apply`",
		len(v.rep.Repos), withErrors, unchecked, ruleErrs, fixable)
}

func (v *view) generated() string {
	rep := v.rep
	return fmt.Sprintf("Generated %s · %d repositories · %d rules · fleetlint %s, catalog %s", rep.GeneratedAt.Format("2006-01-02 15:04 MST"),
		len(rep.Repos), len(rep.Rules), orUnknown(rep.Tool), orUnknown(rep.Catalog))
}

// levers ranks the failing rules by how much of the fleet they hold back:
// repositories failing at error severity first (they fail CI), then all
// failing ones. The cumulative columns show how much fixing the first n closes.
func (v *view) levers() table {
	failing := make([]ruleStat, 0, len(v.stats))
	total, totalErr := 0, 0
	for _, s := range v.stats {
		if s.Fail > 0 {
			failing = append(failing, s)
			total += s.Fail
			totalErr += s.FailErr
		}
	}
	sort.SliceStable(failing, func(i, j int) bool {
		a, b := failing[i], failing[j]
		if a.FailErr != b.FailErr {
			return a.FailErr > b.FailErr
		}
		if a.Fail != b.Fail {
			return a.Fail > b.Fail
		}
		return a.Fixable > b.Fixable
	})
	t := table{
		Title: "Top levers",
		Head:  []string{"#", "Rule", "Severity", "Failing in", "Fixable by `fix --apply`", "Errors closed (cumulative)", "All failures closed (cumulative)"},
	}
	share := func(n, of int) string { return fmt.Sprintf("%.0f%%", 100*float64(n)/float64(max(of, 1))) }
	cum, cumErr, errRules := 0, 0, 0
	for i, s := range failing[:min(len(failing), 10)] {
		cum += s.Fail
		cumErr += s.FailErr
		if s.FailErr > 0 {
			errRules = i + 1
		}
		t.Rows = append(t.Rows, []tcell{
			num(i + 1), code(s.ID),
			{Text: s.Worst.String(), Class: sevClass(s.Worst)},
			txt(fmt.Sprintf("%d of %d", s.Fail, s.Pass+s.Fail+s.Excepted)), numIf(s.Fixable, "ok"),
			txt(share(cumErr, totalErr)), txt(share(cum, total)),
		})
	}
	if len(t.Rows) > 0 {
		t.Intro = fmt.Sprintf("Rules ranked by the repositories they fail at error severity, then by all failures. %d failing rule × repository pairs, %d of them errors (these fail CI). Fixing the first %d rules closes %s of the errors; the long tail of warnings is in the rules table.",
			total, totalErr, max(errRules, 1), share(cumErr, totalErr))
	}
	return t
}

func sevClass(s model.Severity) string {
	switch s {
	case model.SeverityError:
		return "err"
	case model.SeverityWarning:
		return "warn"
	case model.SeverityInfo:
	}
	return "na"
}

// column is one column of the repository table; optional ones appear
// only when some repository has a value for them.
type column struct {
	head string
	cell func(RepoReport) tcell
}

func (v *view) repoColumns() []column {
	any := func(pred func(RepoReport) bool) bool {
		for _, r := range v.rep.Repos {
			if pred(r) {
				return true
			}
		}
		return false
	}
	// checked leaves the cell empty for a repository that could not be checked.
	checked := func(f func(RepoReport) tcell) func(RepoReport) tcell {
		return func(r RepoReport) tcell {
			if r.Err != "" {
				return txt("")
			}
			return f(r)
		}
	}
	cols := []column{{"Repository", func(r RepoReport) tcell { return tcell{Text: r.Name, Href: "actions/" + r.Name + ".html"} }}}
	if any(func(r RepoReport) bool { return r.Group != "" }) {
		cols = append(cols, column{"Group", func(r RepoReport) tcell { return txt(r.Group) }})
	}
	if any(func(r RepoReport) bool { return r.Team != "" }) {
		cols = append(cols, column{"Team", func(r RepoReport) tcell { return txt(r.Team) }})
	}
	cols = append(cols,
		column{"Stacks", func(r RepoReport) tcell {
			if r.Err != "" {
				return tcell{Text: "could not be checked: " + r.Err, Class: "err"}
			}
			return txt(strings.Join(r.Facts.Stacks, ", "))
		}},
		column{"Tier", checked(func(r RepoReport) tcell { return tierCell(r.Facts) })},
		column{"Visibility", checked(func(r RepoReport) tcell { return visibilityCell(r.Facts.Visibility) })},
		column{"Compliance", checked(func(r RepoReport) tcell {
			pct := r.Compliance() * 100
			return tcell{Text: fmt.Sprintf("%.0f%%", pct), Bar: &pct, Sort: fmt.Sprintf("%.3f", pct)}
		})},
		column{"Errors", checked(func(r RepoReport) tcell { return numIf(r.Summary.Errors, "err") })},
		column{"Warnings", checked(func(r RepoReport) tcell { return numIf(r.Summary.Warnings, "warn") })},
		column{"Fixable", checked(func(r RepoReport) tcell { return numIf(r.Fixable(), "ok") })},
		column{"Rule errors", checked(func(r RepoReport) tcell { return numIf(r.Summary.Error, "err") })},
	)
	if any(func(r RepoReport) bool { return r.Summary.Excepted > 0 }) {
		cols = append(cols, column{"Excepted", checked(func(r RepoReport) tcell { return num(r.Summary.Excepted) })})
	}
	if any(func(r RepoReport) bool { return len(r.Disabled) > 0 }) {
		cols = append(cols, column{"Disabled", checked(func(r RepoReport) tcell { return num(len(r.Disabled)) })})
	}
	return append(cols,
		column{"Checked", checked(func(r RepoReport) tcell { return checkedCell(r.Git) })},
		column{"Last commit", checked(func(r RepoReport) tcell { return lastCommitCell(r.Git, v) })},
	)
}

func (v *view) repositories() table {
	cols := v.repoColumns()
	t := table{
		Title: "Repositories", Sortable: true, ID: "repos",
		Intro: "Most urgent first. Compliance is the share of applicable rules that pass or are excepted; rules that could not be evaluated are left out and counted under rule errors. A tier that is not configured is a guess: see facts to pin.",
	}
	for _, c := range cols {
		t.Head = append(t.Head, c.head)
	}
	for _, r := range v.rep.Repos {
		row := make([]tcell, 0, len(cols))
		for _, c := range cols {
			row = append(row, c.cell(r))
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}

func tierCell(f facts.Facts) tcell {
	c := tcell{Text: fmt.Sprint(f.Tier), Title: f.TierSource(), Sort: fmt.Sprint(f.Tier)}
	switch f.Sources["tier"] {
	case facts.SourceInferred:
		c.Text += " (inferred)"
		c.Class = "warn"
	case facts.SourceDefault:
		c.Text += " (default)"
		c.Class = "warn"
	case facts.SourceConfigured, facts.SourceDetected:
	}
	return c
}

func visibilityCell(vis facts.Visibility) tcell {
	c := txt(string(vis))
	if vis == facts.VisibilityUnknown || vis == "" {
		c.Text, c.Class = "unknown", "warn"
	}
	return c
}

func checkedCell(g *GitState) tcell {
	if g == nil {
		return tcell{Text: "no git", Class: "warn"}
	}
	s := orUnknown(g.Branch)
	if g.Commit != "" {
		s += " @ " + g.Commit[:min(len(g.Commit), 7)]
	}
	c := tcell{Text: s}
	if g.Dirty > 0 {
		c.Text += fmt.Sprintf(", %d uncommitted", g.Dirty)
		c.Class = "warn"
		c.Title = "the working tree was checked, not the commit"
	}
	return c
}

func lastCommitCell(g *GitState, v *view) tcell {
	if g == nil || g.LastCommit.IsZero() {
		return txt("")
	}
	c := tcell{Text: g.LastCommit.Format("2006-01-02"), Sort: g.LastCommit.Format("20060102")}
	if g.Stale(v.rep.GeneratedAt) {
		c.Text += " (stale)"
		c.Class = "warn"
		c.Title = "no commit for more than a year: archive it or drop it from the fleet"
	}
	return c
}

func (v *view) ruleErrors() table {
	t := table{
		Title: "Rule errors",
		Intro: "These rules could not be evaluated, so they say nothing about the repository. The cause is usually a file fleetlint cannot parse; fix the rule or the file, never count them as compliance.",
		Head:  []string{"Repository", "Rule", "Scope", "Error"},
	}
	for _, r := range v.rep.Repos {
		for _, res := range r.Results {
			if res.Status == model.StatusError {
				t.Rows = append(t.Rows, []tcell{txt(r.Name), code(res.Rule.ID), txt(res.Scope), {Text: res.Err, Class: "err"}})
			}
		}
	}
	return t
}

// factsToPin lists repositories whose tier or visibility was guessed: both
// decide which rules apply and how severe they are.
func (v *view) factsToPin() table {
	t := table{
		Title: "Facts to pin",
		Intro: "Tier and visibility decide which rules apply and how severe they are. For these repositories they were guessed or are unknown (the forge did not answer without a token). Pin them in `.fleetlint.yaml`.",
		Head:  []string{"Repository", "Tier", "Tier source", "Visibility", "Add to .fleetlint.yaml"},
	}
	for _, r := range v.rep.Repos {
		if r.Err != "" {
			continue
		}
		f := r.Facts
		var pins []string
		if f.Sources["tier"] != facts.SourceConfigured {
			pins = append(pins, fmt.Sprintf("tier: %d", f.Tier))
		}
		if f.Visibility == facts.VisibilityUnknown || f.Visibility == "" {
			pins = append(pins, "visibility: public|private")
		}
		if len(pins) == 0 {
			continue
		}
		t.Rows = append(t.Rows, []tcell{txt(r.Name), num(f.Tier), txt(f.TierSource()), visibilityCell(f.Visibility), code("facts: { " + strings.Join(pins, ", ") + " }")})
	}
	return t
}

func (v *view) rules() table {
	t := table{
		Title: "Rules", Sortable: true,
		Intro: "Counts are repositories. Not selected: the repository's configuration does not include the rule; n/a: it is selected but its stack, tier or condition does not apply.",
		Head:  []string{"Rule", "Pass", "Fail", "Fixable", "Rule errors", "Excepted", "n/a", "Not selected"},
	}
	for _, s := range v.stats {
		t.Rows = append(t.Rows, []tcell{code(s.ID), num(s.Pass), numIf(s.Fail, sevClass(s.Worst)), numIf(s.Fixable, "ok"), numIf(s.Errs, "err"), num(s.Excepted), num(s.NA), num(s.NotSelected)})
	}
	return t
}

func (v *view) inapplicableNote() string {
	if len(v.inapplicable) == 0 {
		return ""
	}
	return fmt.Sprintf("Selected but applicable in no repository (%d, left out of the tables): %s.", len(v.inapplicable), strings.Join(v.inapplicable, ", "))
}

func (v *view) unversioned() table {
	t := table{
		Title: "Not under version control",
		Intro: "Discovery found these project directories outside any repository of their own. fleetlint cannot check them; put them under git or exclude them with `--exclude`.",
		Head:  []string{"Directory"},
	}
	for _, u := range v.rep.Unversioned {
		t.Rows = append(t.Rows, []tcell{txt(u)})
	}
	return t
}

func (v *view) policyTables() []table {
	cols := []string{"Repository", "Rule", "Layer", "Where", "Reason"}
	return []table{
		{Title: "Disabled rules", Head: cols, Rows: collectDisabled(v.rep, false)},
		{Title: "Lowered severities", Head: cols, Rows: collectDisabled(v.rep, true)},
		{Title: "Exceptions", Head: []string{"Repository", "Team", "Rule", "Reason", "Until"}, Rows: collectExceptions(v.rep)},
	}
}

// sections is the report in order; Markdown and HTML render the same list
// and differ only in the matrix.
func (v *view) sections() (before, after []table) {
	before = []table{
		v.levers(), v.repositories(),
		teamsTable("Groups", "Group", v.rep.Groups), teamsTable("Teams", "Team", v.rep.Teams),
		v.ruleErrors(), v.rules(),
	}
	after = append([]table{v.factsToPin(), v.unversioned()}, v.policyTables()...)
	return before, after
}

func renderMarkdown(v *view) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# Fleet report\n\n%s\n\n%s\n", v.generated(), v.headline())
	before, after := v.sections()
	for _, t := range before {
		t.markdown(&b)
	}
	if note := v.inapplicableNote(); note != "" {
		b.WriteString("\n" + note + "\n")
	}
	// Rules down, repositories across: the rule list is long, the fleet usually is not.
	b.WriteString("\n## Matrix\n\n| Rule |")
	for _, r := range v.rep.Repos {
		fmt.Fprintf(&b, " %s |", escapeCell(r.Name))
	}
	b.WriteString("\n|---|" + strings.Repeat("---|", len(v.rep.Repos)) + "\n")
	for _, s := range v.stats {
		fmt.Fprintf(&b, "| `%s` |", s.ID)
		for _, r := range v.rep.Repos {
			fmt.Fprintf(&b, " %s |", cellFor(r, s.ID).Mark)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n" + legend + "\n")
	for _, t := range after {
		t.markdown(&b)
	}
	return []byte(b.String())
}

const legend = "ok pass · FAIL error · warn warning · info info · `*` fixable with `fleetlint fix --apply` · ERR rule error · exc excepted · base baselined · - not applicable · · not selected"

func collectDisabled(rep *Report, weakened bool) [][]tcell {
	var rows [][]tcell
	for _, r := range rep.Repos {
		list := r.Disabled
		if weakened {
			list = r.Weakened
		}
		for _, d := range list {
			rows = append(rows, []tcell{txt(r.Name), code(d.ID), txt(d.Layer), txt(d.Where), txt(d.Reason)})
		}
	}
	return rows
}

// escapeCell makes repository-derived text safe inside a Markdown table cell.
func escapeCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " ")
}

func collectExceptions(rep *Report) [][]tcell {
	var rows [][]tcell
	for _, r := range rep.Repos {
		for _, ex := range r.Exceptions {
			until := txt(ex.Until)
			switch days, dated := daysLeft(ex, rep.GeneratedAt); {
			case ex.Until == "":
				until.Text = "no deadline"
			case ex.Expired:
				until.Text += " (expired)"
				until.Class = "err"
			case dated && days <= expiryWindowDays:
				until.Text += fmt.Sprintf(" (in %d days)", days)
				until.Class = "warn"
			}
			rows = append(rows, []tcell{txt(r.Name), txt(r.Team), code(ex.Rule), txt(ex.Reason), until})
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

func renderHTML(v *view) []byte {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Fleet report</title><style>` + baseCSS + reportCSS + `</style></head><body>`)
	fmt.Fprintf(&b, "<h1>Fleet report</h1><p class=muted>%s</p><p><strong>%s</strong></p>", html.EscapeString(v.generated()), inlineHTML(v.headline()))
	before, after := v.sections()
	for _, t := range before {
		t.html(&b)
	}
	if note := v.inapplicableNote(); note != "" {
		b.WriteString("<p class=muted>" + html.EscapeString(note) + "</p>")
	}
	b.WriteString(`<h2>Matrix</h2><div class=filter><input type=search id=mf placeholder="filter rules (e.g. release/ or lint)"><label><input type=checkbox id=mfail> only rules that fail somewhere</label></div><div class=wrap><table id=matrix><thead><tr><th>Rule</th>`)
	for _, r := range v.rep.Repos {
		fmt.Fprintf(&b, "<th class=rot><div>%s</div></th>", html.EscapeString(r.Name))
	}
	b.WriteString("</tr></thead><tbody>")
	for _, s := range v.stats {
		fmt.Fprintf(&b, "<tr data-fail=%d><td><code>%s</code></td>", s.Fail+s.Errs, html.EscapeString(s.ID))
		for _, r := range v.rep.Repos {
			c := cellFor(r, s.ID)
			fmt.Fprintf(&b, "<td class=\"c %s\"><abbr title=\"%s\">%s</abbr></td>", markClass(c.Mark), html.EscapeString(c.Title), html.EscapeString(c.Mark))
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</tbody></table></div><p class=muted>" + inlineHTML(legend) + "</p>")
	for _, t := range after {
		t.html(&b)
	}
	b.WriteString(pageScript + "</body></html>\n")
	return []byte(b.String())
}

// pageScript sorts tables with class "sort" by the clicked column and
// filters the matrix. The report reads fine without it.
const pageScript = `<script>
document.querySelectorAll("table.sort").forEach(function(t){t.querySelectorAll("th").forEach(function(th,i){th.addEventListener("click",function(){
var tb=t.tBodies[0],rows=Array.from(tb.rows),asc=th.dataset.dir!=="asc";t.querySelectorAll("th").forEach(function(h){delete h.dataset.dir});th.dataset.dir=asc?"asc":"desc";
function val(r){var c=r.cells[i];if(!c)return"";var v=c.dataset.v!==undefined?c.dataset.v:c.textContent.trim();var n=parseFloat(v);return isNaN(n)?v.toLowerCase():n}
rows.sort(function(a,b){var x=val(a),y=val(b);if(typeof x!==typeof y){x=String(x);y=String(y)}return(x<y?-1:x>y?1:0)*(asc?1:-1)});rows.forEach(function(r){tb.appendChild(r)})})})});
(function(){var q=document.getElementById("mf"),f=document.getElementById("mfail");if(!q)return;function run(){var s=q.value.toLowerCase();
document.querySelectorAll("#matrix tbody tr").forEach(function(r){var show=r.cells[0].textContent.toLowerCase().indexOf(s)>=0&&(!f.checked||r.dataset.fail!=="0");r.style.display=show?"":"none"})}
q.addEventListener("input",run);f.addEventListener("change",run)})();
</script>`

func markClass(mark string) string {
	switch strings.TrimSuffix(mark, "*") {
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
