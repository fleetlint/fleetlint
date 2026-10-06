package facts_test

import (
	"reflect"
	"testing"

	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

func TestWorkspaceMembers(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		files map[string]string
		want  []string
	}{
		"go.work": {map[string]string{
			"go.work": "go 1.24\n\nuse (\n\t./api\n\n\t// retired: ./old\n\t./worker\n)\n", "api/go.mod": "module a\n", "worker/go.mod": "module w\n", "docs/readme.md": "x",
		}, []string{"api", "worker"}},
		"pnpm": {map[string]string{
			"pnpm-workspace.yaml": "packages:\n  - 'packages/*'\n  - '!packages/skip'\n", "packages/ui/package.json": "{}", "packages/core/package.json": "{}", "packages/notes/README.md": "x",
		}, []string{"packages/core", "packages/ui"}},
		"package.json workspaces": {map[string]string{
			"package.json": `{"workspaces":["apps/*"]}`, "apps/web/package.json": "{}",
		}, []string{"apps/web"}},
		"cargo": {map[string]string{
			"Cargo.toml": "[workspace]\nmembers = [\"crates/*\"]\n", "crates/a/Cargo.toml": "[package]\n", "crates/b/Cargo.toml": "[package]\n",
		}, []string{"crates/a", "crates/b"}},
		"gradle": {map[string]string{
			"settings.gradle.kts": `include(":app", ":core:data")` + "\n", "app/build.gradle.kts": "", "core/data/build.gradle.kts": "",
		}, []string{"app", "core/data"}},
		"single": {map[string]string{"go.mod": "module x\n"}, nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := testutil.Fixture(t, tc.files)
			f := facts.Discover(r, facts.Overrides{})
			if !reflect.DeepEqual(f.Members, tc.want) {
				t.Fatalf("members = %v, want %v", f.Members, tc.want)
			}
			wantLayout := "single"
			if len(tc.want) > 0 {
				wantLayout = "workspace"
			}
			if f.Layout != wantLayout {
				t.Fatalf("layout = %s, want %s", f.Layout, wantLayout)
			}
		})
	}
}

func TestStacksAndTaskRunner(t *testing.T) {
	t.Parallel()
	r := testutil.Fixture(t, map[string]string{
		"package.json":   `{"scripts":{"lint":"eslint .","test":"vitest","check":"npm run lint && npm test"}}`,
		"pyproject.toml": "[project]\nname='x'\n",
	})
	f := facts.Discover(r, facts.Overrides{})
	if !reflect.DeepEqual(f.Stacks, []string{"node", "python"}) {
		t.Fatalf("stacks = %v", f.Stacks)
	}
	if f.TaskRunner.Kind != "npm-scripts" || !reflect.DeepEqual(f.TaskRunner.Targets, []string{"check", "lint", "test"}) {
		t.Fatalf("taskrunner = %+v", f.TaskRunner)
	}
	if f.Tier != 3 || f.Sources["tier"] != facts.SourceDefault {
		t.Fatalf("plain repo defaults to tier 3, got %d", f.Tier)
	}
	pinned := facts.Discover(r, facts.Overrides{Tier: 1, Visibility: facts.VisibilityPublic})
	if pinned.Tier != 1 || !pinned.Public() || pinned.Sources["tier"] != facts.SourceConfigured {
		t.Fatalf("overrides not applied: %+v", pinned)
	}
}

// A Hugo site is recognised by its hugo.* configuration, or by a legacy
// config.toml that names the baseURL next to a content directory; a
// config.toml alone is not Hugo.
func TestHugoDetection(t *testing.T) {
	t.Parallel()
	for name, files := range map[string]map[string]string{
		"hugo.toml":          {"hugo.toml": "baseURL = 'https://x/'\n"},
		"config dir":         {"config/_default/hugo.yaml": "baseURL: https://x/\n"},
		"legacy config.toml": {"config.toml": "baseURL = 'https://x/'\ntitle = 'x'\n", "content/_index.md": "# x\n"},
	} {
		if f := facts.Discover(testutil.Fixture(t, files), facts.Overrides{}); !reflect.DeepEqual(f.Stacks, []string{"hugo"}) {
			t.Errorf("%s: stacks = %v", name, f.Stacks)
		}
	}
	plain := facts.Discover(testutil.Fixture(t, map[string]string{"config.toml": "name = 'x'\n", "content/a.md": "a\n"}), facts.Overrides{})
	if len(plain.Stacks) != 0 {
		t.Errorf("a config.toml without baseURL is not Hugo: %v", plain.Stacks)
	}
}

