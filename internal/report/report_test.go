package report_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/report"
)

func sampleRun() *engine.Run {
	rule := func(id string, sev model.Severity) model.Rule {
		return model.Rule{ID: id, Title: "title " + id, Severity: sev, Requirement: "req", Fix: model.Fix{Human: "do it", Agent: "agent does it"}}
	}
	return &engine.Run{
		Facts: facts.Facts{Name: "demo", Stacks: []string{"go"}, Tier: 2, Sources: map[string]facts.Source{"tier": facts.SourceConfigured}},
		Config: &config.Effective{
			Disabled: []config.Disabled{{ID: "hooks/config-present", Reason: "no hooks | yet", Where: "web/.fleetlint.yaml", Layer: "repo"}},
		},
		Results: []model.Result{
			{
				Rule: rule("repo/junk", model.SeverityError), Status: model.StatusFail, Severity: model.SeverityError,
				Findings: []model.Finding{{Path: "a.log", Line: 3, Message: "junk | tracked\n# not a heading"}},
			},
			{
				Rule: rule("repo/warn", model.SeverityWarning), Status: model.StatusFail, Severity: model.SeverityWarning,
				Findings: []model.Finding{{Message: "soft"}}, Fixable: true,
			},
			{Rule: rule("repo/ok", model.SeverityError), Status: model.StatusPass, Severity: model.SeverityError},
			{Rule: rule("repo/na", model.SeverityError), Status: model.StatusNotApplicable, Severity: model.SeverityError, Evidence: "stacks [go] not in [rust]"},
			{Rule: rule("repo/err", model.SeverityError), Status: model.StatusError, Severity: model.SeverityError, Err: "boom"},
			{Rule: rule("repo/exc", model.SeverityError), Status: model.StatusExcepted, Severity: model.SeverityError, Evidence: "exception: later"},
		},
	}
}

