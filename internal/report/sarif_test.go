package report_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/report"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

// sarifRun covers every kind of result: findings with and without a path
// (at the root, in a scope with its own configuration file, in one
// without), an excepted and a baselined finding, a rule error, a pass and
// an n/a. The root has a README.md but no .fleetlint.yaml.
func sarifRun(t *testing.T) (*engine.Run, string) {
	t.Helper()
	root := t.TempDir()
	for _, f := range []string{"README.md", "web/.fleetlint.yaml", "lib/go.mod"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, f)), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, f), []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rule := func(id string, sev model.Severity) model.Rule {
		return model.Rule{ID: id, Title: "title " + id, Severity: sev, Requirement: "req", Rationale: "why", Fix: model.Fix{Human: "do it"}, Source: "fleetlint:minimal", Label: "preset"}
	}
	sast := rule("ci/sast", model.SeverityError)
	sast.Refs = []string{"https://example.org/ssdf"}
	run := &engine.Run{
		Facts:  facts.Facts{Name: "demo"},
		Config: &config.Effective{},
		Results: []model.Result{
			{Rule: rule("repo/junk", model.SeverityError), Status: model.StatusFail, Severity: model.SeverityError, Findings: []model.Finding{{Path: "a.log", Line: 3, Message: "junk tracked"}}},
			{Rule: sast, Status: model.StatusFail, Severity: model.SeverityError, Findings: []model.Finding{{Message: "no SAST workflow"}}},
			{Rule: rule("docs/readme", model.SeverityWarning), Scope: "web", Status: model.StatusFail, Severity: model.SeverityWarning, Findings: []model.Finding{{Message: "no README"}}},
			{Rule: rule("docs/license", model.SeverityInfo), Scope: "lib", Status: model.StatusFail, Severity: model.SeverityInfo, Findings: []model.Finding{{Message: "no LICENSE"}}},
			{Rule: rule("repo/exc", model.SeverityError), Status: model.StatusExcepted, Severity: model.SeverityError, Findings: []model.Finding{{Path: "vendor/x.go", Message: "vendored", Exception: &model.Exception{Rule: "repo/exc", Reason: "vendored on purpose", Until: "2027-01-01"}}}},
			{Rule: rule("repo/base", model.SeverityWarning), Status: model.StatusBaselined, Severity: model.SeverityWarning, Findings: []model.Finding{{Message: "old debt", Baselined: true}}},
			{Rule: rule("repo/err", model.SeverityError), Status: model.StatusError, Severity: model.SeverityError, Err: "boom"},
			{Rule: rule("repo/ok", model.SeverityError), Status: model.StatusPass, Severity: model.SeverityError},
			{Rule: rule("repo/na", model.SeverityError), Status: model.StatusNotApplicable, Severity: model.SeverityError, Evidence: "no stack"},
		},
	}
	return run, root
}

func renderSARIF(t *testing.T, run *engine.Run, opts report.Options) (string, map[string]any) {
	t.Helper()
	var b bytes.Buffer
	if err := report.Write(&b, run, report.FormatSARIF, opts); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	return b.String(), doc["runs"].([]any)[0].(map[string]any)
}

func TestSARIFValidatesAgainstSchema(t *testing.T) {
	t.Parallel()
	run, root := sarifRun(t)
	out, _ := renderSARIF(t, run, report.Options{Version: "1.2.3", Root: root})
	schema, err := jsonschema.NewCompiler().Compile("testdata/sarif-schema-2.1.0.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(doc); err != nil {
		t.Fatalf("sarif does not validate against the OASIS 2.1.0 schema: %v\n%s", err, out)
	}
}

// The SARIF output is a contract with code scanning: pinned in a golden
// file, regenerate with UPDATE_GOLDEN=1.
func TestSARIFShape(t *testing.T) {
	t.Parallel()
	run, root := sarifRun(t)
	out, sr := renderSARIF(t, run, report.Options{Version: "1.2.3", Catalog: "v0.4.0", Name: "demo", Root: root})
	testutil.Golden(t, "testdata/sarif.golden.json", out)

	if id := sr["automationDetails"].(map[string]any)["id"]; id != "fleetlint/demo" {
		t.Errorf("automationDetails.id = %v", id)
	}
	results := sr["results"].([]any)
	if len(results) != 6 {
		t.Fatalf("want 6 results (passes, n/a and rule errors are none), got %d", len(results))
	}
	// Every result has a location; pathless findings are anchored per scope.
	anchors := map[string]string{"repo/junk": "a.log", "ci/sast": "README.md", "docs/readme": "web/.fleetlint.yaml", "docs/license": "lib/.fleetlint.yaml", "repo/exc": "vendor/x.go", "repo/base": "README.md"}
	for _, r := range results {
		res := r.(map[string]any)
		id := res["ruleId"].(string)
		loc := res["locations"].([]any)[0].(map[string]any)["physicalLocation"].(map[string]any)
		if uri := loc["artifactLocation"].(map[string]any)["uri"]; uri != anchors[id] {
			t.Errorf("%s: location %v, want %s", id, uri, anchors[id])
		}
		if id == "repo/junk" && loc["region"].(map[string]any)["startLine"] != 3.0 {
			t.Errorf("real path keeps its line: %v", loc)
		}
		if fp := res["partialFingerprints"].(map[string]any)["fleetlint/v1"]; fp == nil || fp == "" {
			t.Errorf("%s: no fingerprint", id)
		}
	}
	sum := sha256.Sum256([]byte("repo/junk\x00\x00a.log\x00junk tracked"))
	if fp := results[0].(map[string]any)["partialFingerprints"].(map[string]any)["fleetlint/v1"]; fp != hex.EncodeToString(sum[:]) {
		t.Errorf("fingerprint is sha256(rule, scope, path, message): %v", fp)
	}
	if msg := results[1].(map[string]any)["message"].(map[string]any)["text"]; msg != "no SAST workflow" {
		t.Errorf("anchoring keeps the message: %v", msg)
	}
	checkSARIFSuppressions(t, results)
	checkSARIFRules(t, sr)
	inv := sr["invocations"].([]any)[0].(map[string]any)
	notes := inv["toolExecutionNotifications"].([]any)
	if inv["executionSuccessful"] != false || len(notes) != 1 {
		t.Fatalf("a rule error fails the invocation with one notification: %v", inv)
	}
	note := notes[0].(map[string]any)
	if note["level"] != "error" || note["associatedRule"].(map[string]any)["id"] != "repo/err" || !strings.Contains(note["message"].(map[string]any)["text"].(string), "boom") {
		t.Errorf("notification: %v", note)
	}
}

