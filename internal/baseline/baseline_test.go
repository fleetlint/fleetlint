package baseline_test

import (
	"testing"
	"time"

	"github.com/fleetlint/fleetlint/internal/baseline"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

func results(paths ...string) []model.Result {
	r := model.Result{Rule: model.Rule{ID: "repo/no-tracked-junk"}, Status: model.StatusFail, Severity: model.SeverityError}
	for _, p := range paths {
		r.Findings = append(r.Findings, model.Finding{Path: p, Message: "junk is tracked"})
	}
	return []model.Result{r}
}

// The fingerprint is pinned: existing baseline files must keep matching.
// It is the first 16 characters of Key, which SARIF uses whole; the anchor
// is not part of it, an artifact's name is.
func TestFingerprintIsAPrefixOfKey(t *testing.T) {
	t.Parallel()
	key := baseline.Key("repo/no-tracked-junk", "", model.Finding{Path: "a.log", Message: "junk is tracked", Anchor: "ignored.md"})
	if key != "d05606907047180b8318d69b3a6344d4184b882065762f64d6eb74b497d8f3ac" {
		t.Fatalf("key %s", key)
	}
	if fp := baseline.Fingerprint("repo/no-tracked-junk", "", model.Finding{Path: "a.log", Message: "junk is tracked"}); fp != "d05606907047180b" {
		t.Fatalf("fingerprint %s", fp)
	}
	if baseline.Key("r", "api", model.Finding{Path: "p", Message: "m"}) != "7748ef15c37c07bca5e4a1df3634b5f1d031f1f7cb0d064752c33e73c8958c58" {
		t.Fatal("the scope is part of the key")
	}
	a := baseline.Key("r", "", model.Finding{Message: "m", Artifact: "ghcr.io/x/a"})
	if a == baseline.Key("r", "", model.Finding{Message: "m", Artifact: "ghcr.io/x/b"}) || a == baseline.Key("r", "", model.Finding{Message: "m"}) {
		t.Error("a named artifact is a finding of its own")
	}
}

func TestBaselineOnlyShrinks(t *testing.T) {
	t.Parallel()
	r, path := testutil.Fixture(t, map[string]string{}), "b.json"
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)

	// first write grandfathers everything
	added, removed, err := baseline.Write(r, path, baseline.Build(results("a.log", "b.log")), nil, false, now)
	if err != nil || added != 2 || removed != 0 {
		t.Fatalf("first write: %d %d %v", added, removed, err)
	}
	b, exists, err := baseline.Load(r, path)
	if err != nil || !exists || b.Len() != 2 {
		t.Fatalf("load: %v %v %d", err, exists, b.Len())
	}

	// a new finding appears and one old one is fixed: new one is refused, old one drops
	added, removed, err = baseline.Write(r, path, baseline.Build(results("a.log", "new.log")), b, false, now.Add(time.Hour))
	if err != nil || added != 0 || removed != 1 {
		t.Fatalf("ratchet write: added=%d removed=%d err=%v", added, removed, err)
	}
	b2, _, _ := baseline.Load(r, path)
	if b2.Len() != 1 || !b2.Has(baseline.Fingerprint("repo/no-tracked-junk", "", model.Finding{Path: "a.log", Message: "junk is tracked"})) {
		t.Fatalf("baseline should keep only a.log: %d", b2.Len())
	}

	// reset grandfathers the new one too
	added, _, err = baseline.Write(r, path, baseline.Build(results("a.log", "new.log")), b2, true, now)
	if err != nil || added != 1 {
		t.Fatalf("reset: added=%d err=%v", added, err)
	}
}

func TestApplyMarksAndDowngrades(t *testing.T) {
	t.Parallel()
	r, path := testutil.Fixture(t, map[string]string{}), "b.json"
	if _, _, err := baseline.Write(r, path, baseline.Build(results("a.log")), nil, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	b, _, _ := baseline.Load(r, path)

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
	r, path := testutil.Fixture(t, map[string]string{}), "b.json"
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	if _, _, err := baseline.Write(r, path, baseline.Build(results("a.log")), nil, false, now); err != nil {
		t.Fatal(err)
	}
	b, _, _ := baseline.Load(r, path)
	// everything fixed: the baseline shrinks to zero entries
	if _, removed, err := baseline.Write(r, path, nil, b, false, now); err != nil || removed != 1 {
		t.Fatalf("shrink to zero: removed=%d err=%v", removed, err)
	}
	empty, exists, err := baseline.Load(r, path)
	if err != nil || !exists || empty.Len() != 0 {
		t.Fatalf("empty baseline should still exist: %v %v %d", err, exists, empty.Len())
	}
	// a regression must not be grandfathered just because the file is empty
	added, _, err := baseline.Write(r, path, baseline.Build(results("again.log")), empty, false, now)
	if err != nil || added != 0 {
		t.Fatalf("empty baseline must refuse new entries without reset: added=%d err=%v", added, err)
	}
}

// Two findings with one fingerprint are two entries: a third copy fails
// and cannot be written in, a fixed copy drops one entry.
func TestDuplicatesAreCounted(t *testing.T) {
	t.Parallel()
	r, path := testutil.Fixture(t, map[string]string{}), "b.json"
	now := time.Now()
	if added, _, err := baseline.Write(r, path, baseline.Build(results("a.log", "a.log")), nil, false, now); err != nil || added != 2 {
		t.Fatalf("first write: %d %v", added, err)
	}
	b, _, _ := baseline.Load(r, path)
	if b.Len() != 2 {
		t.Fatalf("len %d, want 2", b.Len())
	}
	three := results("a.log", "a.log", "a.log")
	if used := b.Apply(three); used != 2 || three[0].Status != model.StatusFail || three[0].Findings[2].Baselined {
		t.Fatalf("the third copy is new: used=%d %+v", used, three[0])
	}
	if added, removed, err := baseline.Write(r, path, baseline.Build(three), b, false, now); err != nil || added != 0 || removed != 0 {
		t.Fatalf("the third copy is refused: added=%d removed=%d %v", added, removed, err)
	}
	if _, removed, _ := baseline.Write(r, path, baseline.Build(results("a.log")), b, false, now); removed != 1 {
		t.Fatalf("a fixed copy drops one entry: removed=%d", removed)
	}
	if b, _, _ := baseline.Load(r, path); b.Len() != 1 {
		t.Fatalf("len %d, want 1", b.Len())
	}
}
