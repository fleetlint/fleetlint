package engine_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/model"
)

// ruleFixture is one rule's passing and failing repository. Config is the
// .fleetlint.yaml (tier 1, public, recommended + slop when empty); Message
// is a substring the first finding of the failing run must contain.
type ruleFixture struct {
	Pass    map[string]string `json:"pass"`
	Fail    map[string]string `json:"fail"`
	Config  string            `json:"config,omitempty"`
	Message string            `json:"message,omitempty"`
}

// Every testdata/rule_fixtures_*.json maps rule ids (an implementation id
// for a capability) to lists of fixtures; each must pass and fail as stated.
func TestRuleFixtures(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("testdata/rule_fixtures_*.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var cases map[string][]ruleFixture
		if err := json.Unmarshal(raw, &cases); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for id, list := range cases {
			for i, tc := range list {
				t.Run(filepath.Base(file)+"/"+id+"/"+string(rune('a'+i)), func(t *testing.T) {
					t.Parallel()
					checkFixture(t, id, tc)
				})
			}
		}
	}
}

func checkFixture(t *testing.T, id string, tc ruleFixture) {
	t.Helper()
	cfg := tc.Config
	if cfg == "" {
		cfg = withSlop
	}
	if tc.Pass != nil {
		if s := status(t, run(t, tc.Pass, cfg), id); s.Status != model.StatusPass {
			t.Errorf("pass fixture: %s %q evidence=%q %+v", s.Status, s.Err, s.Evidence, s.Findings)
		}
	}
	if tc.Fail != nil {
		s := status(t, run(t, tc.Fail, cfg), id)
		if s.Status != model.StatusFail {
			t.Fatalf("fail fixture: %s %q evidence=%q", s.Status, s.Err, s.Evidence)
		}
		if tc.Message != "" && !strings.Contains(s.Findings[0].Message, tc.Message) {
			t.Errorf("fail fixture message %q does not contain %q", s.Findings[0].Message, tc.Message)
		}
	}
}
