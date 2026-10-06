package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/model"
)

func TestWithDefaultCommand(t *testing.T) {
	t.Parallel()
	root := &cobra.Command{Use: "fleetlint"}
	root.AddCommand(&cobra.Command{Use: "init"}, &cobra.Command{Use: "check", Aliases: []string{"lint"}})
	cases := []struct {
		in, want string
	}{
		{"", "check"},
		{"-f json", "check -f json"},
		{"-C dir init", "-C dir init"},
		{"--path x lint", "--path x lint"},
		{"init --force", "init --force"},
		{"--version", "--version"},
		{"-h", "-h"},
		{"-C dir --deep", "check -C dir --deep"},
	}
	for _, c := range cases {
		got := strings.Join(withDefaultCommand(root, strings.Fields(c.in)), " ")
		if got != c.want {
			t.Errorf("withDefaultCommand(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExitFor(t *testing.T) {
	t.Parallel()
	res := func(status model.Status, sev model.Severity) model.Result {
		return model.Result{Status: status, Severity: sev}
	}
	warn := &engine.Run{Results: []model.Result{res(model.StatusFail, model.SeverityWarning), res(model.StatusPass, model.SeverityError)}}
	info := &engine.Run{Results: []model.Result{res(model.StatusFail, model.SeverityInfo)}}
	ruleErr := &engine.Run{Results: []model.Result{res(model.StatusError, model.SeverityInfo)}}
	clean := &engine.Run{Results: []model.Result{res(model.StatusPass, model.SeverityError), res(model.StatusExcepted, model.SeverityError)}}

	if exitFor(warn, "error") != ExitOK || exitFor(warn, "") != ExitOK {
		t.Error("warnings do not fail at the default threshold")
	}
	if exitFor(warn, "warning") != ExitFindings || exitFor(info, "warning") != ExitOK {
		t.Error("--fail-on warning fails on warnings only")
	}
	if exitFor(info, "info") != ExitFindings {
		t.Error("--fail-on info fails on any finding")
	}
	if exitFor(ruleErr, "never") != ExitFindings {
		t.Error("a rule error is never silenced by --fail-on")
	}
	if exitFor(clean, "info") != ExitOK {
		t.Error("excepted findings do not fail")
	}
}

func TestReleaseText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		rel  facts.Release
		want string
	}{
		{"none", facts.Release{}, "none detected"},
		{"mechanisms only", facts.Release{Exists: true, Mechanisms: []string{"goreleaser", "tag-workflow"}}, "goreleaser, tag-workflow"},
		{"with latest tag", facts.Release{Exists: true, Mechanisms: []string{"tag-workflow"}, LatestTag: "v1.2.3"}, "tag-workflow latest=v1.2.3"},
	}
	for _, c := range cases {
		if got := releaseText(facts.Facts{Release: c.rel}); got != c.want {
			t.Errorf("%s: releaseText = %q, want %q", c.name, got, c.want)
		}
	}
}
