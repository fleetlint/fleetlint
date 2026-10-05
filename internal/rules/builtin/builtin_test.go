package builtin_test

import (
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/repo"
	"github.com/fleetlint/fleetlint/internal/rules"
	_ "github.com/fleetlint/fleetlint/internal/rules/builtin"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

func check(t *testing.T, id string, r *repo.Repo) rules.Outcome {
	t.Helper()
	c, ok := rules.Lookup(id)
	if !ok {
		t.Fatalf("rule %s not registered", id)
	}
	out, err := c.Check(rules.Context{Repo: r, Facts: facts.Discover(r, facts.Overrides{})})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTrackedJunk(t *testing.T) {
	t.Parallel()
	r := testutil.GitFixture(t, map[string]string{
		"src/main.go": "package main\n", ".DS_Store": "x", "notes.bak": "x", "debug.log": "x", "README.md": "ok",
	})
	out := check(t, "repo/no-tracked-junk", r)
	if len(out.Findings) != 3 {
		t.Fatalf("want 3 findings, got %d: %+v", len(out.Findings), out.Findings)
	}
	clean := testutil.GitFixture(t, map[string]string{"src/main.go": "package main\n"})
	if out := check(t, "repo/no-tracked-junk", clean); len(out.Findings) != 0 {
		t.Fatalf("clean repo should pass, got %+v", out.Findings)
	}
}

func TestLockfileCommitted(t *testing.T) {
	t.Parallel()
	missing := testutil.GitFixture(t, map[string]string{"go.mod": "module x\nrequire a.b/c v1.0.0\n", "package.json": "{}"})
	out := check(t, "repo/lockfile-committed", missing)
	if len(out.Findings) != 2 {
		t.Fatalf("want go and node findings, got %+v", out.Findings)
	}
	present := testutil.GitFixture(t, map[string]string{"go.mod": "module x\nrequire a.b/c v1.0.0\n", "go.sum": "x", "package.json": "{}", "pnpm-lock.yaml": "x"})
	if out := check(t, "repo/lockfile-committed", present); len(out.Findings) != 0 || !strings.Contains(out.Evidence, "pnpm-lock.yaml") {
		t.Fatalf("expected pass with evidence, got %+v / %q", out.Findings, out.Evidence)
	}
	noDeps := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n"})
	if out := check(t, "repo/lockfile-committed", noDeps); len(out.Findings) != 0 {
		t.Fatalf("go module without requirements needs no go.sum, got %+v", out.Findings)
	}
}

func TestActionsPinned(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 40)
	r := testutil.Fixture(t, map[string]string{
		".github/workflows/ci.yml": "jobs:\n  a:\n    steps:\n      - uses: actions/checkout@v4\n      - uses: actions/setup-go@" + sha + " # v5\n      - uses: ./local/action\n      - uses: docker://alpine:3\n",
		".gitea/workflows/x.yml":   "jobs:\n  b:\n    steps:\n      - uses: 'foo/bar@main'\n",
	})
	out := check(t, "ci/actions-pinned", r)
	if len(out.Findings) != 2 {
		t.Fatalf("want 2 unpinned, got %+v", out.Findings)
	}
	if out.Findings[0].Line != 4 || out.Findings[0].Path != ".github/workflows/ci.yml" {
		t.Fatalf("expected ci.yml:4, got %s:%d", out.Findings[0].Path, out.Findings[0].Line)
	}
}
