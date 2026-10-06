package catalog_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/fix"
	"github.com/fleetlint/fleetlint/internal/repo"
)

func TestComposeHooksForNestedProjects(t *testing.T) {
	t.Parallel()
	nested := []fix.Nested{{Path: "frontend", Stack: "node"}, {Path: "tools/gen", Stack: "go"}}
	body, ok, err := catalog.EmbeddedTemplates{}.Compose("go/pre-commit-config.yaml", fix.Project{Nested: nested})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	var doc struct {
		Repos []struct {
			Hooks []struct {
				ID, Entry, Files string
			}
		}
	}
	if err := yaml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("composed hooks are not valid YAML: %v\n%s", err, body)
	}
	hooks := map[string][2]string{}
	for _, r := range doc.Repos {
		for _, h := range r.Hooks {
			if _, dup := hooks[h.ID]; dup {
				t.Errorf("hook id %s appears twice", h.ID)
			}
			hooks[h.ID] = [2]string{h.Entry, h.Files}
		}
	}
	want := map[string][2]string{
		"golangci-lint":         {"golangci-lint run --new-from-rev=HEAD --fix", ""},
		"eslint-frontend":       {`bash -c 'cd frontend && npx --no-install eslint --fix --max-warnings=0 "${@#frontend/}"' --`, "^frontend/"},
		"go-mod-tidy":           {"go mod tidy -diff", `^go\.(mod|sum)$`},
		"go-mod-tidy-tools-gen": {`bash -c 'cd tools/gen && go mod tidy -diff "${@#tools/gen/}"' --`, `^tools/gen/go\.(mod|sum)$`},
	}
	for id, w := range want {
		if hooks[id] != w {
			t.Errorf("hook %s: entry=%q files=%q, want %q %q", id, hooks[id][0], hooks[id][1], w[0], w[1])
		}
	}
}

func TestComposeCheckWorkflowForNestedProjects(t *testing.T) {
	t.Parallel()
	body, ok, err := catalog.EmbeddedTemplates{}.Compose("go/check.yml", fix.Project{Nested: []fix.Nested{{Path: "frontend", Stack: "node"}, {Path: "tools/gen", Stack: "go"}}})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("composed workflow is not valid YAML: %v\n%s", err, body)
	}
	s := string(body)
	for _, want := range []string{"go-version-file: go.mod", "node-version-file: frontend/.nvmrc", "run: make check"} {
		if !strings.Contains(s, want) {
			t.Errorf("workflow lacks %q:\n%s", want, s)
		}
	}
	if strings.Count(s, "actions/setup-go") != 1 {
		t.Errorf("a stack's toolchain is set up once:\n%s", s)
	}
	// A plain file is not composed, and a single stack equals composing nothing.
	if _, ok, _ := (catalog.EmbeddedTemplates{}).Compose("cliff.toml", fix.Project{}); ok {
		t.Error("cliff.toml is a plain template")
	}
	single, err := catalog.EmbeddedTemplates{}.Template("go/check.yml")
	if err != nil || strings.Contains(string(single), "setup-node") {
		t.Errorf("single stack: err=%v\n%s", err, single)
	}
}

func TestComposeDevcontainer(t *testing.T) {
	t.Parallel()
	body, ok, err := catalog.EmbeddedTemplates{}.Compose("go/devcontainer.json", fix.Project{Container: "devcontainer", Nested: []fix.Nested{{Path: "frontend", Stack: "node"}}})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	var doc struct {
		Image             string
		Features          map[string]map[string]any
		PostCreateCommand string `yaml:"postCreateCommand"`
	}
	if err := yaml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, body)
	}
	if len(doc.Features) != 2 || doc.Features["ghcr.io/devcontainers/features/go:1"] == nil || doc.Features["ghcr.io/devcontainers/features/node:1"] == nil {
		t.Errorf("features for both stacks expected: %v", doc.Features)
	}
	if !strings.Contains(doc.PostCreateCommand, "make tools") {
		t.Errorf("the container must hand over to the task runner: %q", doc.PostCreateCommand)
	}
	flutter, _, err := catalog.EmbeddedTemplates{}.Compose("flutter/devcontainer.json", fix.Project{Container: "devcontainer"})
	if err != nil || !strings.Contains(string(flutter), "flutter:stable") {
		t.Errorf("flutter uses an image: err=%v\n%s", err, flutter)
	}
	if _, _, err := (catalog.EmbeddedTemplates{}).Compose("cobol/devcontainer.json", fix.Project{Container: "devcontainer"}); err == nil {
		t.Error("an unknown stack has no dev container")
	}
	if body, ok, err := (catalog.EmbeddedTemplates{}).Compose("go/devcontainer.json", fix.Project{}); body != nil || !ok || err != nil {
		t.Errorf("without the flag no container is written: body=%q ok=%v err=%v", body, ok, err)
	}
}

