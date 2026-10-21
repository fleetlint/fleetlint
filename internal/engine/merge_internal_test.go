package engine

import (
	"testing"

	"github.com/fleetlint/fleetlint/internal/model"
)

// The outcomes of several artifacts add up: a partial's severity only when
// every failing artifact matched a partial, the strongest of them; the
// rule's as soon as one failed outright, whatever the order.
func TestMergeOutcomes(t *testing.T) {
	t.Parallel()
	rule := model.Rule{Severity: model.SeverityError}
	warn := &model.Partial{Name: "by-tag", Severity: model.SeverityWarning}
	info := &model.Partial{Name: "outside", Severity: model.SeverityInfo}
	fail := func(p *model.Partial) outcome {
		return outcome{findings: []model.Finding{{Message: "m"}}, partial: p}
	}
	pass := outcome{evidence: "a: satisfied by x"}
	cases := map[string]struct {
		outs []outcome
		want model.Severity
	}{
		"partials only, strongest wins":  {[]outcome{fail(info), fail(warn)}, model.SeverityWarning},
		"partial then outright":          {[]outcome{fail(warn), fail(nil)}, model.SeverityError},
		"outright then partial":          {[]outcome{fail(nil), fail(warn)}, model.SeverityError},
		"a pass does not lower anything": {[]outcome{pass, fail(info)}, model.SeverityInfo},
		"all pass":                       {[]outcome{pass, pass}, model.SeverityError},
	}
	for name, c := range cases {
		got := merge(c.outs)
		if s := got.severity(rule); s != c.want {
			t.Errorf("%s: severity %s, want %s", name, s, c.want)
		}
	}
	if got := merge([]outcome{pass, fail(nil), pass}); len(got.findings) != 1 || got.evidence != "a: satisfied by x; a: satisfied by x" {
		t.Errorf("findings and evidence add up: %+v", got)
	}
}
