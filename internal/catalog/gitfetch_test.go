package catalog_test

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

const gitCatalogBody = "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: org, version: 1.0.0}\nrules: []\n"

func TestGitRefShape(t *testing.T) {
	t.Parallel()
	l := catalog.Loader{FetchGit: func(string, string, string) ([]byte, error) { return []byte(gitCatalogBody), nil }}
	digest := "#sha256-" + catalog.Digest([]byte(gitCatalogBody))
	bad := map[string]string{
		"plain http":         "git+http://example.com/o/r.git//c.yaml@v1" + digest,
		"file transport":     "git+file:///tmp/r.git//c.yaml@v1" + digest,
		"ext transport":      "git+ext::sh -c x//c.yaml@v1" + digest,
		"no path":            "git+https://example.com/o/r.git@v1" + digest,
		"no revision":        "git+https://example.com/o/r.git//c.yaml" + digest,
		"option as revision": "git+https://example.com/o/r.git//c.yaml@--upload-pack=x" + digest,
		"path escapes":       "git+https://example.com/o/r.git//../c.yaml@v1" + digest,
		"other fragment":     "git+https://example.com/o/r.git//c.yaml@v1#md5-00",
		"short commit":       "git+https://example.com/o/r.git//c.yaml@0123abc",
	}
	for name, ref := range bad {
		if _, err := l.Load(ref); err == nil {
			t.Errorf("%s: %s must be rejected", name, ref)
		}
	}
	if _, err := l.Load("git+https://example.com/o/r.git//c.yaml@v1.0.0"); !errors.Is(err, catalog.ErrGitPinRequired) {
		t.Fatalf("a tag without digest must ask for a pin, got %v", err)
	}
}

func TestGitCatalogPinning(t *testing.T) {
	t.Parallel()
	var gotURL, gotRev, gotPath string
	l := catalog.Loader{FetchGit: func(u, rev, path string) ([]byte, error) {
		gotURL, gotRev, gotPath = u, rev, path
		return []byte(gitCatalogBody), nil
	}}
	commit := strings.Repeat("a", 40)
	cats, err := l.Load("git+ssh://git@example.com/o/r.git//teams/a/c.yaml@" + commit)
	if err != nil || len(cats) != 1 || cats[0].Trusted {
		t.Fatalf("commit pin: cats=%v err=%v", cats, err)
	}
	if gotURL != "ssh://git@example.com/o/r.git" || gotRev != commit || gotPath != "teams/a/c.yaml" {
		t.Fatalf("parsed as url=%q rev=%q path=%q", gotURL, gotRev, gotPath)
	}
	tagged := "git+https://example.com/o/r.git//c.yaml@v1.0.0#sha256-"
	if _, err := l.Load(tagged + catalog.Digest([]byte(gitCatalogBody))); err != nil {
		t.Fatalf("tag with matching digest: %v", err)
	}
	if _, err := l.Load(tagged + strings.Repeat("0", 64)); err == nil {
		t.Fatal("a moved tag (digest mismatch) must be rejected")
	}
	if _, err := (catalog.Loader{}).Load(tagged + catalog.Digest([]byte(gitCatalogBody))); err == nil {
		t.Fatal("git catalogs must be refused when FetchGit is nil")
	}
}

func TestGitCatalogIncludes(t *testing.T) {
	t.Parallel()
	body := "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: team, version: 1.0.0, includes: [../local.yaml]}\nrules: []\n"
	l := catalog.Loader{FetchGit: func(string, string, string) ([]byte, error) { return []byte(body), nil }}
	if _, err := l.Load("git+https://example.com/o/r.git//c.yaml@" + strings.Repeat("b", 40)); err == nil {
		t.Fatal("a git catalog must not include local paths")
	}
}

func TestGitFetchReadsTagAndCommit(t *testing.T) {
	t.Parallel()
	src := testutil.GitFixture(t, map[string]string{"policy/catalog.yaml": gitCatalogBody}).Root
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", src}, args...)...)
		cmd.Env = testutil.GitEnv()
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("tag", "v1.0.0")
	commit := git("rev-parse", "HEAD")
	fetch := catalog.GitFetch(context.Background())

	for _, rev := range []string{"v1.0.0", commit} {
		b, err := fetch(src, rev, "policy/catalog.yaml")
		if err != nil || string(b) != gitCatalogBody {
			t.Fatalf("rev %s: body=%q err=%v", rev, b, err)
		}
	}
	if _, err := fetch(src, "main", "policy/catalog.yaml"); err == nil {
		t.Fatal("a branch name must not resolve: only tags and commits are fetched")
	}
	if _, err := fetch(src, "v1.0.0", "policy/missing.yaml"); err == nil || !strings.Contains(err.Error(), "policy/missing.yaml") {
		t.Fatalf("a missing file must be named in the error, got %v", err)
	}
}