func TestNestedProjectsWithoutWorkspaceFile(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"go.mod":                     "module x\n",
		"frontend/package.json":      "{}",
		"frontend/src/package.json":  "{}",
		"samples/demo/package.json":  "{}",
		"samples/package.json":       "{}",
		"docs/requirements.txt":      "mkdocs\n",
		".github/tools/package.json": "{}",
		"worker/pyproject.toml":      "[project]\nname = \"w\"\n",
	}
	f := facts.Discover(testutil.GitFixture(t, files), facts.Overrides{})
	if f.Layout != "nested" || !reflect.DeepEqual(f.Members, []string{"frontend", "worker"}) {
		t.Fatalf("layout=%s members=%v, want nested [frontend worker]", f.Layout, f.Members)
	}
	// Without git there is no way to tell a project from an installed dependency.
	if f := facts.Discover(testutil.Fixture(t, files), facts.Overrides{}); f.Layout != "single" {
		t.Fatalf("untracked tree: layout=%s members=%v", f.Layout, f.Members)
	}
}

func TestContainerFact(t *testing.T) {
	t.Parallel()
	with := map[string]string{"go.mod": "module x\n", ".devcontainer/devcontainer.json": "{}"}
	without := map[string]string{"go.mod": "module x\n"}
	cases := map[string]struct {
		files map[string]string
		pin   string
		want  string
		src   facts.Source
	}{
		"dev container present": {with, "", facts.ContainerDevcontainer, facts.SourceDetected},
		"nothing":               {without, "", facts.ContainerNone, facts.SourceDetected},
		"wanted, not there":     {without, facts.ContainerDevcontainer, facts.ContainerDevcontainer, facts.SourceConfigured},
		"there, not used":       {with, facts.ContainerNone, facts.ContainerNone, facts.SourceConfigured},
		"podman":                {without, facts.ContainerPodman, facts.ContainerPodman, facts.SourceConfigured},
		"named configuration":   {map[string]string{".devcontainer/api/devcontainer.json": "{}"}, "", facts.ContainerDevcontainer, facts.SourceDetected},
	}
	for name, tc := range cases {
		f := facts.Discover(testutil.Fixture(t, tc.files), facts.Overrides{Container: tc.pin})
		if f.Container != tc.want || f.Sources["container"] != tc.src {
			t.Errorf("%s: container=%s (%s), want %s (%s)", name, f.Container, f.Sources["container"], tc.want, tc.src)
		}
	}
}

func TestTaskRunnerKinds(t *testing.T) {
	t.Parallel()
	justfile := "set shell := [\"bash\", \"-c\"]\nimage := \"x\"\n\n# lint it\nlint:\n    golangci-lint run\n\n@test *args: lint\n    go test {{args}} ./...\n\ncheck: lint (test \"-race\") && build\n\nbuild:\n    go build ./...\n"
	taskfile := "version: '3'\ntasks:\n  lint:\n    cmds: [golangci-lint run]\n  test:\n    deps: [lint]\n    cmds:\n      - cmd: go test ./...\n  check:\n    cmds:\n      - task: lint\n      - task: test\n      - fleetlint check\n  build: go build ./...\n"
	cases := map[string]struct {
		files      map[string]string
		want       string
		kind, cmd  string
		targets    []string
		checkDeps  []string
		lintRecipe string
	}{
		"make":           {map[string]string{"Makefile": "lint:\n\tgolangci-lint run\ncheck: lint test\ntest:\n\tgo test ./...\n"}, "", "make", "make", []string{"check", "lint", "test"}, []string{"lint", "test"}, "golangci-lint run"},
		"just":           {map[string]string{"justfile": justfile}, "", "just", "just", []string{"build", "check", "lint", "test"}, []string{"lint", "test", "build"}, "golangci-lint run"},
		"task":           {map[string]string{"Taskfile.yml": taskfile}, "", "task", "task", []string{"build", "check", "lint", "test"}, []string{"lint", "test"}, "golangci-lint run"},
		"configured":     {map[string]string{"Makefile": "x:\n\ttrue\n", "justfile": justfile}, "just", "just", "just", []string{"build", "check", "lint", "test"}, []string{"lint", "test", "build"}, "golangci-lint run"},
		"wanted, absent": {map[string]string{"go.mod": "module x\n"}, "task", "task", "task", nil, nil, ""},
		"none":           {map[string]string{"go.mod": "module x\n"}, "", "none", "make", nil, nil, ""},
	}
	for name, tc := range cases {
		tr := facts.Discover(testutil.Fixture(t, tc.files), facts.Overrides{TaskRunner: tc.want}).TaskRunner
		if tr.Kind != tc.kind || tr.Cmd != tc.cmd || !reflect.DeepEqual(tr.Targets, tc.targets) {
			t.Errorf("%s: kind=%s cmd=%s targets=%v", name, tr.Kind, tr.Cmd, tr.Targets)
		}
		if !reflect.DeepEqual(tr.Deps["check"], tc.checkDeps) {
			t.Errorf("%s: check depends on %v, want %v", name, tr.Deps["check"], tc.checkDeps)
		}
		if tc.lintRecipe != "" && (len(tr.Recipes["lint"]) != 1 || tr.Recipes["lint"][0] != tc.lintRecipe) {
			t.Errorf("%s: lint recipe %v", name, tr.Recipes["lint"])
		}
	}
}
