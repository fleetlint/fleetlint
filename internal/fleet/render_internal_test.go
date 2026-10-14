package fleet

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/model"
)

func res(id string, st model.Status, sev model.Severity, fixable bool) model.Result {
	return model.Result{
		Rule: model.Rule{ID: id, Title: id, Severity: sev}, Status: st, Severity: sev, Fixable: fixable,
		Findings: []model.Finding{{Message: id + " failed"}},
	}
}

func triageReport() *Report {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	inferred := facts.Facts{
		Tier: 1, TierReason: "release pipeline", Visibility: facts.VisibilityUnknown,
		Sources: map[string]facts.Source{"tier": facts.SourceInferred},
	}
	pinned := facts.Facts{Tier: 2, Visibility: facts.VisibilityPrivate, Sources: map[string]facts.Source{"tier": facts.SourceConfigured}}
	mk := func(name string, f facts.Facts, g *GitState, results ...model.Result) RepoReport {
		return RepoReport{Name: name, Facts: f, Git: g, Results: results, Summary: model.Summarize(results)}
	}
	repos := []RepoReport{
		mk("old", pinned, &GitState{Branch: "main", Commit: "abcdef123456", Dirty: 3, LastCommit: now.AddDate(-2, 0, 0)},
			res("a/hooks", model.StatusFail, model.SeverityError, true),
			res("b/parse", model.StatusError, model.SeverityWarning, false),
			res("c/cpp", model.StatusNotApplicable, model.SeverityWarning, false),
			res("d/ok", model.StatusPass, model.SeverityWarning, false)),
		mk("new", inferred, &GitState{Branch: "main", Commit: "123456abcdef", LastCommit: now},
			res("a/hooks", model.StatusFail, model.SeverityError, false),
			res("c/cpp", model.StatusNotApplicable, model.SeverityWarning, false),
			res("e/warn", model.StatusFail, model.SeverityWarning, false)),
	}
	repos[1].Results[1].Err = ""
	repos[0].Results[1].Err = "tsconfig.json: parse json"
	return &Report{GeneratedAt: now, Repos: repos, Rules: []string{"a/hooks", "b/parse", "c/cpp", "d/ok", "e/warn"}}
}

func TestMarkdownTriageSections(t *testing.T) {
	t.Parallel()
	rep := triageReport()
	md := string(renderMarkdown(newView(rep, nil)))
	for _, want := range []string{
		"## Top levers", "| 1 | `a/hooks` | error | 2 of 2 | 1 | 100% |",
		"2 repositories: 2 with errors, 0 could not be checked · 1 rule errors · 1 findings fixable",
		"## Rule errors", "| old | `b/parse` |  | tsconfig.json: parse json |",
		"## Facts to pin", "`facts: { tier: 1, visibility: public\\|private }`", "inferred from release pipeline",
		"main &#64; abcdef1, 3 uncommitted", "(stale)",
		"applicable in no repository (1, left out of the tables): c/cpp.",
		"| `a/hooks` | FAIL* | FAIL |", // fixable marked
		"| `e/warn` | · | warn |",      // not selected differs from n/a
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "| `c/cpp` |") {
		t.Error("a rule applicable nowhere has no row")
	}
	if strings.Contains(md, "| old | 2 |") {
		t.Error("a configured tier is not a fact to pin")
	}
}

func TestUrgentOrder(t *testing.T) {
	t.Parallel()
	a := RepoReport{Name: "B", Summary: model.Counts{Errors: 1}}
	b := RepoReport{Name: "a", Summary: model.Counts{Pass: 1}}
	broken := RepoReport{Name: "z", Err: "clone failed"}
	if !urgent(broken, a) || !urgent(a, b) || urgent(b, a) {
		t.Fatal("unchecked first, then errors")
	}
	if !urgent(RepoReport{Name: "a"}, RepoReport{Name: "B"}) {
		t.Fatal("ties sort by name, case-insensitively")
	}
}