func checkSARIFSuppressions(t *testing.T, results []any) {
	t.Helper()
	want := map[string]string{"repo/exc": "vendored on purpose", "repo/base": "baselined: grandfathered by the baseline file"}
	for _, r := range results {
		res := r.(map[string]any)
		id := res["ruleId"].(string)
		sup, _ := res["suppressions"].([]any)
		if want[id] == "" {
			if len(sup) != 0 {
				t.Errorf("%s: unexpected suppression %v", id, sup)
			}
			continue
		}
		if len(sup) != 1 {
			t.Errorf("%s: want one suppression, got %v", id, sup)
			continue
		}
		s := sup[0].(map[string]any)
		if s["kind"] != "external" || s["status"] != "accepted" || s["justification"] != want[id] {
			t.Errorf("%s: suppression %v", id, s)
		}
	}
}

func checkSARIFRules(t *testing.T, sr map[string]any) {
	t.Helper()
	rules := sr["tool"].(map[string]any)["driver"].(map[string]any)["rules"].([]any)
	byID := map[string]map[string]any{}
	for _, r := range rules {
		rule := r.(map[string]any)
		byID[rule["id"].(string)] = rule
	}
	if len(byID) != 7 || byID["repo/err"] == nil || byID["repo/ok"] != nil {
		t.Errorf("rules: failing, suppressed and erroring rules, not passing ones: %v", byID)
	}
	if uri := byID["ci/sast"]["helpUri"]; uri != "https://fleetlint.org/rules/ci--sast/" {
		t.Errorf("helpUri: %v", uri)
	}
	props := byID["ci/sast"]["properties"].(map[string]any)
	tags, _ := json.Marshal(props["tags"])
	if string(tags) != `["repository-setup","ci","security"]` || props["security-severity"] != "8.0" || props["source"] != "fleetlint:minimal" || props["label"] != "preset" {
		t.Errorf("security rule properties: %v", props)
	}
	if refs, _ := json.Marshal(props["refs"]); string(refs) != `["https://example.org/ssdf"]` {
		t.Errorf("refs: %s", refs)
	}
	docs := byID["docs/readme"]["properties"].(map[string]any)
	tags, _ = json.Marshal(docs["tags"])
	if string(tags) != `["repository-setup","docs"]` || docs["security-severity"] != nil || docs["refs"] != nil {
		t.Errorf("non-security rule properties: %v", docs)
	}
	if byID["repo/base"]["properties"].(map[string]any)["security-severity"] != "5.0" || byID["docs/license"]["properties"].(map[string]any)["security-severity"] != nil {
		t.Error("security-severity follows the severity")
	}
}

func TestSARIFNamesTheRunAfterTheRoot(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "my-repo")
	if err := os.Mkdir(root, 0o750); err != nil {
		t.Fatal(err)
	}
	_, sr := renderSARIF(t, sampleRun(), report.Options{Root: root})
	if id := sr["automationDetails"].(map[string]any)["id"]; id != "fleetlint/my-repo" {
		t.Errorf("automationDetails.id = %v", id)
	}
	// Without a configuration file or README, the configuration file's path is the anchor anyway.
	for _, r := range sr["results"].([]any) {
		res := r.(map[string]any)
		if uri := res["locations"].([]any)[0].(map[string]any)["physicalLocation"].(map[string]any)["artifactLocation"].(map[string]any)["uri"]; res["ruleId"] == "repo/warn" && uri != ".fleetlint.yaml" {
			t.Errorf("pathless finding anchored to %v", uri)
		}
	}
	run := sampleRun()
	run.Results = run.Results[:1]
	_, sr = renderSARIF(t, run, report.Options{Root: root})
	if inv := sr["invocations"].([]any)[0].(map[string]any); inv["executionSuccessful"] != true || inv["toolExecutionNotifications"] != nil {
		t.Errorf("no rule error: successful, no notifications: %v", inv)
	}
}
