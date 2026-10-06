package fix_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/fix"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

func planAndApply(t *testing.T, files map[string]string, actions []fix.Action, tpl fakeTemplates) (map[string]string, []fix.Change, error) {
	t.Helper()
	r := testutil.Fixture(t, files)
	changes, err := fix.PlanFor(r, fix.Project{Stack: "python"}, actions, tpl, nil)
	if err != nil {
		return nil, nil, err
	}
	if err := fix.Apply(changes); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for name := range files {
		b, err := os.ReadFile(filepath.Join(r.Root, name))
		if err != nil {
			t.Fatal(err)
		}
		out[name] = string(b)
	}
	return out, changes, nil
}

// append adds a template block to a TOML or YAML file once, never to a file
// that already has the thing, never to a missing file, and never when the
// result would not parse.
func TestAppendBlock(t *testing.T) {
	t.Parallel()
	ruff := "[tool.ruff]\nline-length = 100\n\n[tool.ruff.lint]\nselect = [\"E\", \"F\"]\n"
	tpl := fakeTemplates{"python/ruff.toml": ruff, "broken.toml": "[tool.ruff\n"}
	appendRuff := []fix.Action{{Append: &fix.AppendAction{File: "pyproject.toml", Template: "{stack}/ruff.toml", Unless: `(?m)^\[tool\.ruff\]`}}}

	got, changes, err := planAndApply(t, map[string]string{"pyproject.toml": "[project]\nname = \"x\""}, appendRuff, tpl)
	if err != nil || len(changes) != 1 || changes[0].Kind != "append" {
		t.Fatalf("changes=%+v err=%v", changes, err)
	}
	if want := "[project]\nname = \"x\"\n\n" + strings.TrimLeft(ruff, "\n"); got["pyproject.toml"] != want {
		t.Errorf("got %q\nwant %q", got["pyproject.toml"], want)
	}
	if !strings.Contains(changes[0].Diff, "+ [tool.ruff]") {
		t.Errorf("diff shows the block: %q", changes[0].Diff)
	}

	_, changes, _ = planAndApply(t, map[string]string{"pyproject.toml": "[project]\nname = \"x\"\n\n[tool.ruff]\nline-length = 88\n"}, appendRuff, tpl)
	if len(changes) != 0 {
		t.Error("a file that has the table is left alone")
	}
	_, changes, _ = planAndApply(t, map[string]string{"README.md": "x"}, appendRuff, tpl)
	if len(changes) != 0 {
		t.Error("a missing file is not created by append")
	}
	bad := []fix.Action{{Append: &fix.AppendAction{File: "pyproject.toml", Template: "broken.toml", Unless: "never-matches"}}}
	if _, _, err := planAndApply(t, map[string]string{"pyproject.toml": "[project]\n"}, bad, tpl); err == nil || !strings.Contains(err.Error(), "would not parse") {
		t.Errorf("an unparsable result is refused: %v", err)
	}

	// Two appends to one file in a single plan both land, once each.
	two := []fix.Action{
		{Append: &fix.AppendAction{File: "pyproject.toml", Template: "{stack}/ruff.toml", Unless: `(?m)^\[tool\.ruff\]`}},
		{Append: &fix.AppendAction{File: "pyproject.toml", Template: "dev.toml", Unless: `(?m)^\[dependency-groups\]`}},
	}
	tpl["dev.toml"] = "\n[dependency-groups]\ndev = [\"ruff\"]\n"
	got, changes, err = planAndApply(t, map[string]string{"pyproject.toml": "[project]\nname = \"x\"\n"}, two, tpl)
	if err != nil || len(changes) != 2 || strings.Count(got["pyproject.toml"], "[tool.ruff]") != 1 || strings.Count(got["pyproject.toml"], "[dependency-groups]") != 1 {
		t.Errorf("both blocks once: %v\n%s", err, got["pyproject.toml"])
	}

	yml := []fix.Action{{Append: &fix.AppendAction{File: "analysis_options.yaml", Template: "strict.yaml", Unless: "strict-casts"}}}
	got, _, err = planAndApply(t, map[string]string{"analysis_options.yaml": "include: package:lints/recommended.yaml\n"}, yml, fakeTemplates{"strict.yaml": "analyzer:\n  language:\n    strict-casts: true\n"})
	if err != nil || !strings.HasSuffix(got["analysis_options.yaml"], "strict-casts: true\n") {
		t.Errorf("yaml append: %q %v", got["analysis_options.yaml"], err)
	}
}

