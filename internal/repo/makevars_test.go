package repo_test

import (
	"testing"

	"github.com/fleetlint/fleetlint/internal/repo"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

func TestMakefileRecipesExpandVariables(t *testing.T) {
	t.Parallel()
	r := testutil.Fixture(t, map[string]string{"Makefile": "V ?= v2\nV ?= v9\nTOOL := go run example.com/go-licenses@$(V)\n\nlicenses:\n\t$(TOOL) check ./... ${UNSET}\n"})
	mf, ok := r.Makefile("Makefile")
	if !ok {
		t.Fatal("no Makefile")
	}
	if got := mf.Recipes["licenses"][0]; got != "go run example.com/go-licenses@v2 check ./... ${UNSET}" {
		t.Fatalf("simple assignments expand, the first ?= wins, unknown names stay: %q", got)
	}
}

func TestDocumentsWithAByteOrderMark(t *testing.T) {
	t.Parallel()
	r := testutil.Fixture(t, map[string]string{"package.json": "\ufeff{\"name\": \"a\"}", "x.yaml": "\ufeffk: v\n"})
	for _, f := range []struct {
		format repo.Format
		path   string
	}{{repo.FormatJSON, "package.json"}, {repo.FormatYAML, "x.yaml"}} {
		if _, ok, err := r.Doc(f.format, f.path); !ok || err != nil {
			t.Errorf("%s with a BOM: %v %v", f.path, ok, err)
		}
	}
}
