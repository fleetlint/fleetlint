package baseline_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/fleetlint/fleetlint/internal/baseline"
	"github.com/fleetlint/fleetlint/internal/model"
)

func results(paths ...string) []model.Result {
	r := model.Result{Rule: model.Rule{ID: "repo/no-tracked-junk"}, Status: model.StatusFail, Severity: model.SeverityError}
	for _, p := range paths {
		r.Findings = append(r.Findings, model.Finding{Path: p, Message: "junk is tracked"})
	}
	return []model.Result{r}
}

func TestBaselineOnlyShrinks(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "b.json")
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)

	// first write grandfathers everything
	added, removed, err := baseline.Write(path, baseline.Build(results("a.log", "b.log")), nil, false, now)
	if err != nil || added != 2 || removed != 0 {
		t.Fatalf("first write: %d %d %v", added, removed, err)
	}
	b, exists, err := baseline.Load(path)
	if err != nil || !exists || b.Len() != 2 {
		t.Fatalf("load: %v %v %d", err, exists, b.Len())
	}

	// a new finding appears and one old one is fixed: new one is refused, old one drops
	added, removed, err = baseline.Write(path, baseline.Build(results("a.log", "new.log")), b, false, now.Add(time.Hour))
	if err != nil || added != 0 || removed != 1 {
		t.Fatalf("ratchet write: added=%d removed=%d err=%v", added, removed, err)
	}
	b2, _, _ := baseline.Load(path)
	if b2.Len() != 1 || !b2.Has(baseline.Fingerprint("repo/no-tracked-junk", "", model.Finding{Path: "a.log", Message: "junk is tracked"})) {
		t.Fatalf("baseline should keep only a.log: %d", b2.Len())
	}

	// reset grandfathers the new one too
	added, _, err = baseline.Write(path, baseline.Build(results("a.log", "new.log")), b2, true, now)
	if err != nil || added != 1 {
		t.Fatalf("reset: added=%d err=%v", added, err)
	}
}

func TestApplyMarksAndDowngrades(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "b.json")
	if _, _, err := baseline.Write(path, baseline.Build(results("a.log")), nil, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	b, _, _ := baseline.Load(path)

	partial := results("a.log", "b.log")
	if used := b.Apply(partial); used != 1 || partial[0].Status != model.StatusFail || !partial[0].Findings[0].Baselined || partial[0].Findings[1].Baselined {
		t.Fatalf("partial coverage must stay failing with one marked finding: %+v", partial[0])
	}
	full := results("a.log")
	if used := b.Apply(full); used != 1 || full[0].Status != model.StatusBaselined {
		t.Fatalf("full coverage must downgrade to baselined: %+v", full[0])
	}
	if c := model.Summarize(full); c.Baselined != 1 || c.Fail != 0 {
		t.Fatalf("summary: %+v", c)
	}
}

func TestEmptyBaselineDoesNotRegrow(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "b.json")
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	if _, _, err := baseline.Write(path, baseline.Build(results("a.log")), nil, false, now); err != nil {
		t.Fatal(err)
	}
	b, _, _ := baseline.Load(path)
	// everything fixed: the baseline shrinks to zero entries
	if _, removed, err := baseline.Write(path, nil, b, false, now); err != nil || removed != 1 {
		t.Fatalf("shrink to zero: removed=%d err=%v", removed, err)
	}
	empty, exists, err := baseline.Load(path)
	if err != nil || !exists || empty.Len() != 0 {
		t.Fatalf("empty baseline should still exist: %v %v %d", err, exists, empty.Len())
	}
	// a regression must not be grandfathered just because the file is empty
	added, _, err := baseline.Write(path, baseline.Build(results("again.log")), empty, false, now)
	if err != nil || added != 0 {
		t.Fatalf("empty baseline must refuse new entries without reset: added=%d err=%v", added, err)
	}
}
