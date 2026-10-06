package celenv_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/celenv"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/repo"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

func fixture(t *testing.T, files map[string]string) *repo.Repo {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r, err := repo.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestExpressions(t *testing.T) {
	t.Parallel()
	r := fixture(t, map[string]string{
		"Makefile":                      "check: lint test ## green\n\tgo test ./...\nlint:\n\tgolangci-lint run\n",
		"go.mod":                        "module x\n",
		".pre-commit-config.yaml":       "repos:\n  - repo: builtin\n    hooks:\n      - id: detect-private-key\n",
		"pyproject.toml":                "[tool.ruff]\nline-length = 100\n",
		"docs/notes.md":                 "a\nTODO: later\nb\n",
		".github/workflows/release.yml": "on:\n  push:\n    tags: ['v*']\npermissions:\n  contents: read\n",
	})
	f := facts.Discover(r, facts.Overrides{})
	env, err := celenv.New(r, f)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{
		`file("Makefile")`:                        true,
		`exists("**/*.md")`:                       true,
		`exists("*.rs")`:                          false,
		`"check" in makefile("Makefile").targets`: true,
		`["check","lint"].all(t, t in makefile("Makefile").targets)`:                        true,
		`"test" in makefile("Makefile").deps["check"]`:                                      true,
		`yaml(".pre-commit-config.yaml").repos.exists(r, r.repo == "builtin")`:              true,
		`toml("pyproject.toml").tool.ruff["line-length"] == 100`:                            true,
		`yaml("missing.yaml") == null`:                                                      true,
		`size(scan("docs/notes.md", "TODO")) == 1 && scan("docs/notes.md", "TODO")[0] == 2`: true,
		`"go" in repo.stacks && "python" in repo.stacks`:                                    true,
		`release.exists && "v*" in release.tag_patterns`:                                    true,
		`matchesAny(glob("**"), "^docs/")`:                                                  true,
		`yaml(".github/workflows/release.yml").permissions.contents == "read"`:              true,
		`taskrunner.kind == "make"`:                                                         true,
	}
	for expr, want := range cases {
		got, err := env.Check(expr, nil)
		if err != nil {
			t.Errorf("%s: %v", expr, err)
			continue
		}
		if got != want {
			t.Errorf("%s = %v, want %v", expr, got, want)
		}
	}
}

func TestCompileErrors(t *testing.T) {
	t.Parallel()
	r := fixture(t, map[string]string{})
	env, err := celenv.New(r, facts.Discover(r, facts.Overrides{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, expr := range []string{`file(1)`, `nosuch("x")`, `"string result"`, `repo.tier +`} {
		if _, err := env.Compile(expr); err == nil {
			t.Errorf("%s: expected a compile error", expr)
		}
	}
}

func TestWorkflowAccessors(t *testing.T) {
	t.Parallel()
	r := fixture(t, map[string]string{
		".github/workflows/ci.yml":      "on: push\njobs:\n  a:\n    steps:\n      - uses: actions/checkout@v4\n      - name: lint\n        run: make lint\n",
		".github/workflows/release.yml": "on:\n  push:\n    tags: ['v*']\njobs:\n  rel:\n    steps:\n      - uses: anchore/sbom-action@v0\n        with: { format: spdx-json }\n      - run: syft scan dir:.\n",
	})
	env, err := celenv.New(r, facts.Discover(r, facts.Overrides{}))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{
		`size(workflows()) == 2`:                                                                                                     true,
		`tagged_workflows() == [".github/workflows/release.yml"]`:                                                                    true,
		`steps(".github/workflows/ci.yml").exists(s, s.run.contains("make lint"))`:                                                   true,
		`steps(".github/workflows/ci.yml").exists(s, s.uses.startsWith("actions/checkout"))`:                                         true,
		`tagged_workflows().exists(w, steps(w).exists(s, s.run.matches("\\bsyft\\b")))`:                                              true,
		`tagged_workflows().exists(w, steps(w).exists(s, s.uses.startsWith("anchore/sbom-action") && s.with.format == "spdx-json"))`: true,
		`steps("missing.yml").size() == 0`:                                                                                           true,
	}
	for expr, want := range cases {
		got, err := env.Check(expr, nil)
		if err != nil {
			t.Errorf("%s: %v", expr, err)
			continue
		}
		if got != want {
			t.Errorf("%s = %v, want %v", expr, got, want)
		}
	}
}

func TestCIAndHistoryAccessors(t *testing.T) {
	t.Parallel()
	r := testutil.GitFixture(t, map[string]string{
		"big.bin":                      strings.Repeat("x", 3000),
		".github/workflows/pr.yml":     "on: [pull_request, push]\nconcurrency: {group: pr, cancel-in-progress: true}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    timeout-minutes: 10\n    steps: [{run: make check}]\n  slow:\n    runs-on: ubuntu-latest\n    steps: [{run: make bench}]\n",
		".github/workflows/danger.yml": "on:\n  pull_request_target:\n    types: [opened]\njobs:\n  a:\n    steps:\n      - uses: actions/checkout@v4\n        with: {ref: \"${{ github.event.pull_request.head.sha }}\"}\n",
		".github/workflows/single.yml": "on: push\njobs: {a: {steps: [{run: true}]}}\n",
	})
	gitRun(t, r.Root, "tag", "v1.2.3")
	gitRun(t, r.Root, "tag", "release-2")
	fakeSignedTag(t, r.Root, "v0.9.0")
	gitRun(t, r.Root, "commit", "--allow-empty", "-q", "-m", "feat: x\n\nCo-Authored-By: Claude <noreply@anthropic.com>")
	env, err := celenv.New(r, facts.Discover(r, facts.Overrides{}))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{
		`filesize("big.bin") == 3000 && filesize("missing") == 0 && filesize(".github") == 0`:                                  true,
		`globmatch("*.bin", "big.bin") && !globmatch("*.go", "big.bin") && globmatch("**/*.yml", ".github/workflows/pr.yml")`:  true,
		`triggers(".github/workflows/pr.yml") == ["pull_request", "push"]`:                                                     true,
		`triggers(".github/workflows/danger.yml") == ["pull_request_target"]`:                                                  true,
		`triggers(".github/workflows/single.yml") == ["push"]`:                                                                 true,
		`triggers("missing.yml").size() == 0`:                                                                                  true,
		`jobs(".github/workflows/pr.yml").size() == 2`:                                                                         true,
		`jobs(".github/workflows/pr.yml").filter(j, j.timeout_minutes == 0).map(j, j.name) == ["slow"]`:                        true,
		`jobs(".github/workflows/pr.yml")[0].runs_on == "ubuntu-latest" && jobs(".github/workflows/pr.yml")[0].name == "slow"`: true,
		`yaml(".github/workflows/pr.yml").concurrency["cancel-in-progress"] == true`:                                           true,
		`tags() == ["v1.2.3", "v0.9.0", "release-2"]`:                                                                          true,
		`tag_signed("v0.9.0") && !tag_signed("v1.2.3") && !tag_signed("missing") && !tag_signed("")`:                           true,
		`commits(10).size() == 2`: true,
		`commits(1)[0].subject == "feat: x" && commits(1)[0].author == "t" && commits(1)[0].email == "t@t"`: true,
		`commits(1)[0].trailers.contains("Co-Authored-By: Claude")`:                                         true,
		`commits(2)[1].trailers == ""`: true,
	}
	for expr, want := range cases {
		got, err := env.Check(expr, nil)
		if err != nil {
			t.Errorf("%s: %v", expr, err)
			continue
		}
		if got != want {
			t.Errorf("%s = %v, want %v", expr, got, want)
		}
	}
	if _, err := env.Check(`commits(0).size() == 0`, nil); err == nil {
		t.Error("commits(0) must be an error, not an empty list")
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = testutil.GitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestCodegrep(t *testing.T) {
	t.Parallel()
	r := fixture(t, map[string]string{
		"go.mod":            "module x\n",
		"a.go":              "package a\n// Marker here\n",
		"a_test.go":         "package a\n// Marker here\n",
		"vendor/v/v.go":     "package v\n// Marker here\n",
		"api/client.pb.go":  "package api\n// Marker here\n",
		"docs/notes.md":     "Marker here\n",
		"scripts/deploy.sh": "# marker in lower case\n",
	})
	env, err := celenv.New(r, facts.Discover(r, facts.Overrides{}))
	if err != nil {
		t.Fatal(err)
	}
	for expr, want := range map[string]bool{
		`codegrep("code", "Marker").map(m, m.path) == ["a.go", "a_test.go"]`:                                true,
		`codegrep("nontest", "Marker").map(m, m.path) == ["a.go"]`:                                          true,
		`codegrep("test", "Marker")[0].line == 2 && codegrep("test", "Marker")[0].text == "// Marker here"`: true,
		`codegrep("code", "(?i)marker").size() == 3`:                                                        true,
	} {
		prg, err := env.Compile(expr)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		got, err := env.EvalBool(prg, nil)
		if err != nil || got != want {
			t.Errorf("%s = %v (%v), want %v", expr, got, err, want)
		}
	}
	prg, err := env.Compile(`codegrep("docs", "x").size() == 0`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.EvalBool(prg, nil); err == nil {
		t.Error("an unknown kind must be an evaluation error, not an empty list")
	}
}

// fakeSignedTag writes an annotated tag whose message carries an SSH
// signature block, the way `git tag -s` does; git does not check it.
func fakeSignedTag(t *testing.T, dir, name string) {
	t.Helper()
	head := exec.CommandContext(context.Background(), "git", "-C", dir, "rev-parse", "HEAD")
	head.Env = testutil.GitEnv()
	sha, err := head.Output()
	if err != nil {
		t.Fatal(err)
	}
	body := "object " + strings.TrimSpace(string(sha)) + "\ntype commit\ntag " + name + "\ntagger t <t@t> 0 +0000\n\n" + name +
		"\n-----BEGIN SSH SIGNATURE-----\nU1NIU0lHAAAAAQ==\n-----END SSH SIGNATURE-----\n"
	mk := exec.CommandContext(context.Background(), "git", "-C", dir, "mktag")
	mk.Env = testutil.GitEnv()
	mk.Stdin = strings.NewReader(body)
	obj, err := mk.Output()
	if err != nil {
		t.Fatalf("mktag: %v", err)
	}
	gitRun(t, dir, "update-ref", "refs/tags/"+name, strings.TrimSpace(string(obj)))
}
