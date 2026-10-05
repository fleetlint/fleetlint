package fix_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/fix"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

func applySet(t *testing.T, file, content, key string, vars map[string]string) (changed bool, result string) {
	t.Helper()
	r := testutil.Fixture(t, map[string]string{file: content})
	actions := []fix.Action{{Set: &fix.SetAction{File: file, Key: key, Value: "{license}"}}}
	changes, err := fix.PlanFor(r, fix.Project{Vars: vars}, actions, fakeTemplates{}, nil)
	if err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	if err := fix.Apply(changes); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(r.Root, file))
	if err != nil {
		t.Fatal(err)
	}
	return len(changes) > 0, string(b)
}

func TestSetInsertsIntoExistingManifests(t *testing.T) {
	t.Parallel()
	mit := map[string]string{"license": "MIT"}
	cases := map[string]struct {
		file, key, in, want string
	}{
		"json after name and version": {
			"package.json", "license",
			"{\n  \"name\": \"x\",\n  \"version\": \"1.0.0\",\n  \"scripts\": {\n    \"name\": \"y\",\n    \"build\": \"vite build\"\n  }\n}\n",
			"{\n  \"name\": \"x\",\n  \"version\": \"1.0.0\",\n  \"license\": \"MIT\",\n  \"scripts\": {\n    \"name\": \"y\",\n    \"build\": \"vite build\"\n  }\n}\n",
		},
		"json without anchors": {"package.json", "license", "{\n\t\"private\": true\n}\n", "{\n\t\"license\": \"MIT\",\n\t\"private\": true\n}\n"},
		"json compact":         {"package.json", "license", `{"name":"x","private":true}`, `{"license": "MIT","name":"x","private":true}`},
		"json empty":           {"package.json", "license", "{}\n", "{\n  \"license\": \"MIT\"\n}\n"},
		"toml after name": {
			"Cargo.toml", "package.license",
			"[package]\nname = \"x\"\nversion = \"0.1.0\"\nedition = \"2021\"\n\n[dependencies]\nname = \"1\"\n",
			"[package]\nname = \"x\"\nversion = \"0.1.0\"\nlicense = \"MIT\"\nedition = \"2021\"\n\n[dependencies]\nname = \"1\"\n",
		},
	}
	for name, tc := range cases {
		changed, got := applySet(t, tc.file, tc.in, tc.key, mit)
		if !changed || got != tc.want {
			t.Errorf("%s: changed=%v\n got: %q\nwant: %q", name, changed, got, tc.want)
		}
	}
}

func TestSetLeavesAloneWhatItCannotEditSafely(t *testing.T) {
	t.Parallel()
	mit := map[string]string{"license": "MIT"}
	cases := map[string]struct {
		file, key, in string
		vars          map[string]string
	}{
		"key already set":        {"package.json", "license", "{\n  \"license\": \"ISC\"\n}\n", mit},
		"json with comments":     {"package.json", "license", "{\n  // name\n  \"name\": \"x\"\n}\n", mit},
		"toml key already set":   {"Cargo.toml", "package.license", "[package]\nname = \"x\"\nlicense = \"ISC\"\n", mit},
		"toml workspace value":   {"Cargo.toml", "package.license", "[package]\nname = \"x\"\nlicense.workspace = true\n", mit},
		"toml table missing":     {"Cargo.toml", "package.license", "[workspace]\nmembers = [\"a\"]\n", mit},
		"license not recognised": {"package.json", "license", "{\n  \"name\": \"x\"\n}\n", nil},
	}
	for name, tc := range cases {
		if changed, got := applySet(t, tc.file, tc.in, tc.key, tc.vars); changed || got != tc.in {
			t.Errorf("%s: must not be edited, got %q", name, got)
		}
	}
	// A manifest that does not exist is not created.
	r := testutil.Fixture(t, map[string]string{"go.mod": "module x\n"})
	changes, err := fix.PlanFor(r, fix.Project{Vars: mit}, []fix.Action{{Set: &fix.SetAction{File: "package.json", Key: "license", Value: "{license}"}}}, fakeTemplates{}, nil)
	if err != nil || len(changes) != 0 {
		t.Errorf("absent file: changes=%v err=%v", changes, err)
	}
}

func TestDetectLicense(t *testing.T) {
	t.Parallel()
	mit, err := os.ReadFile("../../LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	if id := fix.DetectLicense(testutil.Fixture(t, map[string]string{"LICENSE": string(mit)})); id != "MIT" {
		t.Errorf("the repository's own license: got %q", id)
	}
	agpl, err := os.ReadFile("testdata/AGPL-3.0.txt")
	if err != nil {
		t.Fatal(err)
	}
	for name, files := range map[string]map[string]string{
		"gnu license":       {"LICENSE": string(agpl)},
		"no file":           {"README.md": "x"},
		"not a license":     {"LICENSE": "All rights reserved, ask me first.\n"},
		"mostly other text": {"LICENSE": string(mit) + strings.Repeat("Additional terms apply to this software and take precedence.\n", 40)},
	} {
		if id := fix.DetectLicense(testutil.Fixture(t, files)); id != "" {
			t.Errorf("%s: got %q, want none", name, id)
		}
	}
}

func TestDecodeSet(t *testing.T) {
	t.Parallel()
	good := []map[string]any{{"set": map[string]any{"file": "package.json", "key": "license", "value": "{license}"}}}
	if a, err := fix.Decode(good); err != nil || a[0].Set == nil || a[0].Set.Key != "license" {
		t.Fatalf("decode: %+v %v", a, err)
	}
	for name, raw := range map[string]any{
		"not a map":     "package.json",
		"unknown key":   map[string]any{"file": "package.json", "key": "license", "value": "x", "force": true},
		"missing value": map[string]any{"file": "package.json", "key": "license"},
		"other format":  map[string]any{"file": "setup.cfg", "key": "license", "value": "x"},
	} {
		if _, err := fix.Decode([]map[string]any{{"set": raw}}); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
