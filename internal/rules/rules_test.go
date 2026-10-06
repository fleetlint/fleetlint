package rules_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/rules"
	_ "github.com/fleetlint/fleetlint/internal/rules/builtin"
)

type fake struct{ id string }

func (f fake) ID() string                               { return f.id }
func (fake) Check(rules.Context) (rules.Outcome, error) { return rules.Outcome{}, nil }

func TestRegistryListsSortedIDs(t *testing.T) {
	t.Parallel()
	rules.Register(fake{"zzz/test-only"})
	rules.Register(fake{"aaa/test-only"})
	ids := rules.IDs()
	if !slices.IsSorted(ids) {
		t.Fatalf("IDs must be sorted: %v", ids)
	}
	for _, want := range []string{"aaa/test-only", "quality/check-passes", "zzz/test-only"} {
		if !slices.Contains(ids, want) {
			t.Errorf("IDs() lacks %s: %v", want, ids)
		}
	}
	if c, ok := rules.Lookup("aaa/test-only"); !ok || c.ID() != "aaa/test-only" {
		t.Errorf("Lookup after Register: %v %v", c, ok)
	}
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "duplicate registration") {
			t.Errorf("duplicate id must panic, got %v", r)
		}
	}()
	rules.Register(fake{"aaa/test-only"})
}

func TestStringList(t *testing.T) {
	t.Parallel()
	def := []string{"d"}
	cases := []struct {
		name   string
		params map[string]any
		want   []string
	}{
		{"missing key", map[string]any{}, def},
		{"not a list", map[string]any{"k": "x"}, def},
		{"strings only", map[string]any{"k": []any{"a", 1, "b", nil}}, []string{"a", "b"}},
		{"empty list", map[string]any{"k": []any{}}, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := rules.StringList(c.params, "k", def); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}