// merge adds the keys a JSON file lacks, keeps the ones it has, keeps the
// indentation, and refuses JSON with comments instead of mangling it.
func TestMergeJSON(t *testing.T) {
	t.Parallel()
	tpl := fakeTemplates{"node/tsconfig-strict.json": `{"compilerOptions": {"strict": true, "noUncheckedIndexedAccess": true, "exactOptionalPropertyTypes": true}}`}
	merge := []fix.Action{{Merge: &fix.MergeAction{File: "tsconfig.json", Template: "node/tsconfig-strict.json"}}}

	in := "{\n    \"compilerOptions\": {\n        \"target\": \"es2022\",\n        \"strict\": false\n    },\n    \"include\": [\"src\"]\n}\n"
	got, changes, err := planAndApply(t, map[string]string{"tsconfig.json": in}, merge, tpl)
	if err != nil || len(changes) != 1 {
		t.Fatalf("changes=%+v err=%v", changes, err)
	}
	out := got["tsconfig.json"]
	for _, want := range []string{`"strict": false`, `"noUncheckedIndexedAccess": true`, `"exactOptionalPropertyTypes": true`, `"target": "es2022"`, "\n    \"compilerOptions\""} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// Member order is the file's: compilerOptions before include, target before strict, new keys after the existing ones.
	if strings.Index(out, `"compilerOptions"`) > strings.Index(out, `"include"`) || strings.Index(out, `"target"`) > strings.Index(out, `"strict"`) || strings.Index(out, `"strict"`) > strings.Index(out, `"noUncheckedIndexedAccess"`) {
		t.Errorf("merge reordered the document:\n%s", out)
	}
	numbers := "{\n  \"version\": 1.10,\n  \"big\": 12345678901234567890,\n  \"compilerOptions\": {}\n}\n"
	got, _, err = planAndApply(t, map[string]string{"tsconfig.json": numbers}, merge, tpl)
	if err != nil || !strings.Contains(got["tsconfig.json"], "1.10") || !strings.Contains(got["tsconfig.json"], "12345678901234567890") {
		t.Errorf("numbers are written back as they were: %v\n%s", err, got["tsconfig.json"])
	}
	if !strings.Contains(changes[0].Diff, "compilerOptions.noUncheckedIndexedAccess: true") || strings.Contains(changes[0].Diff, "strict") {
		t.Errorf("the diff lists the added keys only: %q", changes[0].Diff)
	}
	complete := `{"compilerOptions": {"strict": true, "noUncheckedIndexedAccess": true, "exactOptionalPropertyTypes": true}}`
	if _, changes, _ := planAndApply(t, map[string]string{"tsconfig.json": complete}, merge, tpl); len(changes) != 0 {
		t.Error("nothing to add, nothing changes")
	}
	jsonc := "{\n  // comments are allowed by tsc, not by fix\n  \"compilerOptions\": {}\n}\n"
	if _, _, err := planAndApply(t, map[string]string{"tsconfig.json": jsonc}, merge, tpl); err == nil || !strings.Contains(err.Error(), "not strict JSON") {
		t.Errorf("JSON with comments is reported: %v", err)
	}
}

func TestBlockActionsDecode(t *testing.T) {
	t.Parallel()
	good := []map[string]any{
		{"append": map[string]any{"file": "pyproject.toml", "template": "python/ruff.toml", "unless": `\[tool\.ruff\]`}},
		{"merge": map[string]any{"file": "tsconfig.json", "template": "node/tsconfig-strict.json"}},
	}
	actions, err := fix.Decode(good)
	if err != nil || actions[0].Append == nil || actions[1].Merge == nil {
		t.Fatalf("decode: %+v %v", actions, err)
	}
	for _, bad := range []map[string]any{
		{"append": map[string]any{"file": "a.toml", "template": "t"}},
		{"append": map[string]any{"file": "a.toml", "template": "t", "unless": "("}},
		{"append": map[string]any{"file": "a.toml", "template": "t", "unless": "x", "to": "b"}},
		{"merge": map[string]any{"file": "a.json"}},
		{"merge": "a.json"},
	} {
		if _, err := fix.Decode([]map[string]any{bad}); err == nil {
			t.Errorf("%v: accepted", bad)
		}
	}
}
