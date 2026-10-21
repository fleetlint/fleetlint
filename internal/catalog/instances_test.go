package catalog_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/artifacts"
	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/celenv"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

// These test the built-in catalog's `instances:` expressions: what each
// artifact kind names, detected from a repository's workflows.

// detect runs the built-in catalog's artifact detection on the files.
func detect(t *testing.T, files map[string]string) []facts.Artifact {
	t.Helper()
	defs, err := catalog.Builtin().ArtifactDefs()
	if err != nil {
		t.Fatal(err)
	}
	r := testutil.Fixture(t, files)
	env, err := celenv.New(context.Background(), r, facts.Discover(r, facts.Overrides{}))
	if err != nil {
		t.Fatal(err)
	}
	found, err := artifacts.Detect(env, defs, nil)
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// tagWorkflow is a workflow run on a tag push with the given steps.
func tagWorkflow(steps string) string {
	return "on:\n  push:\n    tags: ['v*']\njobs:\n  r:\n    runs-on: ubuntu-latest\n    steps:\n" + steps
}

// assertInstances compares the artifacts of a kind as "name <- workflows".
func assertInstances(t *testing.T, found []facts.Artifact, kind string, want ...string) {
	t.Helper()
	var got []string
	for _, a := range found {
		if a.Kind == kind {
			got = append(got, a.Name+" <- "+strings.Join(a.Workflows, ","))
		}
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("%s instances:\n%s\nwant:\n%s", kind, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Build-push tags behind docker/metadata-action are not names; the
// metadata-action's images are. Run-step pushes name the image too.
func TestImageInstancesPreferMetadataImages(t *testing.T) {
	t.Parallel()
	meta := "      - id: meta\n        uses: docker/metadata-action@v5\n        with:\n          images: |\n            ghcr.io/x/app\n            name=docker.io/x/app,enable=true\n" +
		"      - uses: docker/build-push-action@v6\n        with:\n          push: true\n          tags: ${{ steps.meta.outputs.tags }}\n"
	found := detect(t, map[string]string{
		".github/workflows/meta.yml": tagWorkflow(meta),
		".github/workflows/run.yml":  tagWorkflow("      - run: docker push ${{ env.IMAGE }}:${{ github.sha }}\n      - run: crane push out.tar ghcr.io/x/tool:v1\n"),
	})
	assertInstances(t, found, "image",
		"docker.io/x/app <- .github/workflows/meta.yml",
		"ghcr.io/x/app <- .github/workflows/meta.yml",
		"${{ env.IMAGE }} <- .github/workflows/run.yml",
		"ghcr.io/x/tool <- .github/workflows/run.yml",
	)
}

// A GoReleaser configuration with two dockers entries publishes two images.
func TestGoReleaserImageInstances(t *testing.T) {
	t.Parallel()
	found := detect(t, map[string]string{
		"go.mod":                        "module x\n",
		".goreleaser.yaml":              "builds: [{main: .}]\ndockers:\n  - image_templates: [\"ghcr.io/x/cli:{{ .Version }}\", \"ghcr.io/x/cli:latest\"]\n  - image_templates: [\"ghcr.io/x/cli-alpine:{{ .Version }}\"]\n",
		".github/workflows/release.yml": tagWorkflow("      - uses: goreleaser/goreleaser-action@v6\n        with:\n          args: release --clean\n"),
	})
	assertInstances(t, found, "image",
		"ghcr.io/x/cli <- .github/workflows/release.yml",
		"ghcr.io/x/cli-alpine <- .github/workflows/release.yml",
	)
}

// A helm push names the chart it pushes.
func TestHelmChartInstances(t *testing.T) {
	t.Parallel()
	found := detect(t, map[string]string{
		".github/workflows/chart.yml": tagWorkflow("      - run: helm push charts/foo-1.0.0.tgz oci://ghcr.io/x/charts\n"),
	})
	assertInstances(t, found, "helm-chart", "charts/foo-1.0.0.tgz <- .github/workflows/chart.yml")
}

// An oras push names the registry reference, without the tag.
func TestOCIArtifactInstances(t *testing.T) {
	t.Parallel()
	found := detect(t, map[string]string{
		".github/workflows/bundle.yml": tagWorkflow("      - run: oras push ghcr.io/x/bundle:v1 bundle.tar.gz:application/vnd.x.bundle\n"),
	})
	assertInstances(t, found, "oci-artifact", "ghcr.io/x/bundle <- .github/workflows/bundle.yml")
}