func render(t *testing.T, f report.Format, opts report.Options) string {
	t.Helper()
	var b bytes.Buffer
	if err := report.Write(&b, sampleRun(), f, opts); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestTableRenderer(t *testing.T) {
	t.Parallel()
	out := render(t, report.FormatTable, report.Options{})
	for _, want := range []string{"FAIL  repo/junk", "warn  repo/warn", "ERR   repo/err", "a.log:3", "junk | tracked", "fix: do it", "hooks/config-present", "web/.fleetlint.yaml"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "warn  repo/warn *") || strings.Contains(out, "repo/junk *") || !strings.Contains(out, "* 1 of these can be fixed mechanically") {
		t.Errorf("only the fixable finding is starred, with a legend:\n%s", out)
	}
	if strings.Contains(out, "repo/ok") || strings.Contains(out, "repo/na") {
		t.Error("passes and n/a hidden unless verbose")
	}
	verbose := render(t, report.FormatTable, report.Options{Verbose: true})
	if !strings.Contains(verbose, "ok    repo/ok") || !strings.Contains(verbose, "-     repo/na") || !strings.Contains(verbose, "exc   repo/exc") {
		t.Errorf("verbose table should list every status:\n%s", verbose)
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("no color codes without Color")
	}
	if !strings.Contains(render(t, report.FormatTable, report.Options{Color: true}), "\x1b[") {
		t.Error("Color should paint the output")
	}
}

func TestTableShowsExceptionDeadlines(t *testing.T) {
	t.Parallel()
	excepted := func(id string, ex *model.Exception) model.Result {
		return model.Result{
			Rule: model.Rule{ID: id, Title: id}, Status: model.StatusExcepted, Severity: model.SeverityError,
			Evidence: "evidence not shown", Findings: []model.Finding{{Message: "m", Exception: ex}},
		}
	}
	run := &engine.Run{Facts: facts.Facts{Name: "demo", Sources: map[string]facts.Source{}}, Config: &config.Effective{}, Results: []model.Result{
		excepted("repo/dated", &model.Exception{Rule: "repo/dated", Reason: "migration", Until: "2027-01-31"}),
		excepted("repo/open", &model.Exception{Rule: "repo/open", Reason: "legacy"}),
	}}
	var b bytes.Buffer
	if err := report.Write(&b, run, report.FormatTable, report.Options{Verbose: true}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"excepted: migration (until 2027-01-31)", "excepted: legacy (no deadline)"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "evidence not shown") {
		t.Errorf("the exception text replaces the evidence:\n%s", out)
	}
}

func TestMarkdownRendererEscapesCells(t *testing.T) {
	t.Parallel()
	out := render(t, report.FormatMarkdown, report.Options{})
	if !strings.Contains(out, "| FAIL | `repo/junk` |") || !strings.Contains(out, "| warn | `repo/warn` |") {
		t.Errorf("markdown rows:\n%s", out)
	}
	if strings.Contains(out, "junk | tracked") {
		t.Error("pipes inside cells must be escaped")
	}
	if !strings.Contains(out, `junk \| tracked`) {
		t.Errorf("escaped pipe expected:\n%s", out)
	}
	if strings.Contains(out, "\n# not a heading") {
		t.Error("finding text must not inject headings")
	}
}

func TestAgentRendererFencesFindings(t *testing.T) {
	t.Parallel()
	out := render(t, report.FormatAgent, report.Options{})
	if !strings.Contains(out, "## 1.") || !strings.Contains(out, "Instruction: agent does it") {
		t.Errorf("agent output:\n%s", out)
	}
	if strings.Contains(out, "\n# not a heading") {
		t.Error("finding text must appear inside a code fence, never as markdown")
	}
	if !strings.Contains(out, "```") {
		t.Error("findings should be fenced")
	}
}

func TestJSONAndSARIFRenderers(t *testing.T) {
	t.Parallel()
	var doc map[string]any
	if err := json.Unmarshal([]byte(render(t, report.FormatJSON, report.Options{})), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["repo"] == nil || doc["results"] == nil || doc["disabled"] == nil {
		t.Errorf("json keys: %v", doc)
	}
	if d, _ := doc["disabled"].([]any); len(d) != 1 || d[0].(map[string]any)["Layer"] != "repo" {
		t.Errorf("disabled entries must carry their layer: %v", doc["disabled"])
	}
	var sarif map[string]any
	if err := json.Unmarshal([]byte(render(t, report.FormatSARIF, report.Options{Version: "1.2.3"})), &sarif); err != nil {
		t.Fatal(err)
	}
	runs := sarif["runs"].([]any)
	driver := runs[0].(map[string]any)["tool"].(map[string]any)["driver"].(map[string]any)
	if driver["version"] != "1.2.3" {
		t.Errorf("sarif driver version: %v", driver)
	}
	results := runs[0].(map[string]any)["results"].([]any)
	if len(results) < 2 {
		t.Errorf("sarif should carry failing results, got %d", len(results))
	}
}

func TestUnknownFormat(t *testing.T) {
	t.Parallel()
	if err := report.Write(&bytes.Buffer{}, sampleRun(), "nope", report.Options{}); err == nil {
		t.Fatal("unknown format must error")
	}
}

// Every format says which fleetlint and which catalog produced it.
func TestReportsNameToolAndCatalog(t *testing.T) {
	t.Parallel()
	opts := report.Options{Version: "1.2.3", Catalog: "v0.4.0"}
	for _, f := range []report.Format{report.FormatTable, report.FormatMarkdown, report.FormatAgent} {
		if out := render(t, f, opts); !strings.Contains(out, "fleetlint 1.2.3, catalog v0.4.0") {
			t.Errorf("%s output lacks the versions:\n%s", f, out)
		}
	}
	var doc struct {
		Tool struct{ Name, Version, Catalog string } `json:"tool"`
	}
	if err := json.Unmarshal([]byte(render(t, report.FormatJSON, opts)), &doc); err != nil || doc.Tool.Version != "1.2.3" || doc.Tool.Catalog != "v0.4.0" {
		t.Errorf("json tool: %+v err=%v", doc.Tool, err)
	}
	if out := render(t, report.FormatSARIF, opts); !strings.Contains(out, `"catalog": "v0.4.0"`) {
		t.Errorf("sarif lacks the catalog version:\n%s", out)
	}
	if out := render(t, report.FormatTable, report.Options{}); !strings.Contains(out, "fleetlint unknown, catalog unknown") {
		t.Errorf("missing versions are stated, not left out:\n%s", out)
	}
}
