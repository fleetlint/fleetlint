package engine_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/catalog/fetch"
	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

// A repository that pins a catalog version runs with that version's stacks,
// artifacts and renamed ids: the whole catalog is fetched with git, cached
// and checked against, here the built-in catalog module served as a tag.
func TestPinnedCatalogVersionRunsEndToEnd(t *testing.T) {
	t.Parallel()
	src := testutil.BuiltinCatalogRepo(t, "v9.9.9")
	pin := fetch.PinFetch(context.Background(), t.TempDir())
	loader := catalog.Loader{FetchCatalog: func(_, rev string) (string, string, error) { return pin(src, rev) }}

	r := testutil.GitFixture(t, map[string]string{
		"go.mod": "module x\n",
		config.FileName: "version: 1\ncatalog: {version: v9.9.9}\nextends: [fleetlint:recommended]\nfacts: {tier: 1, artifacts: [image]}\n" +
			"overrides:\n  lint/go-config: {severity: info, reason: r}\n",
	})
	eff, err := config.Load(r.Root, config.Options{Loader: loader})
	if err != nil {
		t.Fatal(err)
	}
	if !eff.Source.Pinned || !strings.HasPrefix(eff.Source.Version, "v9.9.9 (") {
		t.Errorf("the pinned version runs: %s", eff.Source.Describe())
	}
	if !slices.ContainsFunc(eff.Deprecations, func(d string) bool { return strings.Contains(d, "lint/go-config was renamed") }) {
		t.Errorf("the pinned aliases.yaml renames ids: %v", eff.Deprecations)
	}
	run, err := engine.Evaluate(context.Background(), r, eff)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(run.Facts.Stacks, "go") {
		t.Errorf("the pinned stacks/ detect go: %v", run.Facts.Stacks)
	}
	if res := status(t, run, "release/signing/image"); res.Status != model.StatusFail {
		t.Errorf("the pinned artifacts/ define the image, whose signing rule runs: %s %s", res.Status, res.Evidence)
	}
}
