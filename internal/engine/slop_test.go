package engine_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

const withSlop = "version: 1\nextends: [fleetlint:recommended, fleetlint:slop]\nfacts: {tier: 1, visibility: public}\n"

// Every slop rule gets a fixture that trips it and a clean counterpart. The
// fixtures live in a data file so this package's source does not contain
// the patterns the rules look for.
func TestSlopRules(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/slop_fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases map[string]struct {
		Pass map[string]string `json:"pass"`
		Fail map[string]string `json:"fail"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for id, tc := range cases {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			pass := run(t, withDefaults(tc.Pass), withSlop)
			if s := status(t, pass, id); s.Status != model.StatusPass {
				t.Errorf("pass fixture: %s %q %+v", s.Status, s.Err, s.Findings)
			}
			fail := run(t, withDefaults(tc.Fail), withSlop)
			if s := status(t, fail, id); s.Status != model.StatusFail {
				t.Fatalf("fail fixture: %s %q evidence=%q", s.Status, s.Err, s.Evidence)
			}
		})
	}
}

func TestSlopHistoryRules(t *testing.T) {
	t.Parallel()
	r := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n", ".fleetlint.yaml": withSlop})
	results := evaluate(t, r)
	for _, id := range []string{"slop/vague-commit-messages", "slop/non-conventional-commits", "slop/huge-commits"} {
		if s := status(t, results, id); s.Status != model.StatusPass {
			t.Errorf("%s on a clean history: %s %q %+v", id, s.Status, s.Err, s.Findings)
		}
	}
	testutil.WriteFiles(t, r.Root, map[string]string{"big.txt": strings.Repeat("line\n", 1600)})
	gitCmd(t, r.Root, "add", "-A")
	gitCmd(t, r.Root, "commit", "-q", "-m", "wip")
	results = evaluate(t, r)
	for _, id := range []string{"slop/vague-commit-messages", "slop/non-conventional-commits", "slop/huge-commits"} {
		if s := status(t, results, id); s.Status != model.StatusFail || len(s.Findings) != 1 {
			t.Errorf("%s after a 1600-line commit named wip: %s %q %+v", id, s.Status, s.Err, s.Findings)
		}
	}
}

// An initial import, git's own revert subjects and a vague description after a
// type are judged as such.
func TestSlopHistoryExemptions(t *testing.T) {
	t.Parallel()
	r := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n", ".fleetlint.yaml": withSlop, "big.txt": strings.Repeat("line\n", 1600)})
	gitCmd(t, r.Root, "commit", "--allow-empty", "-q", "-m", "Revert \"feat: a\"")
	results := evaluate(t, r)
	for _, id := range []string{"slop/non-conventional-commits", "slop/huge-commits", "slop/vague-commit-messages"} {
		if s := status(t, results, id); s.Status != model.StatusPass {
			t.Errorf("%s: a large root commit and a git revert pass: %s %+v", id, s.Status, s.Findings)
		}
	}
	gitCmd(t, r.Root, "commit", "--allow-empty", "-q", "-m", "fix: update")
	if s := status(t, evaluate(t, r), "slop/vague-commit-messages"); s.Status != model.StatusFail || len(s.Findings) != 1 {
		t.Errorf("a type does not make an empty description specific: %s %+v", s.Status, s.Findings)
	}
}
