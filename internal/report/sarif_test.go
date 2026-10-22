package report_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
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
// an n/a. The root has a README.md but no .fleetlint.yaml; the web scope's
// README rule carries a planned fix.
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
	sast.Refs = []model.Ref{{URL: "https://example.org/ssdf", Kind: "standard", Section: "PW.7"}}
	run := &engine.Run{
		Facts:  facts.Facts{Name: "demo"},
		Config: &config.Effective{},
		Results: []model.Result{
			{Rule: rule("repo/junk", model.SeverityError), Status: model.StatusFail, Severity: model.SeverityError, Findings: []model.Finding{{Path: "a.log", Line: 3, Message: "junk tracked"}}},
			{Rule: sast, Status: model.StatusFail, Severity: model.SeverityError, Findings: []model.Finding{{Message: "no SAST workflow"}}},
			{Rule: rule("docs/readme", model.SeverityWarning), Scope: "web", Status: model.StatusFail, Severity: model.SeverityWarning, Findings: []model.Finding{{Message: "no README"}}, Fixable: true, Edits: []model.FileEdit{{Path: "web/README.md", New: "# web\n"}}},
			{Rule: rule("docs/license", model.SeverityInfo), Scope: "lib", Status: model.StatusFail, Severity: model.SeverityInfo, Findings: []model.Finding{{Message: "no LICENSE"}}},
			{Rule: rule("repo/exc", model.SeverityError), Status: model.StatusExcepted, Severity: model.SeverityError, Findings: []model.Finding{{Path: "vendor/x.go", Message: "vendored", Exception: &model.Exception{Rule: "repo/exc", Reason: "vendored on purpose", Until: "2027-01-01", ApprovedBy: "@acme/security"}}}},
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

// sarifRuns returns the log's runs keyed by automation id, and every result
// and rule across them.
func sarifRuns(t *testing.T, out string) (map[string]map[string]any, []any, []any) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	runs := map[string]map[string]any{}
	var results, rules []any
	for _, r := range doc["runs"].([]any) {
		run := r.(map[string]any)
		runs[run["automationDetails"].(map[string]any)["id"].(string)] = run
		results = append(results, run["results"].([]any)...)
		rules = append(rules, run["tool"].(map[string]any)["driver"].(map[string]any)["rules"].([]any)...)
	}
	return runs, results, rules
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

	runs, results, rules := sarifRuns(t, out)
	if len(runs) != 3 || runs["fleetlint/demo/"] == nil || runs["fleetlint/demo/web/"] == nil || runs["fleetlint/demo/lib/"] == nil {
		t.Errorf("one run per scope, keyed by repository and scope: %v", slices.Sorted(maps.Keys(runs)))
	}
	if id := sr["automationDetails"].(map[string]any)["id"]; id != "fleetlint/demo/" {
		t.Errorf("the first run is the root: %v", id)
	}
	if len(results) != 6 {
		t.Fatalf("want 6 results (passes, n/a and rule errors are none), got %d", len(results))
	}
	if n := len(runs["fleetlint/demo/web/"]["results"].([]any)); n != 1 {
		t.Errorf("the web scope's run has its one result, got %d", n)
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
	checkSARIFRules(t, rules)
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
	want := map[string]string{"repo/exc": "vendored on purpose (until 2027-01-01)", "repo/base": "baselined: grandfathered by the baseline file"}
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
		if props, _ := s["properties"].(map[string]any); (id == "repo/exc") != (props["until"] == "2027-01-01" && props["approved_by"] == "@acme/security") {
			t.Errorf("%s: an exception's expiry is a suppression property: %v", id, s)
		}
	}
}

func checkSARIFRules(t *testing.T, rules []any) {
	t.Helper()
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
	if refs, _ := json.Marshal(props["refs"]); string(refs) != `[{"kind":"standard","section":"PW.7","url":"https://example.org/ssdf"}]` {
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
	if id := sr["automationDetails"].(map[string]any)["id"]; id != "fleetlint/my-repo/" {
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

// An anchored finding points at its anchor and keeps the fingerprint of
// the pathless finding; a named artifact shows in the message and makes
// its own alert.
func TestSARIFAnchorsAndArtifacts(t *testing.T) {
	t.Parallel()
	run, root := sarifRun(t)
	run.Results = []model.Result{
		{
			Rule: model.Rule{ID: "docs/readme", Title: "t", Severity: model.SeverityError}, Status: model.StatusFail, Severity: model.SeverityError,
			Findings: []model.Finding{{Message: "no README", Anchor: "README.md"}},
		},
		{
			Rule: model.Rule{ID: "release/signing/image", Title: "t", Severity: model.SeverityError}, Status: model.StatusFail, Severity: model.SeverityError,
			Findings: []model.Finding{{Message: "not signed", Artifact: "ghcr.io/x/a"}, {Message: "not signed", Artifact: "ghcr.io/x/b"}},
		},
	}
	out, _ := renderSARIF(t, run, report.Options{Root: root, Name: "demo"})
	_, results, _ := sarifRuns(t, out)
	readme := results[0].(map[string]any)
	if uri := readme["locations"].([]any)[0].(map[string]any)["physicalLocation"].(map[string]any)["artifactLocation"].(map[string]any)["uri"]; uri != "README.md" {
		t.Errorf("the anchor is the location: %v", uri)
	}
	sum := sha256.Sum256([]byte("docs/readme\x00\x00\x00no README"))
	if fp := readme["partialFingerprints"].(map[string]any)["fleetlint/v1"]; fp != hex.EncodeToString(sum[:]) {
		t.Errorf("the anchor is not part of the fingerprint: %v", fp)
	}
	a, b := results[1].(map[string]any), results[2].(map[string]any)
	if msg := a["message"].(map[string]any)["text"]; msg != "ghcr.io/x/a: not signed" {
		t.Errorf("the artifact's name leads the message: %v", msg)
	}
	if a["partialFingerprints"].(map[string]any)["fleetlint/v1"] == b["partialFingerprints"].(map[string]any)["fleetlint/v1"] {
		t.Error("two artifacts with the same message are two alerts")
	}
}

// A fix is one replacement per file, on the rule's first finding, with
// offsets in UTF-16 code units.
func TestSARIFFixes(t *testing.T) {
	t.Parallel()
	run, root := sarifRun(t)
	run.Results = []model.Result{{
		Rule: model.Rule{ID: "deps/license", Title: "t", Severity: model.SeverityError, Fix: model.Fix{Human: "set the license"}}, Status: model.StatusFail, Severity: model.SeverityError,
		Findings: []model.Finding{{Message: "one"}, {Message: "two"}}, Fixable: true,
		Edits: []model.FileEdit{
			{Path: "package.json", Old: "{\"name\": \"😀\",\n}\n", New: "{\"name\": \"😀\",\n\"license\": \"MIT\",\n}\n"},
			{Path: "LICENSE", New: "MIT\n"},
			{Path: "x", Old: "😀", New: "😃"},
		},
	}}
	out, _ := renderSARIF(t, run, report.Options{Root: root, Name: "demo"})
	_, results, _ := sarifRuns(t, out)
	if _, ok := results[1].(map[string]any)["fixes"]; ok {
		t.Error("only the first finding carries the fix")
	}
	fixes := results[0].(map[string]any)["fixes"].([]any)
	fix := fixes[0].(map[string]any)
	if fix["description"].(map[string]any)["text"] != "set the license" {
		t.Errorf("description is the rule's fix text: %v", fix)
	}
	got, _ := json.Marshal(fix["artifactChanges"])
	want := `[{"artifactLocation":{"uri":"package.json"},"replacements":[{"deletedRegion":{"charLength":0,"charOffset":15},"insertedContent":{"text":"\"license\": \"MIT\",\n"}}]},` +
		`{"artifactLocation":{"uri":"LICENSE"},"replacements":[{"deletedRegion":{"charLength":0,"charOffset":0},"insertedContent":{"text":"MIT\n"}}]},` +
		`{"artifactLocation":{"uri":"x"},"replacements":[{"deletedRegion":{"charLength":2,"charOffset":0},"insertedContent":{"text":"😃"}}]}]`
	if string(got) != want {
		t.Errorf("artifactChanges:\n got %s\nwant %s", got, want)
	}
}
