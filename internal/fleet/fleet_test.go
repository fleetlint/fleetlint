package fleet_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fleetlint/fleetlint/internal/fleet"
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
		".fleetlint.yaml": "version: 1\nextends: [fleetlint:minimal]\nfacts: {tier: 3}\nrules:\n  repo/gitignore-present: {enabled: false, reason: \"demo\"}\n",
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
	if len(rep.Repos) != 3 || rep.Repos[0].Name != "clean" {
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
	for _, f := range []string{"fleet.md", "fleet.html", "fleet.json", "fleet.csv", "index.html", "actions/clean.md", "actions/messy.md", "actions/messy.html", "actions/missing.md", "history/2026-10-04T120000Z.json"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("missing output %s", f)
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
	actions, _ := os.ReadFile(filepath.Join(out, "actions/messy.md"))
	if !strings.Contains(string(actions), "repo/no-tracked-junk") {
		t.Fatalf("actions should list the failing rule:\n%s", actions)
	}
	htmlOut, _ := os.ReadFile(filepath.Join(out, "fleet.html"))
	if !strings.Contains(string(htmlOut), `href="actions/messy.html"`) || strings.Contains(string(htmlOut), "<script") {
		t.Fatal("html should link actions and contain no script")
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
	// second run with update reuses the clone
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
}

func TestComplianceWithoutApplicableRules(t *testing.T) {
	t.Parallel()
	if c := (fleet.RepoReport{}).Compliance(); c != 0 {
		t.Fatalf("no applicable rules is no evidence of compliance, got %v", c)
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
