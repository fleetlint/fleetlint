package engine_test

import (
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/model"
)

// These three rules were Go code once; as data they must find the same things.

func TestTrackedJunk(t *testing.T) {
	t.Parallel()
	out := run(t, map[string]string{
		"src/main.go": "package main\n", ".DS_Store": "x", "notes.bak": "x", "debug.log": "x", "README.md": "ok",
		"web/node_modules/a/index.js": "x", "catalog.go": "package x\n", "logs.md": "not a log file\n",
	}, tier1Public)
	s := status(t, out, "repo/no-tracked-junk")
	paths := map[string]bool{}
	for _, f := range s.Findings {
		paths[f.Path] = true
	}
	if s.Status != model.StatusFail || len(paths) != 4 || !paths[".DS_Store"] || !paths["notes.bak"] || !paths["debug.log"] || !paths["web/node_modules/a/index.js"] {
		t.Fatalf("want exactly the four junk files, each located: %+v", s.Findings)
	}
	clean := run(t, map[string]string{"src/main.go": "package main\n"}, tier1Public)
	if s := status(t, clean, "repo/no-tracked-junk"); s.Status != model.StatusPass {
		t.Fatalf("clean repository: %+v", s)
	}
}

func TestLockfileCommitted(t *testing.T) {
	t.Parallel()
	missing := run(t, map[string]string{"go.mod": "module x\nrequire a.b/c v1.0.0\n", "package.json": "{}"}, tier1Public)
	s := status(t, missing, "repo/lockfile-committed")
	if s.Status != model.StatusFail || len(s.Findings) != 2 || !strings.Contains(s.Findings[0].Message+s.Findings[1].Message, "(go)") || !strings.Contains(s.Findings[0].Message+s.Findings[1].Message, "(node)") {
		t.Fatalf("want one finding for go and one for node: %+v", s.Findings)
	}
	present := run(t, map[string]string{"go.mod": "module x\nrequire a.b/c v1.0.0\n", "go.sum": "x", "package.json": "{}", "pnpm-lock.yaml": "x"}, tier1Public)
	if s := status(t, present, "repo/lockfile-committed"); s.Status != model.StatusPass {
		t.Fatalf("both lockfiles present: %+v", s)
	}
	noDeps := run(t, map[string]string{"go.mod": "module x\n"}, tier1Public)
	if s := status(t, noDeps, "repo/lockfile-committed"); s.Status != model.StatusPass {
		t.Fatalf("a Go module without requirements needs no go.sum: %+v", s)
	}
}

func TestActionsPinned(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 40)
	long := "      - uses: some-organization/an-action-with-a-long-name/in/a/subdirectory@" + sha + " # v12.34.56\n"
	out := run(t, map[string]string{
		".github/workflows/ci.yml": "on: push\njobs:\n  a:\n    steps:\n      - uses: actions/checkout@v4\n      - uses: actions/setup-go@" + sha + " # v5\n      - uses: ./local/action\n      - uses: docker://alpine:3\n" + long,
		".gitea/workflows/x.yml":   "on: push\njobs:\n  b:\n    steps:\n      - uses: 'foo/bar@main'\n",
	}, tier1Public)
	s := status(t, out, "ci/actions-pinned")
	if s.Status != model.StatusFail || len(s.Findings) != 2 {
		t.Fatalf("want the two unpinned references only: %+v", s.Findings)
	}
	if s.Findings[0].Path != ".github/workflows/ci.yml" || s.Findings[0].Line != 5 || !strings.Contains(s.Findings[0].Message, "actions/checkout@v4") {
		t.Fatalf("the finding names file, line and reference: %+v", s.Findings[0])
	}
	if s.Findings[1].Path != ".gitea/workflows/x.yml" {
		t.Fatalf("gitea workflows are checked too: %+v", s.Findings[1])
	}
}
