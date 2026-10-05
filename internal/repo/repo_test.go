package repo_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fleetlint/fleetlint/internal/repo"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

func TestAbsStaysInsideRepository(t *testing.T) {
	t.Parallel()
	r := testutil.Fixture(t, map[string]string{"a/b.txt": "x"})
	for _, rel := range []string{"..", "../x", "a/../../x", "/etc/passwd"} {
		if _, err := r.Abs(rel); err == nil {
			t.Errorf("Abs(%q) must refuse to leave the repository", rel)
		}
	}
	if abs, err := r.Abs("a/../a/b.txt"); err != nil || abs != filepath.Join(r.Root, "a", "b.txt") {
		t.Errorf("Abs should clean paths that stay inside: %q %v", abs, err)
	}
	if _, ok := r.Text("../outside"); ok {
		t.Error("Text must not read outside the repository")
	}
}

func TestScopedRejectsEscapes(t *testing.T) {
	t.Parallel()
	r := testutil.Fixture(t, map[string]string{"api/go.mod": "module a\n", "top.txt": "x"})
	for _, sub := range []string{"..", "../other", "/abs", "api/../.."} {
		if _, err := r.Scoped(sub); err == nil {
			t.Errorf("Scoped(%q) must fail", sub)
		}
	}
	sc, err := r.Scoped("api")
	if err != nil {
		t.Fatal(err)
	}
	if !sc.Has("go.mod") || sc.Has("top.txt") {
		t.Error("scoped accessors resolve relative to the scope")
	}
	// a scope may look upward but never past the root
	if _, ok := sc.Text("../top.txt"); !ok {
		t.Error("a scope may read files of the parent inside the repository")
	}
	if _, ok := sc.Text("../../top.txt"); ok {
		t.Error("a scope must not read past the repository root")
	}
}

func TestScopedViewsShareGitStateSafely(t *testing.T) {
	t.Parallel()
	r := testutil.GitFixture(t, map[string]string{"api/go.mod": "module a\n", "web/package.json": "{}", "README.md": "x"}, "user.name", "fixture")
	var wg sync.WaitGroup
	for _, sub := range []string{"", "api", "web", "api", "web", ""} {
		sc, err := r.Scoped(sub)
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if len(sc.Tracked()) != 3 {
				t.Errorf("tracked files must be shared: %v", sc.Tracked())
			}
			if sc.GitConfig("user.name") != "fixture" {
				t.Error("git config must be shared across scopes")
			}
		}()
	}
	wg.Wait()
	api, _ := r.Scoped("api")
	if got := api.TrackedInScope(); len(got) != 1 || got[0] != "go.mod" {
		t.Errorf("TrackedInScope: %v", got)
	}
}

func TestTextSizeCap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	big := filepath.Join(dir, "big.bin")
	if err := os.WriteFile(big, make([]byte, repo.MaxTextBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := repo.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Text("big.bin"); ok {
		t.Error("files over the cap read as absent")
	}
}

func TestTrackedWithoutGitWalksTree(t *testing.T) {
	t.Parallel()
	r := testutil.Fixture(t, map[string]string{"a.txt": "x", "node_modules/m/i.js": "x", "sub/b.txt": "y"})
	got := strings.Join(r.Tracked(), ",")
	if got != "a.txt,sub/b.txt" {
		t.Errorf("walk should skip vendored dirs: %q", got)
	}
}
