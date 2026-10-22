package catalog_test

import (
	"slices"
	"testing"
)

// kinds lists the detected artifact kinds as "kind <- workflow".
func kinds(t *testing.T, files map[string]string) []string {
	t.Helper()
	var out []string
	for _, a := range detect(t, files) {
		for _, w := range a.Workflows {
			out = append(out, a.Kind+" <- "+w)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// A publish step counts in any workflow not run from a pull request,
// whatever `if:` guards it: release-please and changesets publish from a
// push to main.
func TestPublishCountsOnEveryNonPullRequestTrigger(t *testing.T) {
	t.Parallel()
	wf := func(on, steps string) string {
		return "on:\n" + on + "jobs:\n  r:\n    runs-on: ubuntu-latest\n    steps:\n" + steps
	}
	got := kinds(t, map[string]string{
		".github/workflows/release-please.yml": wf("  push:\n    branches: [main]\n",
			"      - uses: googleapis/release-please-action@v4\n        id: rp\n      - if: ${{ steps.rp.outputs.release_created }}\n        run: npm publish --provenance\n"),
		".github/workflows/changesets.yml": wf("  push:\n    branches: [main]\n", "      - uses: changesets/action@v1\n        with:\n          publish: pnpm release\n"),
		".github/workflows/tag.yml":        wf("  push:\n    tags: ['v*']\n", "      - run: cargo publish\n"),
		".github/workflows/after-ci.yml":   wf("  workflow_run:\n    workflows: [ci]\n    types: [completed]\n", "      - run: twine upload dist/*\n"),
		".github/workflows/reusable.yml":   wf("  workflow_call: {}\n", "      - run: gem push x.gem\n"),
		".github/workflows/nightly.yml":    wf("  schedule:\n    - cron: '0 3 * * *'\n", "      - if: github.ref == 'refs/heads/main'\n        run: gh release upload nightly dist/app.tar.gz\n"),
		".github/workflows/pr.yml":         wf("  pull_request: {}\n", "      - run: dotnet nuget push x.nupkg\n"),
	})
	want := []string{
		"crate <- .github/workflows/tag.yml",
		"gem <- .github/workflows/reusable.yml",
		"npm <- .github/workflows/changesets.yml",
		"npm <- .github/workflows/release-please.yml",
		"pypi <- .github/workflows/after-ci.yml",
		"release-files <- .github/workflows/nightly.yml",
	}
	if !slices.Equal(got, want) {
		t.Errorf("detected:\n%v\nwant:\n%v", got, want)
	}
}

// Rehearsals in a push workflow publish nothing: dry runs, GoReleaser
// builds and snapshots, an Apple archive built without an upload.
func TestRehearsalsAreNotPublishes(t *testing.T) {
	t.Parallel()
	ci := "on:\n  push:\n    branches: [main]\njobs:\n  r:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - run: npm publish --dry-run\n" +
		"      - run: |\n          cargo publish -n\n          dart pub publish --dry-run\n          uv publish --dry-run\n" +
		"      - uses: goreleaser/goreleaser-action@v6\n        with:\n          args: release --snapshot --clean\n" +
		"      - run: goreleaser build --single-target\n" +
		"      - run: flutter build ipa\n"
	if got := kinds(t, map[string]string{".github/workflows/ci.yml": ci}); len(got) != 0 {
		t.Errorf("rehearsals detected as publishes: %v", got)
	}
	both := "on:\n  push:\n    branches: [main]\njobs:\n  r:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - run: |\n          cargo publish --dry-run\n          cargo publish\n" +
		"      - run: fastlane pilot upload\n"
	want := []string{"apple-app <- .github/workflows/ci.yml", "crate <- .github/workflows/ci.yml"}
	if got := kinds(t, map[string]string{".github/workflows/ci.yml": both}); !slices.Equal(got, want) {
		t.Errorf("a real publish next to a rehearsal: %v, want %v", got, want)
	}
}
