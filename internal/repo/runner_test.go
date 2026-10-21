package repo_test

import (
	"reflect"
	"testing"

	"github.com/fleetlint/fleetlint/internal/repo"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

func TestRunnerKindsFilesAndCommands(t *testing.T) {
	t.Parallel()
	if got := repo.RunnerKinds(); !reflect.DeepEqual(got, []string{"make", "just", "task", "gradle", "npm-scripts"}) {
		t.Errorf("kinds: %v", got)
	}
	files := map[string]string{"make": "Makefile", "just": "justfile", "task": "Taskfile.yml", "gradle": "Makefile", "npm-scripts": "Makefile", "none": "Makefile"}
	for kind, want := range files {
		if got := repo.RunnerFile(kind); got != want {
			t.Errorf("RunnerFile(%s) = %s, want %s", kind, got, want)
		}
	}
	cmds := map[string]string{"make": "make", "just": "just", "task": "task", "gradle": "./gradlew", "npm-scripts": "npm run", "bogus": "make"}
	for kind, want := range cmds {
		if got := repo.RunnerCmd(kind); got != want {
			t.Errorf("RunnerCmd(%s) = %s, want %s", kind, got, want)
		}
	}
}

func TestRunnerDetection(t *testing.T) {
	t.Parallel()
	r := testutil.Fixture(t, map[string]string{
		"Makefile":     "check: lint\n\tgo test ./...\n",
		"justfile":     "x:\n  echo\n",
		"package.json": `{"scripts": {"test": "jest", "odd": 1}}`,
		"gradlew":      "",
	})
	if got := r.RunnersFound(); !reflect.DeepEqual(got, []string{"make", "just", "gradle", "npm-scripts"}) {
		t.Errorf("found: %v", got)
	}
	first := r.Runner("")
	if first.Kind != repo.RunnerMake || first.File != "Makefile" || first.Cmd != "make" || !reflect.DeepEqual(first.Targets, []string{"check"}) {
		t.Errorf("first runner: %+v", first)
	}
	if !reflect.DeepEqual(first.Deps["check"], []string{"lint"}) || !reflect.DeepEqual(first.Recipes["check"], []string{"go test ./..."}) {
		t.Errorf("make deps/recipes: %+v", first)
	}
	npm := r.Runner(repo.RunnerNPM)
	if npm.Kind != repo.RunnerNPM || npm.Cmd != "npm run" || !reflect.DeepEqual(npm.Targets, []string{"odd", "test"}) || !reflect.DeepEqual(npm.Recipes["test"], []string{"jest"}) || npm.Recipes["odd"] != nil {
		t.Errorf("npm runner: %+v", npm)
	}
	gradle := r.Runner(repo.RunnerGradle)
	if gradle.Kind != repo.RunnerGradle || gradle.File != "gradlew" || gradle.Cmd != "./gradlew" || len(gradle.Targets) != 0 {
		t.Errorf("gradle runner: %+v", gradle)
	}
	// Configured but not present: the kind is reported with its default file.
	task := r.Runner(repo.RunnerTask)
	if task.Kind != repo.RunnerTask || task.File != "Taskfile.yml" || task.Cmd != "task" || len(task.Targets) != 0 || task.Deps == nil || task.Recipes == nil {
		t.Errorf("absent task runner: %+v", task)
	}
	empty := testutil.Fixture(t, map[string]string{})
	if got := empty.Runner(""); got.Kind != repo.RunnerNone || got.File != "Makefile" || got.Cmd != "make" || got.Deps == nil {
		t.Errorf("no runner: %+v", got)
	}
	if got := empty.RunnersFound(); got != nil {
		t.Errorf("nothing found: %v", got)
	}
}

func TestJustfileRunner(t *testing.T) {
	t.Parallel()
	r := testutil.Fixture(t, map[string]string{"justfile": `# comment
set shell := ["bash", "-c"]
version := "1.0"

default: build test

@build:
    cargo build

test: build (lint "strict") && docs # trailing comment
    cargo test

    cargo doc

lint mode:
	echo {{mode}}
docs:
    mdbook build
`})
	got := r.Runner("")
	if got.Kind != repo.RunnerJust || got.File != "justfile" || got.Cmd != "just" {
		t.Fatalf("runner: %+v", got)
	}
	if !reflect.DeepEqual(got.Targets, []string{"build", "default", "docs", "lint", "test"}) {
		t.Errorf("targets: %v", got.Targets)
	}
	wantDeps := map[string][]string{"default": {"build", "test"}, "build": nil, "test": {"build", "lint", "docs"}, "lint": nil, "docs": nil}
	for name, want := range wantDeps {
		if deps := got.Deps[name]; !reflect.DeepEqual(deps, want) {
			t.Errorf("deps[%s] = %v, want %v", name, deps, want)
		}
	}
	wantRecipes := map[string][]string{"build": {"cargo build"}, "test": {"cargo test", "cargo doc"}, "lint": {"echo {{mode}}"}, "docs": {"mdbook build"}}
	for name, want := range wantRecipes {
		if !reflect.DeepEqual(got.Recipes[name], want) {
			t.Errorf("recipes[%s] = %v, want %v", name, got.Recipes[name], want)
		}
	}
	for _, notTarget := range []string{"set", "version"} {
		if _, ok := got.Deps[notTarget]; ok {
			t.Errorf("%s is a setting, not a recipe", notTarget)
		}
	}
}

func TestTaskfileRunner(t *testing.T) {
	t.Parallel()
	r := testutil.Fixture(t, map[string]string{"Taskfile.yml": `version: '3'
tasks:
  build:
    cmds:
      - go build ./...
      - task: lint
  lint: golangci-lint run
  test:
    deps: [build, {task: lint}, 7]
    cmd: go test ./...
  list:
    - echo a
    - cmd: echo b
    - 42
  odd: 3
`})
	got := r.Runner("")
	if got.Kind != repo.RunnerTask || got.File != "Taskfile.yml" || got.Cmd != "task" {
		t.Fatalf("runner: %+v", got)
	}
	if !reflect.DeepEqual(got.Targets, []string{"build", "lint", "list", "odd", "test"}) {
		t.Errorf("targets: %v", got.Targets)
	}
	wantDeps := map[string][]string{"build": {"lint"}, "test": {"build", "lint"}}
	for name, want := range wantDeps {
		if !reflect.DeepEqual(got.Deps[name], want) {
			t.Errorf("deps[%s] = %v, want %v", name, got.Deps[name], want)
		}
	}
	wantRecipes := map[string][]string{"build": {"go build ./..."}, "lint": {"golangci-lint run"}, "test": {"go test ./..."}, "list": {"echo a", "echo b"}}
	for name, want := range wantRecipes {
		if !reflect.DeepEqual(got.Recipes[name], want) {
			t.Errorf("recipes[%s] = %v, want %v", name, got.Recipes[name], want)
		}
	}
	if got.Recipes["odd"] != nil || got.Deps["odd"] != nil {
		t.Errorf("a task of another shape has nothing: %+v", got)
	}
	broken := testutil.Fixture(t, map[string]string{"Taskfile.yaml": "tasks: [\n"})
	if got := broken.Runner(""); got.Kind != repo.RunnerTask || len(got.Targets) != 0 {
		t.Errorf("an unparsable Taskfile is a task runner without targets: %+v", got)
	}
}
