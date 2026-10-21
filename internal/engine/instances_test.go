package engine_test

import (
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/model"
)

// tagWorkflow is a workflow run on a tag push with the given steps.
func tagWorkflow(steps string) string {
	return "on:\n  push:\n    tags: ['v*']\njobs:\n  r:\n    runs-on: ubuntu-latest\n    steps:\n" + steps
}

// instancesOf lists the artifacts of a kind as "name <- workflows".
func instancesOf(found []facts.Artifact, kind string) []string {
	var out []string
	for _, a := range found {
		if a.Kind == kind {
			out = append(out, a.Name+" <- "+strings.Join(a.Workflows, ","))
		}
	}
	return out
}

func assertInstances(t *testing.T, found []facts.Artifact, kind string, want ...string) {
	t.Helper()
	if got := instancesOf(found, kind); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s instances:\n%s\nwant:\n%s", kind, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Two images pushed from two workflows are two artifacts, each checked on
// its own: the finding names the unsigned one, the evidence the signed one.
func TestImageInstancesAreCheckedEach(t *testing.T) {
	t.Parallel()
	build := func(image string) string {
		return "      - id: build\n        uses: docker/build-push-action@v6\n        with:\n          push: true\n          tags: " + image + ":latest\n"
	}
	out := run(t, map[string]string{
		".github/workflows/a.yml": tagWorkflow(build("ghcr.io/x/a") + "      - run: cosign sign --yes ghcr.io/x/a@${{ steps.build.outputs.digest }}\n"),
		".github/workflows/b.yml": tagWorkflow(build("ghcr.io/x/b")),
	}, tier1Public)
	assertInstances(t, out.Facts.Artifacts, "image",
		"ghcr.io/x/a <- .github/workflows/a.yml",
		"ghcr.io/x/b <- .github/workflows/b.yml",
	)
	s := status(t, out, "release/signing/image")
	if s.Status != model.StatusFail || len(s.Findings) != 1 || !strings.HasPrefix(s.Findings[0].Message, "ghcr.io/x/b: ") {
		t.Errorf("the unsigned image fails by name: %s %+v", s.Status, s.Findings)
	}
	if s.Evidence != "ghcr.io/x/a: satisfied by cosign-by-digest" {
		t.Errorf("the signed image is the evidence: %q", s.Evidence)
	}
}

// Build-push tags behind docker/metadata-action are not names; the
// metadata-action's images are. Run-step pushes name the image too.
func TestImageInstancesPreferMetadataImages(t *testing.T) {
	t.Parallel()
	meta := "      - id: meta\n        uses: docker/metadata-action@v5\n        with:\n          images: |\n            ghcr.io/x/app\n            name=docker.io/x/app,enable=true\n" +
		"      - uses: docker/build-push-action@v6\n        with:\n          push: true\n          tags: ${{ steps.meta.outputs.tags }}\n"
	out := run(t, map[string]string{
		".github/workflows/meta.yml": tagWorkflow(meta),
		".github/workflows/run.yml":  tagWorkflow("      - run: docker push ${{ env.IMAGE }}:${{ github.sha }}\n      - run: crane push out.tar ghcr.io/x/tool:v1\n"),
	}, tier1Public)
	assertInstances(t, out.Facts.Artifacts, "image",
		"docker.io/x/app <- .github/workflows/meta.yml",
		"ghcr.io/x/app <- .github/workflows/meta.yml",
		"${{ env.IMAGE }} <- .github/workflows/run.yml",
		"ghcr.io/x/tool <- .github/workflows/run.yml",
	)
}

// A GoReleaser configuration with two dockers entries publishes two images.
func TestGoReleaserImageInstances(t *testing.T) {
	t.Parallel()
	out := run(t, map[string]string{
		"go.mod":                        "module x\n",
		".goreleaser.yaml":              "builds: [{main: .}]\ndockers:\n  - image_templates: [\"ghcr.io/x/cli:{{ .Version }}\", \"ghcr.io/x/cli:latest\"]\n  - image_templates: [\"ghcr.io/x/cli-alpine:{{ .Version }}\"]\n",
		".github/workflows/release.yml": tagWorkflow("      - uses: goreleaser/goreleaser-action@v6\n        with:\n          args: release --clean\n"),
	}, tier1Public)
	assertInstances(t, out.Facts.Artifacts, "image",
		"ghcr.io/x/cli <- .github/workflows/release.yml",
		"ghcr.io/x/cli-alpine <- .github/workflows/release.yml",
	)
}

// A helm push names the chart it pushes.
func TestHelmChartInstances(t *testing.T) {
	t.Parallel()
	out := run(t, map[string]string{
		".github/workflows/chart.yml": tagWorkflow("      - run: helm push charts/foo-1.0.0.tgz oci://ghcr.io/x/charts\n"),
	}, tier1Public)
	assertInstances(t, out.Facts.Artifacts, "helm-chart", "charts/foo-1.0.0.tgz <- .github/workflows/chart.yml")
}

// An oras push names the registry reference, without the tag.
func TestOCIArtifactInstances(t *testing.T) {
	t.Parallel()
	out := run(t, map[string]string{
		".github/workflows/bundle.yml": tagWorkflow("      - run: oras push ghcr.io/x/bundle:v1 bundle.tar.gz:application/vnd.x.bundle\n"),
	}, tier1Public)
	assertInstances(t, out.Facts.Artifacts, "oci-artifact", "ghcr.io/x/bundle <- .github/workflows/bundle.yml")
}

// A pinned instance is checked under its pinned name and workflows.
func TestPinnedInstanceIsChecked(t *testing.T) {
	t.Parallel()
	cfg := "version: 1\nextends: [fleetlint:recommended]\nfacts: {tier: 1, visibility: public, artifacts: [{kind: image, name: ghcr.io/x/p, workflows: [.github/workflows/p.yml]}]}\n"
	out := run(t, map[string]string{
		".github/workflows/p.yml": tagWorkflow("      - run: make push\n      - run: cosign sign --yes ghcr.io/x/p@${{ steps.build.outputs.digest }}\n"),
	}, cfg)
	assertInstances(t, out.Facts.Artifacts, "image", "ghcr.io/x/p <- .github/workflows/p.yml")
	if s := status(t, out, "release/signing/image"); s.Status != model.StatusPass || s.Evidence != "ghcr.io/x/p: satisfied by cosign-by-digest" {
		t.Errorf("the pinned image passes by name: %s %q %+v", s.Status, s.Evidence, s.Findings)
	}
}

// The severity of a result over several artifacts: a partial's when every
// failing artifact matched a partial, the rule's when one failed outright.
func TestPartialSeverityAcrossArtifacts(t *testing.T) {
	t.Parallel()
	push := func(name, sign string) string {
		return "on:\n  push:\n    tags: ['v*']\njobs:\n  r:\n    runs-on: ubuntu-latest\n    steps:\n      - run: docker push ghcr.io/x/" + name + ":${GITHUB_REF_NAME}\n" + sign
	}
	byTag := func(name string) string {
		return "      - run: cosign sign --yes ghcr.io/x/" + name + ":${GITHUB_REF_NAME}\n"
	}
	partials := run(t, map[string]string{".github/workflows/a.yml": push("a", byTag("a")), ".github/workflows/b.yml": push("b", byTag("b"))}, tier1Public)
	if s := status(t, partials, "release/signing/image"); s.Status != model.StatusFail || s.Severity != model.SeverityWarning || len(s.Findings) != 2 || !strings.HasPrefix(s.Findings[0].Message, "ghcr.io/x/a: ") || !strings.HasPrefix(s.Findings[1].Message, "ghcr.io/x/b: ") {
		t.Errorf("partials only: the partial's severity, findings named: %s %s %+v", s.Status, s.Severity, s.Findings)
	}
	outright := run(t, map[string]string{".github/workflows/a.yml": push("a", ""), ".github/workflows/b.yml": push("b", "")}, tier1Public)
	if s := status(t, outright, "release/signing/image"); s.Status != model.StatusFail || s.Severity != model.SeverityError || len(s.Findings) != 2 || s.Findings[1].Evidence != "unsatisfied" {
		t.Errorf("outright failures: the rule's severity: %s %s %+v", s.Status, s.Severity, s.Findings)
	}
}

// A locate expression on an implementation keyed by artifact sees the
// artifact: the finding points at the workflow that publishes it.
func TestLocateSeesTheArtifact(t *testing.T) {
	t.Parallel()
	image := "on:\n  push:\n    tags: ['v*']\njobs:\n  r:\n    runs-on: ubuntu-latest\n    steps:\n      - run: docker push ghcr.io/x/y:${GITHUB_REF_NAME}\n"
	out := run(t, map[string]string{".github/workflows/publish.yml": image}, tier1Public)
	if s := status(t, out, "release/signing/image"); s.Status != model.StatusFail || len(s.Findings) != 1 || s.Findings[0].Path != ".github/workflows/publish.yml" {
		t.Errorf("the finding points at the publishing workflow: %s %+v", s.Status, s.Findings)
	}
}