func TestHTMLSharesSectionsAndRendersCode(t *testing.T) {
	t.Parallel()
	page := string(renderHTML(newView(triageReport(), nil)))
	for _, want := range []string{
		"<h2>Top levers</h2>", "<h2>Rules</h2>", "<h2>Rule errors</h2>", "<h2>Facts to pin</h2>",
		"<code>fleetlint fix --apply</code>", `class="bar b-err"`, `id=mf`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("html lacks %q", want)
		}
	}
}

func TestInlineHTML(t *testing.T) {
	t.Parallel()
	if got := inlineHTML("run `a<b>` now"); got != "run <code>a&lt;b&gt;</code> now" {
		t.Errorf("got %q", got)
	}
	if got := inlineHTML("odd ` tick"); got != "odd ` tick" {
		t.Errorf("unbalanced backticks stay text, got %q", got)
	}
}

func TestJSONStoresDefinitionsOnce(t *testing.T) {
	t.Parallel()
	rep := triageReport()
	adjusted := rep.Repos[1].Results[0]
	adjusted.Rule.Params = map[string]any{"required": []any{"check"}}
	rep.Repos[1].Results[0] = adjusted
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Version     int                       `json:"version"`
		Definitions map[string]map[string]any `json:"definitions"`
		Repos       []struct {
			Results []struct {
				Rule       string `json:"rule"`
				Definition string `json:"definition"`
			} `json:"results"`
		} `json:"repos"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Version != jsonFormat || len(out.Definitions) != 6 {
		t.Fatalf("version %d, %d definitions: %v", out.Version, len(out.Definitions), out.Definitions)
	}
	first, second := out.Repos[0].Results[0], out.Repos[1].Results[0]
	if first.Rule != "a/hooks" || first.Definition != "" || second.Rule != "a/hooks" || second.Definition != "a/hooks#2" {
		t.Fatalf("results reference definitions: %+v %+v", first, second)
	}
	if out.Definitions["a/hooks#2"]["params"] == nil {
		t.Fatal("the adjusted definition is stored under its own key")
	}
}

func TestGitStateStale(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	var none *GitState
	if none.Stale(now) || (&GitState{}).Stale(now) || (&GitState{LastCommit: now.AddDate(0, -6, 0)}).Stale(now) {
		t.Fatal("missing or recent commits are not stale")
	}
	if !(&GitState{LastCommit: now.AddDate(-1, -1, 0)}).Stale(now) {
		t.Fatal("thirteen months without a commit is stale")
	}
}

// A rule id in the HTML shows its summary on hover and links to its docs:
// the fleetlint.org page for a built-in rule, else its first reference.
func TestRuleIDsLinkToTheirDocs(t *testing.T) {
	t.Parallel()
	builtin := res("repo/no-tracked-junk", model.StatusFail, model.SeverityError, false)
	builtin.Rule.Title = "No editor, OS or backup files are tracked"
	builtin.Rule.Requirement = "Files such as .DS_Store\n  are never committed. " + strings.Repeat("word ", 100)
	own := res("acme/approved-base", model.StatusFail, model.SeverityWarning, false)
	own.Rule.Refs = []string{"https://wiki.acme.example/base-images"}
	plain := res("acme/no-refs", model.StatusPass, model.SeverityWarning, false)
	rep := &Report{Repos: []RepoReport{{Name: "x", Results: []model.Result{builtin, own, plain}}}, Rules: []string{"acme/approved-base", "acme/no-refs", "repo/no-tracked-junk"}}
	page := string(renderHTML(newView(rep, nil)))
	for _, want := range []string{
		"title=\"No editor, OS or backup files are tracked\nseverity: error\n\nFiles such as .DS_Store are never committed.",
		`<a href="https://fleetlint.org/rules/repo--no-tracked-junk/"><code>repo/no-tracked-junk</code></a>`,
		`<a href="https://wiki.acme.example/base-images"><code>acme/approved-base</code></a>`,
		`<code>acme/no-refs</code></td>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %s", want)
		}
	}
	if d := ruleDocs(rep)["repo/no-tracked-junk"]; len(d.Hint) > maxRuleHint+100 || !strings.HasSuffix(d.Hint, " ...") {
		t.Errorf("a long requirement is cut at a word: %d %q", len(d.Hint), d.Hint[len(d.Hint)-10:])
	}
}
