package catalog_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/model"
)

// Every rule says where its practice comes from: at least one ref with a
// kind (a convention of this catalog counts, it says so). A rule with bare
// URLs only may stay while it is listed in testdata/rules_without_source.txt;
// the list only shrinks: a rule that gains a typed source must leave it
// (UPDATE_GOLDEN=1 rewrites the file), one that loses it may not join.
func TestEveryRuleNamesItsSource(t *testing.T) {
	t.Parallel()
	lib, err := catalog.Builtin().Library()
	if err != nil {
		t.Fatal(err)
	}
	const golden = "testdata/rules_without_source.txt"
	raw, err := os.ReadFile(golden)
	if err != nil && os.Getenv("UPDATE_GOLDEN") == "" {
		t.Fatal(err)
	}
	allowed := strings.Fields(string(raw))
	var without []string
	for _, id := range lib.IDs() {
		r, _ := lib.Rule(id)
		if len(r.Refs) == 0 {
			t.Errorf("%s cites nothing", id)
			continue
		}
		for _, ref := range r.Refs {
			if err := ref.Validate(); err != nil {
				t.Errorf("%s: %v", id, err)
			}
		}
		if !slices.ContainsFunc(r.Refs, model.Ref.Typed) {
			without = append(without, id)
		}
	}
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, []byte(strings.Join(without, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, id := range without {
		if !slices.Contains(allowed, id) {
			t.Errorf("%s has no typed source and is not in %s: say which spec, guidance, style guide, standard or check it comes from, or that it is a convention", id, golden)
		}
	}
	for _, id := range allowed {
		if !slices.Contains(without, id) {
			t.Errorf("%s has a typed source now (or no longer exists): remove it from %s", id, golden)
		}
	}
	t.Logf("%d of %d rules have no typed source yet", len(without), len(lib.IDs()))
}
