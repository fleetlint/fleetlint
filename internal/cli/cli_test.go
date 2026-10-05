package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/cli"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

func runCLI(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code, err := cli.Run(context.Background(), append([]string{"-C", dir, "--color=false"}, args...), &out, &errOut)
	if err != nil {
		errOut.WriteString(err.Error())
	}
	return code, out.String(), errOut.String()
}

func messyRepo(t *testing.T) string {
	t.Helper()
	r := testutil.GitFixture(t, map[string]string{
		"go.mod":    "module x\nrequire a.b/c v1\n",
		".DS_Store": "x",
		"README.md": "# x\n",
	})
	return r.Root
}

func TestCheckExitCodesAndFormats(t *testing.T) {
	t.Parallel()
	dir := messyRepo(t)
	code, out, _ := runCLI(t, dir, "check")
	if code != cli.ExitFindings || !strings.Contains(out, "repo/no-tracked-junk") {
		t.Fatalf("code=%d out=%s", code, out)
	}
	code, _, _ = runCLI(t, dir, "check", "--fail-on", "never")
	if code != cli.ExitOK {
		t.Fatalf("--fail-on never should exit 0, got %d", code)
	}
	code, out, _ = runCLI(t, dir, "check", "-f", "json")
	var doc map[string]any
	if code != cli.ExitFindings || json.Unmarshal([]byte(out), &doc) != nil || doc["repo"] == nil {
		t.Fatalf("json output invalid: code=%d %s", code, out[:min(len(out), 200)])
	}
	_, out, _ = runCLI(t, dir, "check", "-f", "sarif", "--fail-on", "never")
	if !strings.Contains(out, `"version": "2.1.0"`) {
		t.Fatalf("sarif missing version: %s", out[:min(len(out), 200)])
	}
	_, out, _ = runCLI(t, dir, "check", "-f", "markdown", "--fail-on", "never")
	if !strings.Contains(out, "| FAIL | `repo/no-tracked-junk`") {
		t.Fatalf("markdown table missing: %s", out)
	}
	_, out, _ = runCLI(t, dir, "check", "-f", "agent", "--fail-on", "never")
	if !strings.Contains(out, "## 1.") || !strings.Contains(out, "Instruction:") {
		t.Fatalf("agent output missing sections: %s", out)
	}
	code, _, errOut := runCLI(t, dir, "check", "-f", "nope")
	if code != cli.ExitConfig || !strings.Contains(errOut, "unknown format") {
		t.Fatalf("bad format: code=%d err=%s", code, errOut)
	}
}

func TestDefaultCommandIsCheck(t *testing.T) {
	t.Parallel()
	dir := messyRepo(t)
	code, out, _ := runCLI(t, dir)
	if code != cli.ExitFindings || !strings.Contains(out, "passed") {
		t.Fatalf("bare invocation should run check: code=%d out=%s", code, out)
	}
}

func TestInitWritesConfigAndRefusesOverwrite(t *testing.T) {
	t.Parallel()
	dir := messyRepo(t)
	code, out, errOut := runCLI(t, dir, "init", "--tier", "2")
	if code != cli.ExitOK || !strings.Contains(out, "wrote .fleetlint.yaml") {
		t.Fatalf("init: code=%d out=%s err=%s", code, out, errOut)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".fleetlint.yaml"))
	if err != nil || !strings.Contains(string(b), "tier: 2") || !strings.Contains(string(b), "# discovered:") {
		t.Fatalf("config content: %s %v", b, err)
	}
	code, _, errOut = runCLI(t, dir, "init")
	if code != cli.ExitConfig || !strings.Contains(errOut, "already exists") {
		t.Fatalf("second init must refuse: code=%d err=%s", code, errOut)
	}
}

func TestConfigErrorsExitTwo(t *testing.T) {
	t.Parallel()
	r := testutil.Fixture(t, map[string]string{".fleetlint.yaml": "version: 1\nrules:\n  hooks/config-present: {enabled: false}\n"})
	code, _, errOut := runCLI(t, r.Root, "check")
	if code != cli.ExitConfig || !strings.Contains(errOut, "requires a reason") {
		t.Fatalf("code=%d err=%s", code, errOut)
	}
}

