package repo

import (
	"reflect"
	"testing"
)

func TestNormalizeShapes(t *testing.T) {
	t.Parallel()
	in := map[any]any{"a": float64(3), "b": []any{float64(1.5), map[any]any{1: "x"}}, "c": uint64(7), "d": 2}
	got := normalize(in)
	want := map[string]any{"a": int64(3), "b": []any{1.5, map[string]any{"1": "x"}}, "c": int64(7), "d": int64(2)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize: %#v", got)
	}
}

func TestParseMakefile(t *testing.T) {
	t.Parallel()
	text := `# comment
GO ?= go
FLAGS := -race
VAR ::= x
.PHONY: check lint test

check: lint test ## run everything
	@echo ok
lint:
	golangci-lint run

fmt vet: deps
	$(GO) $@
a-b.c:
	true
`
	mf := parseMakefile(text)
	wantTargets := []string{"a-b.c", "check", "fmt", "lint", "vet"}
	if !reflect.DeepEqual(mf.Targets, wantTargets) {
		t.Fatalf("targets: %v", mf.Targets)
	}
	if !reflect.DeepEqual(mf.Deps["check"], []string{"lint", "test"}) || !reflect.DeepEqual(mf.Deps["fmt"], []string{"deps"}) {
		t.Fatalf("deps: %v", mf.Deps)
	}
	if len(mf.Recipes["lint"]) != 1 || mf.Recipes["lint"][0] != "golangci-lint run" {
		t.Fatalf("recipes: %v", mf.Recipes)
	}
	for _, name := range []string{"GO", "FLAGS", "VAR", ".PHONY"} {
		if _, ok := mf.Deps[name]; ok {
			t.Errorf("%s is not a target", name)
		}
	}
}
