package fleet_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/fleet"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/repo"
	_ "github.com/fleetlint/fleetlint/internal/rules/builtin"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

func TestRunAndWriteLocalRepos(t *testing.T) {
	t.Parallel()
	clean := testutil.GitFixture(t, map[string]string{
		"go.mod": "module x\n", ".gitignore": "bin/\n", ".fleetlint.yaml": "version: 1\nextends: [fleetlint:minimal]\nfacts: {tier: 3}\n",
	})
	messy := testutil.GitFixture(t, map[string]string{
		"go.mod": "module y\n", ".DS_Store": "x",
		".fleetlint.yaml": "version: 1\nextends: [fleetlint:minimal]\nfacts: {tier: 3}\noverrides:\n  repo/gitignore-present: {enabled: false, reason: \"demo\"}\n",
	})
	spec := &fleet.Spec{Version: 1, Repos: []fleet.Entry{
		{Path: clean.Root, Name: "clean", Team: "a"},
		{Path: messy.Root, Name: "messy", Team: "b"},
		{Path: filepath.Join(t.TempDir(), "missing"), Name: "missing"},
	}}
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	rep, err := fleet.Run(context.Background(), spec, fleet.Options{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	// Most urgent first: the unchecked repository, then the one with errors.
	if len(rep.Repos) != 3 || rep.Repos[0].Name != "missing" || rep.Repos[1].Name != "messy" {
		t.Fatalf("repos: %+v", rep.Repos)
	}
	byName := map[string]fleet.RepoReport{}
	for _, r := range rep.Repos {
		byName[r.Name] = r
	}
	if byName["clean"].Compliance() != 1 || byName["clean"].Summary.Errors != 0 {
		t.Fatalf("clean repo should be fully compliant: %+v", byName["clean"].Summary)
	}
	if byName["messy"].Summary.Errors == 0 || len(byName["messy"].Disabled) != 1 {
		t.Fatalf("messy repo should have errors and one disabled rule: %+v", byName["messy"].Summary)
	}
	if byName["missing"].Err == "" {
		t.Fatal("missing path must be reported as an error, not skipped")
	}

	out := t.TempDir()
	if err := fleet.Write(out, rep); err != nil {
		t.Fatal(err)
	}
	// fleet.json is a contract: its key paths are pinned (see jsonFormat).
	doc, err := os.ReadFile(filepath.Join(out, "fleet.json"))
	if err != nil {
		t.Fatal(err)
	}
	testutil.Golden(t, "testdata/fleet-json.keys", testutil.JSONKeys(t, doc, ".definitions", ".repos[].facts.sources"))
	for _, f := range []string{"fleet.md", "fleet.html", "fleet.json", "fleet.csv", "index.html", "actions/clean.md", "actions/messy.md", "actions/messy.html", "actions/missing.md", "sarif/clean.sarif", "sarif/messy.sarif", "history/2026-10-04T120000Z.json"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("missing output %s", f)
		}
	}
	// An unchecked repository has no results to upload.
	if _, err := os.Stat(filepath.Join(out, "sarif/missing.sarif")); err == nil {
		t.Error("no sarif for a repository that could not be checked")
	}
	sarifOut, _ := os.ReadFile(filepath.Join(out, "sarif/messy.sarif"))
	for _, want := range []string{`"id": "fleetlint/messy/"`, `"ruleId": "repo/no-tracked-junk"`, `"uri": ".DS_Store"`} {
		if !strings.Contains(string(sarifOut), want) {
			t.Errorf("sarif lacks %s:\n%s", want, sarifOut)
		}
	}
	md, _ := os.ReadFile(filepath.Join(out, "fleet.md"))
	if !strings.Contains(string(md), ", catalog ") {
		t.Errorf("the fleet report names the catalog version:\n%s", md)
	}
	if !strings.Contains(string(md), "## Teams") || !strings.Contains(string(md), "| b | 1 | 0 |") {
		t.Errorf("markdown lacks the team summary:\n%s", md)
	}
	csvOut, _ := os.ReadFile(filepath.Join(out, "fleet.csv"))
	if lines := strings.Split(strings.TrimSpace(string(csvOut)), "\n"); len(lines) != len(rep.Repos)+1 || !strings.HasPrefix(lines[0], "repository,team,") {
		t.Errorf("csv must have a header and one row per repository:\n%s", csvOut)
	}
	if !strings.Contains(string(md), "| messy | b |") || !strings.Contains(string(md), "## Disabled rules") || !strings.Contains(string(md), "| repo | .fleetlint.yaml |") || !strings.Contains(string(md), "demo") {
		t.Fatalf("markdown lacks expected sections:\n%s", md)
	}
	// The matrix has rules down and repositories across.
	if i := strings.Index(string(md), "## Matrix\n\n| Rule |"); i < 0 || !strings.Contains(string(md)[i:], "\n| `repo/no-tracked-junk` |") {
		t.Errorf("matrix rows are rules:\n%s", md)
	}
	actions, _ := os.ReadFile(filepath.Join(out, "actions/messy.md"))
	if !strings.Contains(string(actions), "repo/no-tracked-junk") {
		t.Fatalf("actions should list the failing rule:\n%s", actions)
	}
	htmlOut, _ := os.ReadFile(filepath.Join(out, "fleet.html"))
	// The one script is the report's own sort and filter code; nothing from a repository.
	if !strings.Contains(string(htmlOut), `href="actions/messy.html"`) || strings.Count(string(htmlOut), "<script") != 1 {
		t.Fatal("html should link actions and contain only its own script")
	}
	actionsPage, _ := os.ReadFile(filepath.Join(out, "actions/messy.html"))
	if !strings.Contains(string(actionsPage), "<pre>") || !strings.Contains(string(actionsPage), "repo/no-tracked-junk") || strings.Contains(string(actionsPage), "<script") {
		t.Fatalf("actions page should render findings in a block without script:\n%s", actionsPage)
	}
}

func TestLoadSpecValidation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(body string) string {
		p := filepath.Join(dir, "fleet.yaml")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if _, err := fleet.LoadSpec(write("version: 2\nrepos: []\n")); err == nil {
		t.Fatal("bad version must fail")
	}
	if _, err := fleet.LoadSpec(write("version: 1\nrepos:\n  - {path: a, url: b}\n")); err == nil {
		t.Fatal("path and url together must fail")
	}
	if _, err := fleet.LoadSpec(write("version: 1\nrepoz: []\n")); err == nil {
		t.Fatal("unknown key must fail")
	}
	spec, err := fleet.LoadSpec(write("version: 1\nrepos:\n  - path: ../x\n  - url: https://example.com/org/some-repo.git\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(spec.Repos[0].Path) || spec.Repos[0].Name != "x" || spec.Repos[1].Name != "some-repo" {
		t.Fatalf("spec normalization: %+v", spec.Repos)
	}
}

func TestURLEntryIsClonedIntoCache(t *testing.T) {
	t.Parallel()
	origin := testutil.GitFixture(t, map[string]string{"go.mod": "module z\n", ".fleetlint.yaml": "version: 1\nextends: [fleetlint:minimal]\n"})
	cache := t.TempDir()
	spec := &fleet.Spec{Version: 1, Repos: []fleet.Entry{{URL: origin.Root, Name: "cloned"}}}
	rep, err := fleet.Run(context.Background(), spec, fleet.Options{Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Repos[0].Err != "" || rep.Repos[0].Facts.Name == "" {
		t.Fatalf("clone failed: %+v", rep.Repos[0])
	}
	entries, _ := os.ReadDir(cache)
	if len(entries) != 1 {
		t.Fatalf("expected one cached clone, got %d", len(entries))
	}
	// second run with update reuses the clone; its pull must start no
	// background git in the test's directory
	testutil.NoBackgroundGit(t, rep.Repos[0].Root)
	rep2, err := fleet.Run(context.Background(), spec, fleet.Options{Cache: cache, Update: true})
	if err != nil || rep2.Repos[0].Err != "" {
		t.Fatalf("update run: %v %+v", err, rep2.Repos[0])
	}
	noCache := &fleet.Spec{Version: 1, Repos: []fleet.Entry{{URL: origin.Root}}}
	rep3, _ := fleet.Run(context.Background(), noCache, fleet.Options{})
	if !strings.Contains(rep3.Repos[0].Err, "cache directory is required") {
		t.Fatalf("url without cache must report an error: %+v", rep3.Repos[0])
	}
}

func TestSpecNamesAreSafeAndUnique(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(body string) string {
		p := filepath.Join(dir, "fleet.yaml")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	spec, err := fleet.LoadSpec(write("version: 1\nrepos:\n  - {path: a, name: \"../evil name\"}\n  - {url: \"https://x.example/o/r.git\"}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(spec.Repos[0].Name, "/ \\") {
		t.Fatalf("sanitized name still has path characters: %q", spec.Repos[0].Name)
	}
	if _, err := fleet.LoadSpec(write("version: 1\nrepos:\n  - {path: a, name: same}\n  - {path: b, name: same}\n")); err == nil {
		t.Fatal("duplicate names must fail: their action files would overwrite each other")
	}
	if _, err := fleet.LoadSpec(write("version: 1\nrepos:\n  - {path: a, name: \"///\"}\n")); err == nil {
		t.Fatal("a name that is empty after sanitizing must fail")
	}
	for _, bad := range []string{"--upload-pack=evil", "ext::sh -c id", "ftp://x/y"} {
		if _, err := fleet.LoadSpec(write("version: 1\nrepos:\n  - {url: \"" + bad + "\"}\n")); err == nil {
			t.Errorf("url %q must be rejected", bad)
		}
	}
	_, err = fleet.LoadSpec(write("version: 1\nrepos:\n  - {url: \"https://u:ghp_secret@git.example/o/r.git\"}\n"))
	if err == nil || strings.Contains(err.Error(), "ghp_secret") {
		t.Errorf("a url with credentials must be rejected without repeating them: %v", err)
	}
	if _, err := fleet.LoadSpec(write("version: 1\nrepos:\n  - {url: \"ssh://git@git.example/o/r.git\"}\n")); err != nil {
		t.Errorf("an ssh user is no credential: %v", err)
	}
}

func TestComplianceWithoutApplicableRules(t *testing.T) {
	t.Parallel()
	if c := (fleet.RepoReport{}).Compliance(); c != 0 {
		t.Fatalf("no applicable rules is no evidence of compliance, got %v", c)
	}
}

func TestComplianceLeavesRuleErrorsOut(t *testing.T) {
	t.Parallel()
	r := fleet.RepoReport{Summary: model.Counts{Pass: 1, Fail: 1, Error: 1}}
	if c := r.Compliance(); c != 0.5 {
		t.Fatalf("a rule that could not be evaluated says nothing about the repository: compliance %v, want 0.5", c)
	}
}

func TestActionsHTMLEscapes(t *testing.T) {
	t.Parallel()
	md := "# fleetlint: actions for x\n\n## 1. a/b: T (error)\n\nFindings:\n```\n<script>alert(1)</script> & more\n```\n\nInstruction: do <it>\n"
	out := string(fleet.ActionsHTMLForTest("x", []byte(md)))
	if strings.Contains(out, "<script>") || !strings.Contains(out, "&lt;script&gt;") || !strings.Contains(out, "do &lt;it&gt;") {
		t.Fatalf("content must be escaped:\n%s", out)
	}
	if !strings.Contains(out, "<h2>1. a/b: T (error)</h2>") {
		t.Fatalf("headings should render:\n%s", out)
	}
}

func TestFleetRequireReportsMissingBaseline(t *testing.T) {
	t.Parallel()
	r := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n", ".fleetlint.yaml": "version: 1\nextends: [fleetlint:minimal]\n"})
	spec := &fleet.Spec{Version: 1, Repos: []fleet.Entry{{Path: r.Root, Name: "r"}}}
	rep, err := fleet.Run(context.Background(), spec, fleet.Options{Require: []string{"acme-baseline"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rep.Repos[0].Err, "acme-baseline") {
		t.Fatalf("a repository without the required catalog is an error row, got %+v", rep.Repos[0])
	}
	rep, _ = fleet.Run(context.Background(), spec, fleet.Options{Require: []string{"fleetlint:minimal"}})
	if rep.Repos[0].Err != "" {
		t.Fatalf("satisfied requirement: %+v", rep.Repos[0])
	}
}

func TestFleetFileSettingsFillWhatRepositoriesLeaveOpen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	open := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n", ".fleetlint.yaml": "version: 1\nextends: [fleetlint:minimal]\n"})
	pinned := testutil.GitFixture(t, map[string]string{"go.mod": "module y\n", ".fleetlint.yaml": "version: 1\nextends: [fleetlint:minimal]\nfacts: {tier: 3}\n"})
	body := "version: 1\ndefaults: {tier: 2, team: core}\ngroups:\n  tools: {tier: 3, visibility: private}\nrepos:\n" +
		"  - {path: " + open.Root + ", name: grouped, group: tools}\n" +
		"  - {path: " + open.Root + ", name: plain}\n" +
		"  - {path: " + open.Root + ", name: own, group: tools, tier: 1, team: web}\n" +
		"  - {path: " + pinned.Root + ", name: pinned}\n"
	p := filepath.Join(dir, "fleet.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := fleet.LoadSpec(p)
	if err != nil {
		t.Fatal(err)
	}
	asked := 0
	rep, err := fleet.Run(context.Background(), spec, fleet.Options{Visibility: func(context.Context, *repo.Repo, facts.Facts) facts.Visibility {
		asked++
		return facts.VisibilityPublic
	}})
	if err != nil {
		t.Fatal(err)
	}
	type got struct {
		tier       int
		source     facts.Source
		team       string
		visibility facts.Visibility
	}
	want := map[string]got{
		"grouped": {3, facts.SourceFleet, "core", facts.VisibilityPrivate},
		"plain":   {2, facts.SourceFleet, "core", facts.VisibilityPublic},
		"own":     {1, facts.SourceFleet, "web", facts.VisibilityPrivate},
		"pinned":  {3, facts.SourceConfigured, "core", facts.VisibilityPublic},
	}
	for _, r := range rep.Repos {
		if g := (got{r.Facts.Tier, r.Facts.Sources["tier"], r.Team, r.Facts.Visibility}); g != want[r.Name] {
			t.Errorf("%s: %+v, want %+v (entry over group over defaults; the repository's own pin over all)", r.Name, g, want[r.Name])
		}
	}
	if asked != 2 {
		t.Errorf("the forge is asked only where the fleet file sets no visibility: asked %d times, want 2", asked)
	}
	for _, bad := range []string{"defaults: {tier: 4}", "groups: {x: {visibility: internal}}", "repos: [{path: a, tier: 9}]"} {
		if err := os.WriteFile(p, []byte("version: 1\n"+bad+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := fleet.LoadSpec(p); err == nil {
			t.Errorf("%s must be refused", bad)
		}
	}
}

func resultOf(t *testing.T, rr fleet.RepoReport, id string) model.Result {
	t.Helper()
	for _, res := range rr.Results {
		if res.Rule.ID == id {
			return res
		}
	}
	t.Fatalf("%s: no result for %s in %+v", rr.Name, id, rr)
	return model.Result{}
}

// --deep runs a repository's own command rules in local repositories; a
// clone runs them only with DeepClones, and says so in its notes.
func TestDeepSkipsClonesUnlessAsked(t *testing.T) {
	t.Parallel()
	cfg := "version: 1\nextends: [fleetlint:minimal]\nfacts: {tier: 3}\nrules:\n  - id: repo/custom\n    kind: command\n    severity: warning\n    run: ./scripts/check\n    message: x\n    fix: {human: h}\n"
	origin := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n", "scripts/check": "#!/bin/sh\nexit 0\n", ".fleetlint.yaml": cfg})
	if err := os.Chmod(filepath.Join(origin.Root, "scripts", "check"), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, origin.Root, "add", "-A")
	testutil.Git(t, origin.Root, "commit", "-q", "-m", "chore: executable", "--no-verify")
	cases := []struct {
		name  string
		entry fleet.Entry
		opts  fleet.Options
		ran   bool
	}{
		{"local", fleet.Entry{Path: origin.Root, Name: "r"}, fleet.Options{Deep: true}, true},
		{"clone", fleet.Entry{URL: origin.Root, Name: "r"}, fleet.Options{Deep: true}, false},
		{"clone asked", fleet.Entry{URL: origin.Root, Name: "r"}, fleet.Options{Deep: true, DeepClones: true}, true},
		{"clone without deep", fleet.Entry{URL: origin.Root, Name: "r"}, fleet.Options{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			c.opts.Cache = t.TempDir()
			rep, err := fleet.Run(context.Background(), &fleet.Spec{Version: 1, Repos: []fleet.Entry{c.entry}}, c.opts)
			if err != nil || rep.Repos[0].Err != "" {
				t.Fatalf("run: %v %+v", err, rep.Repos[0])
			}
			res := resultOf(t, rep.Repos[0], "repo/custom")
			if ran := res.Status == model.StatusPass; ran != c.ran {
				t.Fatalf("command rule ran: %v, want %v (%+v)", ran, c.ran, res)
			}
			notes := strings.Join(rep.Repos[0].Notes, "\n")
			if skipped := c.opts.Deep && !c.ran; skipped != strings.Contains(notes, "--deep-clones") {
				t.Fatalf("notes %q; a clone skipped under --deep says so, nothing else does", notes)
			}
		})
	}
}

// With Apply, the fixer leaves a working tree with uncommitted changes
// alone unless AllowDirty is set; the repository is still checked and the
// report says why it was not fixed.
func TestFixApplyRefusesDirtyTree(t *testing.T) {
	t.Parallel()
	r := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n", ".DS_Store": "x", ".fleetlint.yaml": "version: 1\nextends: [fleetlint:minimal]\nfacts: {tier: 3}\n"})
	testutil.WriteFiles(t, r.Root, map[string]string{"go.mod": "module x\n\ngo 1.26\n", "wip.txt": "x"})
	spec := &fleet.Spec{Version: 1, Repos: []fleet.Entry{{Path: r.Root, Name: "r"}}}
	run := func(allow bool) (fleet.RepoReport, string) {
		var out strings.Builder
		fixer := &fleet.Fixer{W: &out, Apply: true, AllowDirty: allow, Rules: []string{"repo/no-tracked-junk"}}
		rep, err := fleet.Run(context.Background(), spec, fleet.Options{Fix: fixer.Fix})
		if err != nil {
			t.Fatal(err)
		}
		if err := fixer.Summary(); err != nil {
			t.Fatal(err)
		}
		if err := fleet.Write(filepath.Join(t.TempDir(), "out"), rep); err != nil {
			t.Fatal(err)
		}
		return rep.Repos[0], out.String()
	}
	rr, out := run(false)
	if rr.Err != "" || resultOf(t, rr, "repo/no-tracked-junk").Status != model.StatusFail {
		t.Fatalf("the repository is still checked, unfixed: %+v", rr)
	}
	if len(rr.Notes) != 1 || !strings.Contains(rr.Notes[0], "uncommitted changes") || !strings.Contains(rr.Notes[0], "--allow-dirty") {
		t.Fatalf("notes %q", rr.Notes)
	}
	if !strings.Contains(out, "untrack .DS_Store") || !strings.Contains(out, "not applied: the working tree has uncommitted changes") || !strings.Contains(out, "1 repositories with uncommitted changes (or a state git could not read) were left alone") {
		t.Fatalf("listing:\n%s", out)
	}
	if tracked, _ := r.Git("ls-files"); !strings.Contains(tracked, ".DS_Store") {
		t.Fatal("nothing may be written into a dirty tree")
	}
	rr, out = run(true)
	if len(rr.Notes) != 0 || resultOf(t, rr, "repo/no-tracked-junk").Status != model.StatusPass || !strings.Contains(out, "applied 2 change(s) in 1 repositories") {
		t.Fatalf("with AllowDirty the fix is applied: %+v\n%s", rr, out)
	}
}

// When git cannot tell whether the tree is clean, the fixer writes nothing
// and says why; the repository is still checked.
func TestFixApplyRefusesUnreadableTree(t *testing.T) {
	t.Parallel()
	r := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n", ".DS_Store": "x", ".fleetlint.yaml": "version: 1\nextends: [fleetlint:minimal]\nfacts: {tier: 3}\n"})
	var out strings.Builder
	fixer := &fleet.Fixer{W: &out, Apply: true, Rules: []string{"repo/no-tracked-junk"}}
	// The check has read the index; corrupt it before the fixer asks git.
	corrupt := func(*repo.Repo, *engine.Run) {
		testutil.WriteFiles(t, r.Root, map[string]string{".git/index": "corrupt"})
	}
	rep, err := fleet.Run(context.Background(), &fleet.Spec{Version: 1, Repos: []fleet.Entry{{Path: r.Root, Name: "r"}}}, fleet.Options{Fix: fixer.Fix, MarkFixable: corrupt})
	if err != nil {
		t.Fatal(err)
	}
	rr := rep.Repos[0]
	if rr.Err != "" || len(rr.Notes) != 1 || !strings.Contains(rr.Notes[0], "fixes not applied: cannot tell whether the working tree is clean") {
		t.Fatalf("report %+v", rr)
	}
	if !strings.Contains(out.String(), "not applied: cannot tell") {
		t.Fatalf("listing:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(r.Root, ".gitignore")); err == nil || !strings.Contains(out.String(), ".gitignore") {
		t.Fatal("nothing may be written when git cannot tell")
	}
}

// The notes reach the Markdown report and fleet.json.
func TestNotesAreRendered(t *testing.T) {
	t.Parallel()
	rep := &fleet.Report{GeneratedAt: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC), Repos: []fleet.RepoReport{{Name: "r", Source: "/r", Notes: []string{"fixes not applied: dirty"}}}}
	out := t.TempDir()
	if err := fleet.Write(out, rep); err != nil {
		t.Fatal(err)
	}
	md, _ := os.ReadFile(filepath.Join(out, "fleet.md"))
	js, _ := os.ReadFile(filepath.Join(out, "fleet.json"))
	htmlOut, _ := os.ReadFile(filepath.Join(out, "fleet.html"))
	if !strings.Contains(string(md), "| Notes |") || !strings.Contains(string(md), "fixes not applied: dirty") {
		t.Errorf("markdown lacks the note:\n%s", md)
	}
	if !strings.Contains(string(js), `"notes": [`) || !strings.Contains(string(js), `"fixes not applied: dirty"`) {
		t.Errorf("json lacks the note:\n%s", js)
	}
	if !strings.Contains(string(htmlOut), "fixes not applied: dirty") {
		t.Error("html lacks the note")
	}
}
