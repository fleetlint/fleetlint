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
	if s.Status != model.StatusFail || len(s.Findings) != 1 || s.Findings[0].Artifact != "ghcr.io/x/b" || !strings.HasPrefix(s.Findings[0].Text(), "ghcr.io/x/b: ") {
		t.Errorf("the unsigned image fails by name: %s %+v", s.Status, s.Findings)
	}
	if s.Evidence != "ghcr.io/x/a: satisfied by cosign-by-digest" {
		t.Errorf("the signed image is the evidence: %q", s.Evidence)
	}
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
	if s := status(t, partials, "release/signing/image"); s.Status != model.StatusFail || s.Severity != model.SeverityWarning || len(s.Findings) != 2 || s.Findings[0].Artifact != "ghcr.io/x/a" || s.Findings[1].Artifact != "ghcr.io/x/b" {
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
	if s := status(t, out, "release/signing/image"); s.Status != model.StatusFail || len(s.Findings) != 1 || s.Findings[0].Path != "" || s.Findings[0].Anchor != ".github/workflows/publish.yml" {
		t.Errorf("the finding points at the publishing workflow: %s %+v", s.Status, s.Findings)
	}
}