func TestExplainFactsSchemaPresets(t *testing.T) {
	t.Parallel()
	dir := messyRepo(t)
	code, out, _ := runCLI(t, dir, "explain", "repo/no-tracked-junk")
	if code != cli.ExitOK || !strings.Contains(out, "Requirement") || !strings.Contains(out, "Fix") {
		t.Fatalf("explain: %d %s", code, out)
	}
	code, _, errOut := runCLI(t, dir, "explain", "nope/nope")
	if code != cli.ExitConfig || !strings.Contains(errOut, "not in the loaded catalogs") {
		t.Fatalf("explain unknown: %d %s", code, errOut)
	}
	_, out, _ = runCLI(t, dir, "facts")
	if !strings.Contains(out, "stacks") || !strings.Contains(out, "go") {
		t.Fatalf("facts: %s", out)
	}
	_, out, _ = runCLI(t, dir, "facts", "--json")
	if !strings.Contains(out, `"stacks"`) {
		t.Fatalf("facts json: %s", out)
	}
	_, out, _ = runCLI(t, dir, "schema")
	if !strings.Contains(out, `"title": ".fleetlint.yaml"`) {
		t.Fatalf("schema: %s", out[:min(len(out), 100)])
	}
	_, out, _ = runCLI(t, dir, "catalog", "presets")
	if !strings.Contains(out, "fleetlint:minimal") || !strings.Contains(out, "fleetlint:recommended") {
		t.Fatalf("presets: %s", out)
	}
}

