package facts_test

import (
	"testing"

	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

func TestOwnerFromTheRepositoryFiles(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		files       map[string]string
		owner, from string
	}{
		{"backstage wins", map[string]string{
			"catalog-info.yaml":  "apiVersion: backstage.io/v1alpha1\nkind: Location\nspec: {targets: [x]}\n---\nkind: Component\nspec:\n  owner: group:default/payments\n",
			".github/CODEOWNERS": "* @acme/platform\n",
		}, "payments", "catalog-info.yaml"},
		{"another namespace stays", map[string]string{"catalog-info.yaml": "kind: Component\nspec: {owner: \"group:ops/payments\"}\n"}, "ops/payments", "catalog-info.yaml"},
		{"last catch-all line, first owner", map[string]string{
			".github/CODEOWNERS": "# owners\n* @acme/old\n/docs/ @acme/docs\n*   @acme/platform @alice # default\n",
		}, "acme/platform", ".github/CODEOWNERS"},
		{"gitlab sections are skipped", map[string]string{
			".gitlab/CODEOWNERS": "[Docs] @acme/docs\n*.md @acme/writers\n/** @acme/core\n",
		}, "acme/core", ".gitlab/CODEOWNERS"},
		{"no catch-all, no owner", map[string]string{"CODEOWNERS": "/api/ @acme/api\n"}, "", ""},
		{"nothing", map[string]string{"go.mod": "module x\n"}, "", ""},
	}
	for _, c := range cases {
		f := facts.Discover(testutil.Fixture(t, c.files), facts.Overrides{})
		if f.Owner != c.owner || f.OwnerFrom != c.from {
			t.Errorf("%s: owner %q from %q, want %q from %q", c.name, f.Owner, f.OwnerFrom, c.owner, c.from)
		}
	}
}
