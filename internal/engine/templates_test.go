package engine_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/fix"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/repo"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

// The templates `fleetlint fix` writes must satisfy the rules that asked for
// them: a repository set up from the templates alone passes the hook, CI and
// release rules for its stack. Whether the workflows run on a real runner is
// a separate question (docs/ROADMAP.md). taskrunner/targets is left out on
// purpose: the Makefile template's placeholders must fail until filled in.
func TestTemplatesSatisfyTheRules(t *testing.T) {
	t.Parallel()
	manifests := map[string]map[string]string{
		"go":      {"go.mod": "module x\n\ngo 1.24.0\n", "renovate.json": "{}", ".goreleaser.yaml": "builds: [{main: .}]\nsboms: [{artifacts: archive}]\nsigns: [{cmd: cosign}]\n"},
		"rust":    {"Cargo.toml": "[package]\nname = \"x\"\n", ".goreleaser.yaml": "builds: [{builder: rust}]\nsboms: [{artifacts: archive}]\nsigns: [{cmd: cosign}]\n"},
		"python":  {"pyproject.toml": "[project]\nname = \"x\"\n"},
		"node":    {"package.json": "{}"},
		"kotlin":  {"build.gradle.kts": "plugins {}\n"},
		"flutter": {"pubspec.yaml": "name: x\n"},
		"hugo":    {"hugo.toml": "baseURL = 'https://example.org/'\n"},
	}
	rules := []string{
		"hooks/config-present", "hooks/shared-hygiene", "hooks/secret-scan", "hooks/conventional-commits", "hooks/pre-push-check",
		"ci/check-workflow", "ci/least-privilege", "ci/job-timeouts", "ci/concurrency-cancel", "ci/actions-pinned",
		"release/tag-triggered", "release/sbom", "release/signing", "release/provenance", "release/checksums",
		"changelog/generator-configured", "quality/policy-enforced", "deps/update-automation",
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
				stack + "/renovate.json":          "renovate.json",
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

// generated returns what fix writes for a Go repository with the given
// task runner and container setting.
func generated(t *testing.T, runner, container string) map[string]string {
	t.Helper()
	p := fix.Project{Stack: "go", Runner: runner, Container: container}
	files := map[string]string{"go.mod": "module x\n\ngo 1.26.0\n"}
	for name, dest := range map[string]string{
		"go/pre-commit-config.yaml": ".pre-commit-config.yaml",
		"go/check.yml":              ".github/workflows/check.yml",
		repo.RunnerFile(runner):     repo.RunnerFile(runner),
		"go/devcontainer.json":      ".devcontainer/devcontainer.json",
	} {
		body, ok, err := catalog.EmbeddedTemplates{}.Compose(name, p)
		if err != nil || !ok {
			t.Fatalf("%s: ok=%v err=%v", name, ok, err)
		}
		if body != nil {
			files[dest] = string(body)
		}
	}
	return files
}

// A generated file belongs to the repository and will be edited. The rules
// check what the files achieve, so reasonable edits must keep passing: this
// is the test for that. Each case changes the generated files the way a
// maintainer would and names nothing that may fail.
func TestAlteredTemplatesStillPass(t *testing.T) {
	t.Parallel()
	const hooks, workflow = ".pre-commit-config.yaml", ".github/workflows/check.yml"
	replace := func(file, old, repl string) func(*testing.T, map[string]string) {
		return func(t *testing.T, f map[string]string) {
			t.Helper()
			if !strings.Contains(f[file], old) {
				t.Fatalf("%s no longer contains %q; the case is stale", file, old)
			}
			f[file] = strings.ReplaceAll(f[file], old, repl)
		}
	}
	cut := func(file, from, to string) func(*testing.T, map[string]string) {
		return func(t *testing.T, f map[string]string) {
			t.Helper()
			i := strings.Index(f[file], from)
			j := strings.Index(f[file], to)
			if i < 0 || j < i {
				t.Fatalf("%s: cannot cut from %q to %q; the case is stale", file, from, to)
			}
			f[file] = f[file][:i] + f[file][j:]
		}
	}
	cases := map[string]struct {
		runner, container string
		config            string
		edits             []func(*testing.T, map[string]string)
	}{
		"renamed job and workflow": {repo.RunnerMake, facts.ContainerNone, "", []func(*testing.T, map[string]string){
			replace(workflow, "  check:\n    runs-on", "  verify:\n    runs-on"),
			replace(workflow, "name: check", "name: Verify"),
			replace(workflow, "timeout-minutes: 30", "timeout-minutes: 45"),
		}},
		"bumped versions": {repo.RunnerMake, facts.ContainerNone, "", []func(*testing.T, map[string]string){
			replace(workflow, "3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1", "0123456789abcdef0123456789abcdef01234567 # v8.0.0"),
			replace(hooks, "rev: v8.30.1", "rev: v9.0.0"),
			replace(hooks, "rev: v4.4.0", "rev: v5.1.0"),
		}},
		"no shell hooks, own hooks added": {repo.RunnerMake, facts.ContainerNone, "", []func(*testing.T, map[string]string){
			cut(hooks, "  # Shell scripts (drop if the repo has none)", "  # Repository setup policy"),
			replace(hooks, "  # go hooks\n", "  - repo: local\n    hooks:\n      - id: generate\n        name: go generate is clean\n        entry: go generate ./...\n        language: system\n        pass_filenames: false\n\n  # go hooks\n"),
		}},
		"check through another runner": {repo.RunnerJust, facts.ContainerNone, "", []func(*testing.T, map[string]string){
			replace("justfile", "check: lint test cover audit build", "check: fmt lint test cover audit build"),
			replace(hooks, "entry: just check ", "entry: just --justfile justfile check "),
		}},
		"container through compose instead of the CLI": {repo.RunnerMake, facts.ContainerDevcontainer, "", []func(*testing.T, map[string]string){
			replace("Makefile", "\t@devcontainer up --workspace-folder . >/dev/null\n\tdevcontainer exec --workspace-folder . make", "\tdocker compose -f .devcontainer/compose.yaml exec dev make"),
		}},
		"podman image replaced by a pinned one": {repo.RunnerTask, facts.ContainerPodman, "facts: {tier: 1, visibility: public, taskrunner: task, container: podman}\n", []func(*testing.T, map[string]string){
			replace("Taskfile.yml", "mcr.microsoft.com/devcontainers/go:1", "registry.example.com/ci/go@sha256:abc"),
			replace(workflow, "mcr.microsoft.com/devcontainers/go:1", "registry.example.com/ci/go@sha256:abc"),
		}},
		"fleetlint from the hook only, pre-push through a script": {repo.RunnerMake, facts.ContainerNone, "", []func(*testing.T, map[string]string){
			replace("Makefile", "\tfleetlint check --fail-on error\n", ""),
			replace(hooks, "entry: make check ", "entry: ./scripts/check.sh "),
		}},
	}
	rules := []string{
		"hooks/config-present", "hooks/shared-hygiene", "hooks/secret-scan", "hooks/conventional-commits", "hooks/pre-push-check",
		"ci/check-workflow", "ci/least-privilege", "ci/job-timeouts", "ci/concurrency-cancel", "ci/actions-pinned",
		"taskrunner/check-composition", "quality/policy-enforced",
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			files := generated(t, tc.runner, tc.container)
			for _, edit := range tc.edits {
				edit(t, files)
			}
			cfg := tier1Public
			if tc.config != "" {
				cfg = "version: 1\nextends: [fleetlint:recommended]\n" + tc.config
			}
			out := run(t, files, cfg)
			check := rules
			if tc.container != facts.ContainerNone {
				check = append(append([]string{}, rules...), "taskrunner/container", "repo/dev-environment")
			}
			for _, id := range check {
				if s := status(t, out, id); s.Status != model.StatusPass {
					t.Errorf("%s: %s %q %+v", id, s.Status, s.Err, s.Findings)
				}
			}
		})
	}
}