func TestFixDryRunThenApply(t *testing.T) {
	t.Parallel()
	dir := messyRepo(t)
	code, out, _ := runCLI(t, dir, "fix", "repo/no-tracked-junk", "repo/editorconfig")
	if code != cli.ExitOK || !strings.Contains(out, "- tracked: .DS_Store") || !strings.Contains(out, "+++ .editorconfig") {
		t.Fatalf("dry run: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".editorconfig")); err == nil {
		t.Fatal("dry run must not write")
	}
	code, out, _ = runCLI(t, dir, "fix", "--apply", "repo/no-tracked-junk", "repo/editorconfig")
	if code != cli.ExitOK || !strings.Contains(out, "applied") {
		t.Fatalf("apply: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".editorconfig")); err != nil {
		t.Fatal("apply must create the file")
	}
	_, out, _ = runCLI(t, dir, "check", "--fail-on", "never", "-v")
	if !strings.Contains(out, "ok    repo/editorconfig") || !strings.Contains(out, "ok    repo/no-tracked-junk") {
		t.Fatalf("rules should pass after fix: %s", out)
	}
}

func TestDocsCheckDetectsDrift(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	code, _, _ := runCLI(t, dir, "docs", "--out", filepath.Join(dir, "docs"))
	if code != cli.ExitOK {
		t.Fatalf("docs: %d", code)
	}
	code, _, _ = runCLI(t, dir, "docs", "--out", filepath.Join(dir, "docs"), "--check")
	if code != cli.ExitOK {
		t.Fatalf("freshly generated docs must pass --check, got %d", code)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "cel-reference.md"), []byte("edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runCLI(t, dir, "docs", "--out", filepath.Join(dir, "docs"), "--check")
	if code != cli.ExitConfig || !strings.Contains(errOut, "stale") {
		t.Fatalf("drift must fail: %d %s", code, errOut)
	}
}

func TestBaselineCommandRatchets(t *testing.T) {
	t.Parallel()
	dir := messyRepo(t)
	code, out, errOut := runCLI(t, dir, "baseline")
	if code != cli.ExitOK || !strings.Contains(out, "entries") {
		t.Fatalf("baseline: %d %s %s", code, out, errOut)
	}
	code, out, _ = runCLI(t, dir, "check", "--fail-on", "error")
	if code != cli.ExitOK || !strings.Contains(out, "baselined") {
		t.Fatalf("after baseline the repo must pass with baselined findings: %d\n%s", code, out)
	}
	// a new junk file is a new finding: not covered
	if err := os.WriteFile(filepath.Join(dir, "fresh.log"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitAdd(t, dir, "fresh.log")
	code, out, _ = runCLI(t, dir, "check", "--fail-on", "error")
	if code != cli.ExitFindings || !strings.Contains(out, "fresh.log") {
		t.Fatalf("new finding must fail: %d\n%s", code, out)
	}
	_, out, _ = runCLI(t, dir, "baseline")
	if !strings.Contains(out, "0 added") {
		t.Fatalf("re-baselining must not add the new finding: %s", out)
	}
}

func gitAdd(t *testing.T, dir, file string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", "-C", dir, "add", file)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
}

func TestFixAppliesInsideScopes(t *testing.T) {
	t.Parallel()
	r := testutil.GitFixture(t, map[string]string{
		"go.work":         "go 1.24\nuse (\n\t./api\n)\n",
		"api/go.mod":      "module a\n",
		"api/.DS_Store":   "x",
		".gitignore":      "bin/\n",
		".fleetlint.yaml": "version: 1\nextends: [fleetlint:minimal]\nfacts: {tier: 3}\n",
	})
	code, out, errOut := runCLI(t, r.Root, "fix", "--apply", "repo/no-tracked-junk")
	if code != cli.ExitOK || !strings.Contains(out, "applied") {
		t.Fatalf("apply in scope: %d %s %s", code, out, errOut)
	}
	tracked, _ := r.Git("ls-files")
	if strings.Contains(tracked, "api/.DS_Store") {
		t.Fatalf("junk inside the api scope must be untracked by its repo-relative path:\n%s", tracked)
	}
	if _, err := os.Stat(filepath.Join(r.Root, "api", ".DS_Store")); err != nil {
		t.Fatal("untrack keeps the file on disk")
	}
}

func TestBaselineDoesNotRegrowAtZero(t *testing.T) {
	t.Parallel()
	dir := testutil.GitFixture(t, map[string]string{
		"go.mod":          "module x\n",
		".DS_Store":       "x",
		".fleetlint.yaml": "version: 1\nextends: [fleetlint:minimal]\nfacts: {tier: 3}\nbaseline: .fleetlint-baseline.json\n",
	}).Root
	if code, out, errOut := runCLI(t, dir, "baseline"); code != cli.ExitOK {
		t.Fatalf("first baseline: %d %s %s", code, out, errOut)
	}
	if code, _, _ := runCLI(t, dir, "fix", "--apply", "repo/no-tracked-junk"); code != cli.ExitOK {
		t.Fatal("fix should apply")
	}
	if code, out, errOut := runCLI(t, dir, "baseline"); code != cli.ExitOK || !strings.Contains(out, "removed") {
		t.Fatalf("second baseline should shrink: %d %s %s", code, out, errOut)
	}
	// re-introduce the junk: a baseline at zero entries must refuse it without --reset
	testutil.WriteFiles(t, dir, map[string]string{"again.log": "x"})
	if _, err := repoGit(dir, "add", "again.log"); err != nil {
		t.Fatal(err)
	}
	_, out, _ := runCLI(t, dir, "baseline")
	if strings.Contains(out, "1 added") {
		t.Fatalf("an empty baseline must not grow again:\n%s", out)
	}
	code, _, _ := runCLI(t, dir, "check")
	if code != cli.ExitFindings {
		t.Fatal("the new finding must fail the check")
	}
}

func repoGit(dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestRequireFlag(t *testing.T) {
	t.Parallel()
	dir := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n", ".fleetlint.yaml": "version: 1\nextends: [fleetlint:minimal]\nfacts: {tier: 3}\n"}).Root
	if code, _, _ := runCLI(t, dir, "check", "--require", "fleetlint:minimal", "--fail-on", "never"); code != cli.ExitOK {
		t.Fatalf("satisfied requirement: exit %d", code)
	}
	code, _, errOut := runCLI(t, dir, "check", "--require", "fleetlint:minimal", "--require", "acme-baseline")
	if code != cli.ExitConfig || !strings.Contains(errOut, "acme-baseline") {
		t.Fatalf("missing requirement must be a config error naming the catalog: %d %s", code, errOut)
	}
	// --require is persistent: fix and explain refuse too, so a repo cannot be fixed under a weaker config.
	if code, _, _ := runCLI(t, dir, "explain", "repo/no-tracked-junk", "--require", "acme-baseline"); code != cli.ExitConfig {
		t.Fatalf("explain should honour --require: %d", code)
	}
}

// A finding is starred only when fix can repair it in this repository: a
// tracked junk file can be untracked, a Makefile that exists is not replaced.
func TestCheckMarksFixableFindings(t *testing.T) {
	t.Parallel()
	dir := testutil.GitFixture(t, map[string]string{
		"go.mod": "module x\n", ".DS_Store": "x", "Makefile": "build:\n\tgo build ./...\n",
		".fleetlint.yaml": "version: 1\nextends: [fleetlint:recommended]\nfacts: {tier: 1}\n",
	}).Root
	_, out, _ := runCLI(t, dir, "check")
	for _, want := range []string{"repo/no-tracked-junk *", "repo/editorconfig *", "can be fixed mechanically"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "taskrunner/targets *") {
		t.Errorf("an existing Makefile is not replaced, so the rule is not fixable:\n%s", out)
	}
	_, js, _ := runCLI(t, dir, "check", "--format", "json")
	if !strings.Contains(js, `"fixable": true`) {
		t.Error("json results carry the fixable flag")
	}
}

// A Go repository with a Node project in frontend/ gets one hook file and
// one workflow covering both, and they satisfy the rules that asked for them.
func TestFixCoversNestedProjects(t *testing.T) {
	t.Parallel()
	dir := testutil.GitFixture(t, map[string]string{
		"go.mod": "module x\n\ngo 1.24.0\n", "frontend/package.json": "{}",
		".fleetlint.yaml": "version: 1\nextends: [fleetlint:recommended]\nfacts: {tier: 1}\n",
	}).Root
	if code, out, errOut := runCLI(t, dir, "fix", "--apply"); code != cli.ExitOK {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	hooks, _ := os.ReadFile(filepath.Join(dir, ".pre-commit-config.yaml"))
	workflow, _ := os.ReadFile(filepath.Join(dir, ".github/workflows/check.yml"))
	if !strings.Contains(string(hooks), "id: golangci-lint") || !strings.Contains(string(hooks), "id: eslint-frontend") {
		t.Errorf("hooks must cover both stacks:\n%s", hooks)
	}
	if !strings.Contains(string(workflow), "actions/setup-go") || !strings.Contains(string(workflow), "node-version-file: frontend/.nvmrc") {
		t.Errorf("workflow must set up both toolchains:\n%s", workflow)
	}
	_, out, _ := runCLI(t, dir, "check")
	for _, id := range []string{"hooks/config-present", "hooks/shared-hygiene", "hooks/pre-push-check", "ci/check-workflow"} {
		if strings.Contains(out, id) {
			t.Errorf("%s still reported after fix:\n%s", id, out)
		}
	}
}

// With the dev container flag set and nothing there yet, fix writes the
// container, a Makefile that runs inside it and a workflow that does too.
func TestFixFollowsTheDevcontainerFlag(t *testing.T) {
	t.Parallel()
	dir := testutil.GitFixture(t, map[string]string{
		"go.mod":          "module x\n\ngo 1.26.0\n",
		".fleetlint.yaml": "version: 1\nextends: [fleetlint:recommended]\nfacts: {tier: 1, container: devcontainer}\n",
	}).Root
	_, before, _ := runCLI(t, dir, "check")
	if !strings.Contains(before, "repo/dev-environment *") {
		t.Fatalf("the flag asks for a container that is not there yet:\n%s", before)
	}
	if code, out, errOut := runCLI(t, dir, "fix", "--apply"); code != cli.ExitOK {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if !strings.Contains(read(".devcontainer/devcontainer.json"), `"IN_CONTAINER": "1"`) {
		t.Error("the container must mark itself for the Makefile")
	}
	if !strings.Contains(read("Makefile"), "devcontainer exec --workspace-folder . make $@") {
		t.Error("the Makefile must forward into the container")
	}
	if !strings.Contains(read(".github/workflows/check.yml"), "devcontainers/ci@") {
		t.Error("the workflow must run the check in the container")
	}
	_, after, _ := runCLI(t, dir, "check")
	for _, id := range []string{"repo/dev-environment", "taskrunner/container"} {
		if strings.Contains(after, id) {
			t.Errorf("%s still reported after fix:\n%s", id, after)
		}
	}
	// The same repository without the flag stays on the machine.
	bare := testutil.GitFixture(t, map[string]string{
		"go.mod":          "module x\n\ngo 1.26.0\n",
		".fleetlint.yaml": "version: 1\nextends: [fleetlint:recommended]\nfacts: {tier: 1}\n",
	}).Root
	runCLI(t, bare, "fix", "--apply")
	if b, _ := os.ReadFile(filepath.Join(bare, "Makefile")); strings.Contains(string(b), "devcontainer") {
		t.Errorf("without the flag the Makefile runs on the machine:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(bare, ".devcontainer")); err == nil {
		t.Error("without the flag fix must not introduce a dev container")
	}
	if _, out, _ := runCLI(t, bare, "check"); strings.Contains(out, "taskrunner/container") || strings.Contains(out, "repo/dev-environment *") {
		t.Errorf("on the machine the container rule does not apply and the tools are the user's choice:\n%s", out)
	}
}

// The same contract with other tools: a justfile whose recipes run in a
// podman image, a workflow whose job runs in that image, hooks that call
// `just check`. And a Taskfile on the machine.
func TestFixWritesForTheConfiguredTools(t *testing.T) {
	t.Parallel()
	read := func(dir, name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	dir := testutil.GitFixture(t, map[string]string{
		"go.mod":          "module x\n\ngo 1.26.0\n",
		".fleetlint.yaml": "version: 1\nextends: [fleetlint:recommended]\nfacts: {tier: 1, taskrunner: just, container: podman}\n",
	}).Root
	if code, out, errOut := runCLI(t, dir, "fix", "--apply"); code != cli.ExitOK {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	justfile := read(dir, "justfile")
	for _, want := range []string{`podman run --rm -v "$PWD":/work`, "{{run}}fleetlint check --fail-on error", "check: lint test cover audit build"} {
		if !strings.Contains(justfile, want) {
			t.Errorf("justfile lacks %q:\n%s", want, justfile)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "Makefile")); err == nil {
		t.Error("a just repository does not get a Makefile")
	}
	if _, err := os.Stat(filepath.Join(dir, ".devcontainer")); err == nil {
		t.Error("podman mode has no devcontainer.json")
	}
	workflow := read(dir, ".github/workflows/check.yml")
	for _, want := range []string{"container: mcr.microsoft.com/devcontainers/go", "extractions/setup-just@", "run: just check"} {
		if !strings.Contains(workflow, want) {
			t.Errorf("workflow lacks %q:\n%s", want, workflow)
		}
	}
	if !strings.Contains(read(dir, ".pre-commit-config.yaml"), "entry: just check") {
		t.Error("the pre-push hook must call the configured runner")
	}
	_, after, _ := runCLI(t, dir, "check")
	for _, id := range []string{"taskrunner/container", "repo/dev-environment", "ci/check-workflow", "hooks/pre-push-check"} {
		if strings.Contains(after, id) {
			t.Errorf("%s still reported after fix:\n%s", id, after)
		}
	}

	bare := testutil.GitFixture(t, map[string]string{
		"go.mod":          "module x\n\ngo 1.26.0\n",
		".fleetlint.yaml": "version: 1\nextends: [fleetlint:recommended]\nfacts: {tier: 1, taskrunner: task}\n",
	}).Root
	runCLI(t, bare, "fix", "--apply")
	taskfile := read(bare, "Taskfile.yml")
	if strings.Contains(taskfile, "RUN") || !strings.Contains(taskfile, "- 'fleetlint check --fail-on error'") {
		t.Errorf("on the machine the Taskfile runs its commands directly:\n%s", taskfile)
	}
	if w := read(bare, ".github/workflows/check.yml"); !strings.Contains(w, "arduino/setup-task@") || !strings.Contains(w, "run: task check") {
		t.Errorf("workflow must install and call task:\n%s", w)
	}
}

// init reports everything it will act on without being told: stacks, the
// nested project, the task runner (and that there are two), the container.
// Nothing of it needs pinning for check and fix to use it.
func TestInitPicksUpTheSetup(t *testing.T) {
	t.Parallel()
	dir := testutil.GitFixture(t, map[string]string{
		"go.mod": "module x\n\ngo 1.26.0\n", "frontend/package.json": "{}",
		"justfile": "lint:\n    go vet ./...\n", "Taskfile.yml": "version: '3'\ntasks:\n  lint: go vet ./...\n",
		".devcontainer/devcontainer.json": "{}",
	}).Root
	// --tier 1 because a repository without a remote counts as an experiment,
	// and experiments are not asked for hooks or CI.
	code, out, errOut := runCLI(t, dir, "init", "--tier", "1")
	if code != cli.ExitOK {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	for _, want := range []string{
		"stacks=[go]", "layout=nested", "taskrunner=just", "container=devcontainer",
		"# scopes (each checked as its own project): frontend.",
		"# task runners found: just, task. just is used; set `facts.taskrunner` to choose another.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("init output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "taskrunner: ") || strings.Contains(out, "container: ") || strings.Contains(out, "scopes:\n") {
		t.Errorf("detected facts are not pinned:\n%s", out)
	}
	_, facts, _ := runCLI(t, dir, "facts")
	for _, want := range []string{"container    devcontainer", "taskrunner   just (lint)", "scope        frontend stacks=node"} {
		if !strings.Contains(facts, want) {
			t.Errorf("facts lack %q:\n%s", want, facts)
		}
	}
	// fix then writes for what was detected: no Makefile next to the justfile,
	// hooks that call just, a workflow that uses the dev container.
	if code, out, errOut := runCLI(t, dir, "fix", "--apply"); code != cli.ExitOK {
		t.Fatalf("fix: exit %d\n%s\n%s", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "Makefile")); err == nil {
		t.Error("fix must not add a second task runner")
	}
	hooks, _ := os.ReadFile(filepath.Join(dir, ".pre-commit-config.yaml"))
	workflow, _ := os.ReadFile(filepath.Join(dir, ".github/workflows/check.yml"))
	if !strings.Contains(string(hooks), "entry: just check") || !strings.Contains(string(hooks), "eslint-frontend") {
		t.Errorf("hooks must call just and cover the nested project:\n%s", hooks)
	}
	if !strings.Contains(string(workflow), "devcontainers/ci@") || !strings.Contains(string(workflow), "runCmd: just check") {
		t.Errorf("the workflow must run just check in the dev container:\n%s", workflow)
	}
}

// The binary says which catalog it carries, on its own and in what it prints.
func TestVersionNamesTheCatalog(t *testing.T) {
	t.Parallel()
	_, out, _ := runCLI(t, t.TempDir(), "--version")
	if !strings.Contains(out, "fleetlint version ") || !strings.Contains(out, "\ncatalog ") || !strings.Contains(out, "github.com/fleetlint/catalog") {
		t.Errorf("--version must name the tool and the catalog:\n%s", out)
	}
	dir := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n"}).Root
	if _, out, _ := runCLI(t, dir, "check"); !strings.Contains(out, ", catalog ") {
		t.Errorf("check must end with the versions:\n%s", out)
	}
}

// A repository that pins a catalog version is checked and fixed with that
// version's rules and templates, straight from the cache, and every output
// says so.
func TestPinnedCatalogFromTheCache(t *testing.T) {
	cache := t.TempDir()
	t.Setenv(catalog.CacheEnv, cache)
	entry := catalog.CachePath(filepath.Join(cache, "catalogs"), catalog.DefaultRepo, "v9.9.9")
	testutil.WriteFiles(t, entry, map[string]string{
		"presets/recommended.yaml": "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: pinned, version: 9.9.9}\nrules:\n  - {id: v9/only, title: t, kind: expr, severity: warning, expr: 'file(\".editorconfig\")', message: m, requirement: r, fix: {human: h, actions: [{template: editorconfig, to: .editorconfig}]}}\n",
		"templates/editorconfig":   "# from v9\n",
		"COMMIT":                   strings.Repeat("cd", 20) + "\n",
	})
	dir := testutil.GitFixture(t, map[string]string{
		"go.mod":          "module x\n",
		".fleetlint.yaml": "version: 1\ncatalog: {version: v9.9.9}\nextends: [fleetlint:recommended]\n",
	}).Root
	_, out, errOut := runCLI(t, dir, "check")
	if !strings.Contains(out, "v9/only *") || strings.Contains(out, "repo/") {
		t.Fatalf("only the pinned catalog's rule applies:\n%s\n%s", out, errOut)
	}
	if !strings.Contains(out, "catalog v9.9.9 (cdcdcdcdcdcd), pinned in .fleetlint.yaml") {
		t.Errorf("the report must name the pinned version:\n%s", out)
	}
	if code, out, errOut := runCLI(t, dir, "fix", "--apply"); code != cli.ExitOK {
		t.Fatalf("fix: exit %d\n%s\n%s", code, out, errOut)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, ".editorconfig")); string(b) != "# from v9\n" {
		t.Errorf("the fix must write the pinned catalog's template, got %q", b)
	}
	if _, js, _ := runCLI(t, dir, "check", "--format", "json"); !strings.Contains(js, `"catalog": "v9.9.9 (cdcdcdcdcdcd), pinned in .fleetlint.yaml"`) {
		t.Errorf("json must name the pinned version:\n%s", js)
	}
}
