package celenv_test

import (
	"context"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/celenv"
	"github.com/fleetlint/fleetlint/internal/facts"
)

// The documented extension functions are the ones expressions can call.
func TestExtensionFunctionsAreEnabled(t *testing.T) {
	t.Parallel()
	got := map[string]celenv.Function{}
	for _, x := range celenv.Extensions {
		fns, err := x.Functions()
		if err != nil || len(fns) == 0 {
			t.Fatalf("%s: %v %v", x.Name, fns, err)
		}
		for _, f := range fns {
			got[f.Name] = f
		}
	}
	for name, member := range map[string]bool{"split": true, "flatten": true, "sortBy": true, "strings.quote": false, "lists.range": false} {
		if f, ok := got[name]; !ok || f.Member != member {
			t.Errorf("%s: got %+v", name, f)
		}
	}
	for _, std := range []string{"contains", "size", "matches", "filter"} {
		if _, ok := got[std]; ok {
			t.Errorf("%s is standard CEL, not an extension", std)
		}
	}
	env, err := celenv.New(context.Background(), nil, facts.Facts{})
	if err != nil {
		t.Fatal(err)
	}
	for _, expr := range []string{`"a,b".split(",").size() == 2`, `[[1], [2]].flatten().size() == 2`, `strings.quote("x") != ""`} {
		if err := env.TypeCheck(expr); err != nil {
			t.Errorf("%s: %v", expr, err)
		}
	}
	if !strings.HasPrefix(celenv.ExtensionDocs(), "https://pkg.go.dev/cel.dev/cel-go") {
		t.Errorf("docs link %s", celenv.ExtensionDocs())
	}
}
