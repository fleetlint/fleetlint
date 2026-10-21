package catalog_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/model"
)

// Every rule whose findings can name no path says where they belong
// (`locate`), so a report points at the file. A rule without it may stay
// while it is listed in testdata/rules_without_locate.txt; the list only
// shrinks (UPDATE_GOLDEN=1 rewrites it). Capabilities and foreach rules
// are left out: the former have no findings, the latter locate each item.
func TestEveryRuleLocatesItsFindings(t *testing.T) {
	t.Parallel()
	lib, err := catalog.Builtin().Library()
	if err != nil {
		t.Fatal(err)
	}
	const golden = "testdata/rules_without_locate.txt"
	raw, err := os.ReadFile(golden)
	if err != nil && os.Getenv("UPDATE_GOLDEN") == "" {
		t.Fatal(err)
	}
	allowed := strings.Fields(string(raw))
	var without []string
	for _, id := range lib.IDs() {
		r, _ := lib.Rule(id)
		if r.Kind == model.KindCapability || r.Foreach != "" || r.When == "false" {
			continue
		}
		if r.Locate == "" {
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
			t.Errorf("%s has no locate and is not in %s: say which file its findings belong to", id, golden)
		}
	}
	for _, id := range allowed {
		if !slices.Contains(without, id) {
			t.Errorf("%s locates its findings now (or no longer exists): remove it from %s", id, golden)
		}
	}
}
