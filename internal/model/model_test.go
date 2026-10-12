package model_test

import (
	"testing"

	"github.com/fleetlint/fleetlint/internal/model"
)

func TestSeverityRoundTrip(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"info", "warning", "error"} {
		s, err := model.ParseSeverity(name)
		if err != nil || s.String() != name {
			t.Errorf("%s: %v %v", name, s, err)
		}
	}
	if _, err := model.ParseSeverity("fatal"); err == nil {
		t.Error("an unknown severity is an error")
	}
	if model.SeverityError <= model.SeverityWarning || model.SeverityWarning <= model.SeverityInfo {
		t.Error("severities order from info to error")
	}
}

func TestSummarize(t *testing.T) {
	t.Parallel()
	c := model.Summarize([]model.Result{
		{Status: model.StatusPass},
		{Status: model.StatusExcepted},
		{Status: model.StatusBaselined},
		{Status: model.StatusNotApplicable},
		{Status: model.StatusError},
		{Status: model.StatusFail, Severity: model.SeverityError},
		{Status: model.StatusFail, Severity: model.SeverityWarning},
		{Status: model.StatusFail, Severity: model.SeverityInfo},
	})
	want := model.Counts{Pass: 1, Excepted: 1, Baselined: 1, NotApplicable: 1, Error: 1, Fail: 3, Errors: 1, Warnings: 1, Infos: 1}
	if c != want {
		t.Errorf("got %+v, want %+v", c, want)
	}
}

func TestFloorAndApplicability(t *testing.T) {
	t.Parallel()
	if f := (model.Rule{Severity: model.SeverityError}).Floor(); f != model.SeverityInfo {
		t.Errorf("unlocked: %s", f)
	}
	if f := (model.Rule{Severity: model.SeverityError, Locked: true}).Floor(); f != model.SeverityError {
		t.Errorf("locked: %s", f)
	}
	if f := (model.Rule{Severity: model.SeverityError, MinSeverity: "warning"}).Floor(); f != model.SeverityWarning {
		t.Errorf("min_severity: %s", f)
	}
	r := model.Rule{Tiers: []int{1, 2}, Stacks: []string{"go"}}
	if !r.AppliesToTier(1) || r.AppliesToTier(3) || !r.AppliesToStacks([]string{"node", "go"}) || r.AppliesToStacks([]string{"node"}) {
		t.Error("tiers and stacks")
	}
	if !(model.Rule{Stacks: []string{"any"}}).AppliesToStacks(nil) {
		t.Error("any matches every stack")
	}
	if (model.Rule{Scope: model.ScopeRoot}).AppliesInScope("web", false) || !(model.Rule{Scope: model.ScopeMembers}).AppliesInScope("web", false) {
		t.Error("root and member scopes")
	}
	if (model.Rule{Stacks: []string{"go"}}).AppliesInScope("", true) {
		t.Error("a per-stack rule skips a root that only hosts members")
	}
}

// A clone shares nothing a later layer could change with the original.
func TestCloneIsIndependent(t *testing.T) {
	t.Parallel()
	no := false
	orig := model.Rule{
		Tiers: []int{1}, Params: map[string]any{"list": []any{"a"}, "nested": map[string]any{"k": "v"}},
		Fix:             model.Fix{Actions: []map[string]any{{"template": "t"}}},
		Accept:          []model.Satisfier{{Expr: "true"}},
		AllowExceptions: &no,
	}
	c := orig.Clone()
	c.Tiers[0] = 3
	c.Params["list"].([]any)[0] = "b"
	c.Params["nested"].(map[string]any)["k"] = "w"
	c.Fix.Actions[0]["template"] = "u"
	c.Accept[0].Expr = "false"
	*c.AllowExceptions = true
	if orig.Tiers[0] != 1 || orig.Params["list"].([]any)[0] != "a" || orig.Params["nested"].(map[string]any)["k"] != "v" ||
		orig.Fix.Actions[0]["template"] != "t" || orig.Accept[0].Expr != "true" || *orig.AllowExceptions {
		t.Errorf("the original changed: %+v", orig)
	}
}

func TestAcceptedSatisfiersAreNamed(t *testing.T) {
	t.Parallel()
	got := (model.Rule{Accept: []model.Satisfier{{Expr: "a"}, {Name: "own", Expr: "b"}, {Name: "empty"}}}).AcceptedSatisfiers()
	if len(got) != 2 || got[0].Name != "accept[0]" || got[1].Name != "own" {
		t.Errorf("got %+v", got)
	}
}
