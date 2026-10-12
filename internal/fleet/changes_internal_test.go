package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/model"
)

func TestChangesSinceTheLastRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first := triageReport()
	if err := Write(dir, first); err != nil {
		t.Fatal(err)
	}
	md, _ := os.ReadFile(filepath.Join(dir, "fleet.md"))
	if strings.Contains(string(md), "## Since the last run") {
		t.Error("the first run has nothing to compare with")
	}

	second := triageReport()
	second.GeneratedAt = first.GeneratedAt.AddDate(0, 0, 7)
	second.Repos[0].Results[0] = res("a/hooks", model.StatusPass, model.SeverityError, true) // old: fixed
	second.Repos[1].Results = append(second.Repos[1].Results, res("f/new", model.StatusFail, model.SeverityError, false))
	second.Repos = append(second.Repos, RepoReport{Name: "added", Err: "path missing"})
	if err := Write(dir, second); err != nil {
		t.Fatal(err)
	}
	md, _ = os.ReadFile(filepath.Join(dir, "fleet.md"))
	for _, want := range []string{
		"## Since the last run",
		"Compared with the run of 2026-10-09 12:00 UTC: rules failing at error severity 2 -> 2; 1 rule x repository pairs newly failing, 1 fixed.",
		"| added | new in the fleet |",
		"| new | 1 -&gt; 2 | 1 -&gt; 1 | `f/new` |  |",
		"| old | 1 -&gt; 0 | 0 -&gt; 0 |  | `a/hooks` |",
	} {
		if !strings.Contains(string(md), want) {
			t.Errorf("missing %q:\n%s", want, md)
		}
	}

	// A run older than the newest history entry compares with what came before it.
	if prev, err := loadPrevious(dir, first.GeneratedAt); err != nil || prev != nil {
		t.Errorf("nothing precedes the first run: %v, %v", prev, err)
	}
}

func TestRuleIDReadsBothHistoryFormats(t *testing.T) {
	t.Parallel()
	if got := ruleID(2, []byte(`"a/b"`)); got != "a/b" {
		t.Errorf("version 2: %q", got)
	}
	if got := ruleID(1, []byte(`{"id":"a/b","title":"x"}`)); got != "a/b" {
		t.Errorf("version 1: %q", got)
	}
}
