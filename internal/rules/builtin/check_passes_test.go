package builtin_test

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/rules"
	_ "github.com/fleetlint/fleetlint/internal/rules/builtin"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

func checker(t *testing.T) rules.DeepChecker {
	t.Helper()
	c, ok := rules.Lookup("quality/check-passes")
	if !ok {
		t.Fatal("quality/check-passes is not registered")
	}
	dc, ok := c.(rules.DeepChecker)
	if !ok {
		t.Fatal("quality/check-passes must be a DeepChecker")
	}
	return dc
}

// makeContext builds a deep rule context for a repository whose Makefile has the given check recipe.
func makeContext(t *testing.T, recipe string, params map[string]any) rules.Context {
	t.Helper()
	r := testutil.Fixture(t, map[string]string{"Makefile": "check:\n\t@" + recipe + "\n"})
	f := facts.Discover(r, facts.Overrides{})
	if f.TaskRunner.Kind != "make" {
		t.Fatalf("task runner %q, want make", f.TaskRunner.Kind)
	}
	return rules.Context{Repo: r, Facts: f, Params: params, Deep: true}
}

func TestCheckPassesNeedsDeep(t *testing.T) {
	t.Parallel()
	c := checker(t)
	if _, err := c.Check(rules.Context{}); !errors.Is(err, rules.ErrNeedsDeep) {
		t.Errorf("Check: %v, want ErrNeedsDeep", err)
	}
	if _, _, err := c.CheckDeep(context.Background(), rules.Context{Deep: false}); !errors.Is(err, rules.ErrNeedsDeep) {
		t.Errorf("CheckDeep without Deep: %v, want ErrNeedsDeep", err)
	}
}

func TestCheckPassesWithoutRunner(t *testing.T) {
	t.Parallel()
	rc := rules.Context{Deep: true, Facts: facts.Facts{TaskRunner: facts.TaskRunner{Kind: "none"}}}
	findings, _, err := checker(t).CheckDeep(context.Background(), rc)
	if err != nil || len(findings) != 1 || !strings.Contains(findings[0].Message, "no task runner") {
		t.Fatalf("findings=%+v err=%v", findings, err)
	}
}

func TestCheckPassesBudgetParam(t *testing.T) {
	t.Parallel()
	for name, params := range map[string]map[string]any{"not a string": {"budget": 10}, "not a duration": {"budget": "soon"}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rc := rules.Context{Deep: true, Facts: facts.Facts{TaskRunner: facts.TaskRunner{Kind: "make"}}, Params: params}
			if _, _, err := checker(t).CheckDeep(context.Background(), rc); err == nil || !strings.Contains(err.Error(), "budget") {
				t.Fatalf("err=%v, want a budget error", err)
			}
		})
	}
}

func TestCheckPassesRunsMakeCheck(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("make"); err != nil {
		t.Fatal("make is required: the gate itself runs through it")
	}
	cases := []struct {
		name, recipe string
		params       map[string]any
		wantEvidence []string
		wantMessage  []string
		notMessage   []string
	}{
		{name: "green", recipe: "echo ok"},
		{
			name: "red with a long log", recipe: "for i in $$(seq 1 20); do echo line$$i; done; exit 1",
			wantEvidence: []string{"failed"},
			wantMessage:  []string{"`make check` failed after", "line20 | make", "Error 1"},
			notMessage:   []string{"line15"}, // never within the last five lines, whatever make adds
		},
		{
			name: "over budget and red", recipe: "sleep 0.6; exit 1", params: map[string]any{"budget": "1ms"},
			wantEvidence: []string{"failed", "slow"},
			wantMessage:  []string{"over the 1ms budget"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			findings, evidence, err := checker(t).CheckDeep(context.Background(), makeContext(t, c.recipe, c.params))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(evidence, "make check in ") {
				t.Errorf("evidence %q should name the command and duration", evidence)
			}
			got := make([]string, len(findings))
			all := ""
			for i, f := range findings {
				got[i] = f.Evidence
				all += f.Message + "\n"
			}
			if strings.Join(got, ",") != strings.Join(c.wantEvidence, ",") {
				t.Fatalf("findings %v, want %v:\n%s", got, c.wantEvidence, all)
			}
			for _, want := range c.wantMessage {
				if !strings.Contains(all, want) {
					t.Errorf("messages lack %q:\n%s", want, all)
				}
			}
			for _, not := range c.notMessage {
				if strings.Contains(all, not) {
					t.Errorf("messages must not contain %q (only the last lines):\n%s", not, all)
				}
			}
		})
	}
}