// The Makefile written for a repository with the dev container flag sends
// every target into the container, and runs it directly once inside or when
// told to stay on the machine.
func TestMakefileDevcontainerSwitch(t *testing.T) {
	t.Parallel()
	if plain, _, _ := (catalog.EmbeddedTemplates{}).Compose("Makefile", fix.Project{}); strings.Contains(string(plain), "CONTAINER") {
		t.Fatalf("on the machine the Makefile has no switch:\n%s", plain)
	}
	body, ok, err := catalog.EmbeddedTemplates{}.Compose("Makefile", fix.Project{Container: "devcontainer"})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	dryRun := func(env string, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "make", append([]string{"-n", "-C", dir}, args...)...)
		// Start from an environment that says nothing about containers.
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "CONTAINER=") && !strings.HasPrefix(kv, "IN_CONTAINER=") && !strings.HasPrefix(kv, "MAKEFLAGS=") {
				cmd.Env = append(cmd.Env, kv)
			}
		}
		if env != "" {
			cmd.Env = append(cmd.Env, "IN_CONTAINER="+env)
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("make %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	if out := dryRun("", "lint"); !strings.Contains(out, "devcontainer exec --workspace-folder . make lint") || strings.Contains(out, "TODO: linter") {
		t.Errorf("outside the container the target must be forwarded:\n%s", out)
	}
	if out := dryRun("", "test", "COVER_MIN=90"); !strings.Contains(out, "make test COVER_MIN=90 CONTAINER=0") {
		t.Errorf("variables given on the command line must reach the container:\n%s", out)
	}
	for name, out := range map[string]string{"inside the container": dryRun("1", "lint"), "CONTAINER=0": dryRun("", "lint", "CONTAINER=0")} {
		if strings.Contains(out, "devcontainer exec") || !strings.Contains(out, "TODO: linter") {
			t.Errorf("%s the recipe runs directly:\n%s", name, out)
		}
	}
}

func TestCheckWorkflowUsesTheDevcontainer(t *testing.T) {
	t.Parallel()
	body, _, err := catalog.EmbeddedTemplates{}.Compose("go/check.yml", fix.Project{Container: "devcontainer"})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("not valid YAML: %v\n%s", err, body)
	}
	s := string(body)
	if !strings.Contains(s, "devcontainers/ci@") || !strings.Contains(s, "runCmd: make check") || strings.Contains(s, "actions/setup-go") {
		t.Errorf("the workflow must build the container and run the check in it, without its own toolchain:\n%s", s)
	}
}

// Renovate gets only the settings the repository's stacks call for.
func TestComposeRenovate(t *testing.T) {
	t.Parallel()
	read := func(p fix.Project) map[string]any {
		t.Helper()
		body, ok, err := catalog.EmbeddedTemplates{}.Compose(p.Stack+"/renovate.json", p)
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Fatalf("not JSON: %v\n%s", err, body)
		}
		return doc
	}
	goOnly := read(fix.Project{Stack: "go"})
	if goOnly["postUpdateOptions"] == nil || goOnly["lockFileMaintenance"] != nil {
		t.Errorf("a Go module tidies but has no lockfile to maintain: %v", goOnly)
	}
	none := read(fix.Project{})
	if none["postUpdateOptions"] != nil || none["lockFileMaintenance"] != nil || none["pre-commit"] == nil {
		t.Errorf("a repository without a stack gets neither: %v", none)
	}
	mixed := read(fix.Project{Stack: "go", Nested: []fix.Nested{{Path: "frontend", Stack: "node"}}})
	if mixed["postUpdateOptions"] == nil || mixed["lockFileMaintenance"] == nil {
		t.Errorf("go plus node gets both: %v", mixed)
	}
}

func TestComposeReleaseWorkflow(t *testing.T) {
	t.Parallel()
	compose := func(t *testing.T, name string, p fix.Project) string {
		t.Helper()
		body, ok, err := catalog.EmbeddedTemplates{}.Compose(name, p)
		if err != nil || !ok {
			t.Fatalf("%s: ok=%v err=%v", name, ok, err)
		}
		var doc map[string]any
		if err := yaml.Unmarshal(body, &doc); err != nil {
			t.Fatalf("%s is not valid YAML: %v\n%s", name, err, body)
		}
		return string(body)
	}
	cases := map[string]struct {
		name  string
		p     fix.Project
		want  []string
		avoid []string
	}{
		"go releases through goreleaser": {
			"go/release.yml",
			fix.Project{Stack: "go"},
			[]string{"tags: [\"v*\"]", "goreleaser/goreleaser-action", "go-version-file: go.mod", "make check", "gitleaks_"},
			[]string{"make dist", "npm publish"},
		},
		"node builds dist and publishes to npm": {
			"node/release.yml",
			fix.Project{Stack: "node"},
			[]string{"setup-node", "make dist", "cosign sign-blob", "npm publish --provenance"},
			[]string{"goreleaser", "pypi-publish"},
		},
		"python publishes to pypi with just": {
			"python/release.yml",
			fix.Project{Stack: "python", Runner: repo.RunnerJust},
			[]string{"setup-just", "just check", "just dist", "pypa/gh-action-pypi-publish"},
			[]string{"make "},
		},
		"docker mode runs the job in the image": {
			"go/release.yml",
			fix.Project{Stack: "go", Container: facts.ContainerDocker},
			[]string{"container: mcr.microsoft.com/devcontainers/go:1", "IN_CONTAINER: \"1\""},
			nil,
		},
		"devcontainer mode runs on the runner": {
			"go/release.yml",
			fix.Project{Stack: "go", Container: facts.ContainerDevcontainer},
			[]string{"CONTAINER: \"0\""},
			[]string{"devcontainers/ci"},
		},
		"nested stacks get their toolchains": {
			"go/release.yml",
			fix.Project{Stack: "go", Nested: []fix.Nested{{Path: "web", Stack: "node"}}},
			[]string{"node-version-file: web/.nvmrc", "goreleaser/goreleaser-action"},
			nil,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := compose(t, c.name, c.p)
			for _, w := range c.want {
				if !strings.Contains(s, w) {
					t.Errorf("lacks %q:\n%s", w, s)
				}
			}
			for _, a := range c.avoid {
				if strings.Contains(s, a) {
					t.Errorf("contains %q:\n%s", a, s)
				}
			}
			if n := strings.Count(s, "download-syft"); n != 1 {
				t.Errorf("syft is installed %d times", n)
			}
		})
	}
}