// The lint templates close the lint rules they belong to: for each stack a
// bare manifest fails the rule, applying the rule's fix actions makes it
// pass, and applying them again changes nothing.
func TestLintFixesSatisfyTheirRules(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		files map[string]string
		rules []string
	}{
		"python":  {map[string]string{"pyproject.toml": "[project]\nname = \"x\"\nversion = \"0.1.0\"\n"}, []string{"lint/python-config", "lint/python-rules-enabled"}},
		"rust":    {map[string]string{"Cargo.toml": "[package]\nname = \"x\"\nversion = \"0.1.0\"\nedition = \"2021\"\n"}, []string{"lint/rust-config", "lint/rust-lints-denied"}},
		"node":    {map[string]string{"package.json": "{\"name\": \"x\"}\n", "tsconfig.json": "{\n  \"compilerOptions\": {\n    \"target\": \"es2022\"\n  }\n}\n"}, []string{"lint/node-config", "lint/node-strict-flags"}},
		"kotlin":  {map[string]string{"build.gradle.kts": "plugins {}\n"}, []string{"lint/kotlin-config", "lint/kotlin-detekt-strict"}},
		"flutter": {map[string]string{"pubspec.yaml": "name: x\n"}, []string{"lint/flutter-config", "lint/flutter-strict-modes"}},
	}
	for stack, tc := range cases {
		t.Run(stack, func(t *testing.T) {
			t.Parallel()
			files := map[string]string{}
			for k, v := range tc.files {
				files[k] = v
			}
			files[config.FileName] = tier1Public
			scope := testutil.GitFixture(t, files)
			first := evaluate(t, scope)
			for pass := 0; pass < 2; pass++ {
				var changed int
				for _, res := range first.Results {
					if !slices.Contains(tc.rules, res.Rule.ID) || res.Status != model.StatusFail {
						continue
					}
					actions, err := fix.Decode(res.Rule.Fix.Actions)
					if err != nil {
						t.Fatal(err)
					}
					changes, err := fix.PlanFor(scope, fix.Project{Stack: stack}, actions, catalog.EmbeddedTemplates{}, res.Findings)
					if err != nil {
						t.Fatalf("%s: %v", res.Rule.ID, err)
					}
					if err := fix.Apply(changes); err != nil {
						t.Fatal(err)
					}
					changed += len(changes)
				}
				if pass == 0 && changed == 0 {
					t.Fatal("the bare manifest must fail at least one lint rule with a fix")
				}
				if pass == 1 && changed != 0 {
					t.Errorf("second pass changed %d files: the fixes are not idempotent", changed)
				}
				// A fresh handle: the repository caches parsed documents.
				fresh, err := repo.Open(context.Background(), scope.Root)
				if err != nil {
					t.Fatal(err)
				}
				scope = fresh
				first = evaluate(t, scope)
			}
			for _, id := range tc.rules {
				if s := status(t, first, id); s.Status != model.StatusPass {
					t.Errorf("%s after fix: %s %q %+v", id, s.Status, s.Err, s.Findings)
				}
			}
		})
	}
}
