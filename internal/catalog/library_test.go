package catalog_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/fleetlint/fleetlint/internal/catalog"
)

// The presets select from the library; what they resolve to is what the
// inline presets defined before the split (recorded when the library was
// extracted), and a rule shared with an include is kept once, from the
// include.
func TestPresetsSelectFromTheLibrary(t *testing.T) {
	t.Parallel()
	want := map[string][]string{}
	b, err := os.ReadFile("testdata/preset_rules.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	for preset, ids := range want {
		cats, err := catalog.Loader{}.Load("fleetlint:" + preset)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, c := range cats {
			for _, r := range c.Rules {
				if prev, dup := got[r.ID]; dup {
					t.Errorf("%s: %s defined by %s and %s", preset, r.ID, prev, c.Ref)
				}
				got[r.ID] = c.Ref
			}
		}
		for _, id := range ids {
			if _, ok := got[id]; !ok {
				t.Errorf("%s: %s missing", preset, id)
			}
		}
		own := cats[len(cats)-1]
		if preset == "recommended" {
			if got["repo/no-tracked-junk"] != "fleetlint:minimal" || own.Metadata.Name != "fleetlint-recommended" {
				t.Errorf("a rule of an include stays the include's: %s", got["repo/no-tracked-junk"])
			}
			if len(got) != len(ids)+7 {
				t.Errorf("recommended with minimal resolves to %d rules, want %d", len(got), len(ids)+7)
			}
		}
	}
	lib, err := catalog.Builtin().Library()
	if err != nil || len(lib.IDs()) < 100 {
		t.Fatalf("library: %d rules, %v", len(lib.IDs()), err)
	}
}

func TestUseEntries(t *testing.T) {
	t.Parallel()
	const head = "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: t, version: 1.0.0, engine: 2}\n"
	dir := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		p := dir + "/" + name
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	ok := write("ok.yaml", head+"rules:\n  - use: lint/go-*\n  - id: own/rule\n    title: own\n    kind: expr\n    severity: info\n    expr: \"true\"\n    message: m\n    requirement: r\n    rationale: r\n    fix: {human: h, agent: a}\n  - use: repo/readme-present\n")
	cats, err := catalog.Loader{}.Load(ok)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(cats[0].Rules))
	for _, r := range cats[0].Rules {
		ids = append(ids, r.ID)
	}
	joined := strings.Join(ids, " ")
	if !strings.HasPrefix(joined, "lint/go-config lint/go-linters-enabled own/rule repo/readme-present") {
		t.Errorf("order and expansion: %s", joined)
	}
	if r := cats[0].Rules[0]; r.SelectedBy != "t@1.0.0" || !strings.HasPrefix(r.Source, "library ") {
		t.Errorf("a selected rule names the library as source and the catalog as selector: %+v", r)
	}
	for name, body := range map[string]string{
		"unknown id":       head + "rules:\n  - use: lint/nothing\n",
		"empty glob":       head + "rules:\n  - use: nothing/*\n",
		"use with keys":    head + "rules:\n  - use: lint/go-config\n    severity: error\n",
		"use and id":       head + "rules:\n  - use: lint/go-config\n    id: x\n",
		"library required": strings.Replace(head, "engine: 2", "engine: 2", 1) + "rules:\n  - use: lint/*\n",
	} {
		p := write(strings.ReplaceAll(name, " ", "_")+".yaml", body)
		l := catalog.Loader{}
		if name == "library required" {
			l.Source = &catalog.Source{Presets: fstest.MapFS{}, Templates: fstest.MapFS{}, Version: "empty"}
		}
		if _, err := l.Load(p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// A library file's path spells its id; a rule under the wrong path is refused.
func TestLibraryPathSpellsID(t *testing.T) {
	t.Parallel()
	rule := "id: lint/other\ntitle: t\nkind: expr\nseverity: info\nexpr: \"true\"\nmessage: m\nrequirement: r\nrationale: r\nfix: {human: h, agent: a}\n"
	src := &catalog.Source{Presets: fstest.MapFS{}, Templates: fstest.MapFS{}, Rules: fstest.MapFS{"rules/lint/go-config.yaml": {Data: []byte(rule)}}, Version: "t"}
	if _, err := src.Library(); err == nil || !strings.Contains(err.Error(), "must be") {
		t.Errorf("mismatched path accepted: %v", err)
	}
	good := &catalog.Source{Presets: fstest.MapFS{}, Templates: fstest.MapFS{}, Rules: fstest.MapFS{"rules/lint/other.yaml": {Data: []byte(rule)}}, Version: "t2"}
	lib, err := good.Library()
	if err != nil || len(lib.IDs()) != 1 {
		t.Errorf("one rule: %v %v", lib, err)
	}
	none := &catalog.Source{Presets: fstest.MapFS{}, Templates: fstest.MapFS{}, Version: "t3"}
	if lib, err := none.Library(); err != nil || len(lib.IDs()) != 0 {
		t.Errorf("no rules dir, empty library: %v", err)
	}
}
