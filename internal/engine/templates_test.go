package engine_test

import (
	"testing"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/model"
)

// The templates `fleetlint fix` writes must satisfy the rules that asked for
// them: a repository set up from the templates alone passes the hook, CI and
// release rules for its stack. Whether the workflows run on a real runner is
// a separate question (docs/ROADMAP.md). taskrunner/targets is left out on
// purpose: the Makefile template's placeholders must fail until filled in.
func TestTemplatesSatisfyTheRules(t *testing.T) {
	t.Parallel()
	manifests := map[string]map[string]string{
		"go":      {"go.mod": "module x\n\ngo 1.24.0\n", ".goreleaser.yaml": "builds: [{main: .}]\nsboms: [{artifacts: archive}]\nsigns: [{cmd: cosign}]\n"},
		"rust":    {"Cargo.toml": "[package]\nname = \"x\"\n", ".goreleaser.yaml": "builds: [{builder: rust}]\nsboms: [{artifacts: archive}]\nsigns: [{cmd: cosign}]\n"},
		"python":  {"pyproject.toml": "[project]\nname = \"x\"\n"},
		"node":    {"package.json": "{}"},
		"kotlin":  {"build.gradle.kts": "plugins {}\n"},
		"flutter": {"pubspec.yaml": "name: x\n"},
	}
	rules := []string{
		"hooks/config-present", "hooks/shared-hygiene", "hooks/secret-scan", "hooks/conventional-commits", "hooks/pre-push-check",
		"ci/check-workflow", "ci/least-privilege", "ci/job-timeouts", "ci/concurrency-cancel", "ci/actions-pinned",
		"release/tag-triggered", "release/sbom", "release/signing", "release/provenance", "release/checksums",
		"changelog/generator-configured", "quality/policy-enforced",
	}
	for stack, files := range manifests {
		t.Run(stack, func(t *testing.T) {
			t.Parallel()
			for name, dest := range map[string]string{
				stack + "/pre-commit-config.yaml": ".pre-commit-config.yaml",
				stack + "/check.yml":              ".github/workflows/check.yml",
				stack + "/release.yml":            ".github/workflows/release.yml",
				"Makefile":                        "Makefile",
				"cliff.toml":                      "cliff.toml",
			} {
				b, err := catalog.EmbeddedTemplates{}.Template(name)
				if err != nil {
					t.Fatal(err)
				}
				files[dest] = string(b)
			}
			out := run(t, files, tier1Public)
			for _, id := range rules {
				if s := status(t, out, id); s.Status != model.StatusPass {
					t.Errorf("%s: %s %q %+v", id, s.Status, s.Err, s.Findings)
				}
			}
		})
	}
}
