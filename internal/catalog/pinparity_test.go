package catalog_test

import (
	"context"
	"io/fs"
	"reflect"
	"slices"
	"testing"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/catalog/fetch"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

// Pinning the version the binary embeds changes nothing: the pinned tree,
// fetched from a repository holding the whole catalog module, yields the
// same presets, rules, stacks, artifacts, aliases and templates.
func TestPinnedCatalogMatchesBuiltin(t *testing.T) {
	t.Parallel()
	src := testutil.BuiltinCatalogRepo(t, "v9.9.9")
	pin := fetch.PinFetch(context.Background(), t.TempDir())
	loader := catalog.Loader{FetchCatalog: func(_, rev string) (string, string, error) { return pin(src, rev) }}
	pinned, err := loader.Pin("", "v9.9.9")
	if err != nil {
		t.Fatal(err)
	}
	builtin := catalog.Builtin()

	if got, want := pinned.PresetNames(), builtin.PresetNames(); !slices.Equal(got, want) {
		t.Errorf("presets: pinned %v, built-in %v", got, want)
	}
	pinnedLib, err := pinned.Library()
	if err != nil {
		t.Fatal(err)
	}
	builtinLib, err := builtin.Library()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pinnedLib.IDs(), builtinLib.IDs(); !slices.Equal(got, want) {
		t.Errorf("rules: pinned has %d, built-in %d", len(got), len(want))
	}
	for _, id := range builtinLib.IDs() {
		p, _ := pinnedLib.Rule(id)
		b, _ := builtinLib.Rule(id)
		if !reflect.DeepEqual(p, b) {
			t.Errorf("rule %s differs when pinned", id)
		}
	}
	same(t, "stacks", pinned.StackDefs, builtin.StackDefs)
	same(t, "artifacts", pinned.ArtifactDefs, builtin.ArtifactDefs)
	same(t, "aliases", pinned.Aliases, builtin.Aliases)
	if got, want := tree(t, pinned.Templates), tree(t, builtin.Templates); !reflect.DeepEqual(got, want) {
		t.Errorf("templates: pinned has %d files, built-in %d", len(got), len(want))
	}
	for _, name := range builtin.PresetNames() {
		b, err := catalog.Loader{}.Load("fleetlint:" + name)
		if err != nil {
			t.Fatal(err)
		}
		p, err := loader.Load("fleetlint:" + name)
		if err != nil {
			t.Errorf("preset %s does not load pinned: %v", name, err)
			continue
		}
		if got, want := ruleIDs(p), ruleIDs(b); !slices.Equal(got, want) {
			t.Errorf("preset %s: pinned selects %d rules, built-in %d", name, len(got), len(want))
		}
	}
}

func same[T any](t *testing.T, what string, pinned, builtin func() (T, error)) {
	t.Helper()
	p, err := pinned()
	if err != nil {
		t.Fatalf("pinned %s: %v", what, err)
	}
	b, err := builtin()
	if err != nil {
		t.Fatalf("built-in %s: %v", what, err)
	}
	if !reflect.DeepEqual(p, b) {
		t.Errorf("%s differ when pinned: %d entries, built-in %d", what, reflect.ValueOf(p).Len(), reflect.ValueOf(b).Len())
	}
}

// tree reads templates/ of a source.
func tree(t *testing.T, fsys fs.FS) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := fs.WalkDir(fsys, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		files[p] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func ruleIDs(cats []*catalog.Catalog) []string {
	var ids []string
	for _, c := range cats {
		for _, r := range c.Rules {
			ids = append(ids, c.Ref+" "+r.ID)
		}
	}
	return ids
}
