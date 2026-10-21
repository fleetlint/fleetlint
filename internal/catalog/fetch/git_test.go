package fetch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// gitRepo builds a repository with the files, one commit and the tag
// v1.0.0, and returns its path and the commit.
func gitRepo(t *testing.T, files map[string]string) (dir, commit string) {
	t.Helper()
	dir = t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "chore: fixture", "--no-verify")
	gitRun(t, dir, "tag", "v1.0.0")
	return dir, strings.TrimSpace(gitRun(t, dir, "rev-parse", "HEAD"))
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// catalogFiles is a catalog repository: the catalog and its templates at
// the root and in sub/, the pinned directories, and things to leave out.
var catalogFiles = map[string]string{
	"catalog.yaml":          "apiVersion: fleetlint.org/v1\n",
	"templates/SECURITY.md": "# security\n",
	"templates/sub/x.md":    "x\n",
	"other/z.txt":           "z\n",
	"presets/p.yaml":        "preset\n",
	"rules/r.yaml":          "rule\n",
	"sub/catalog.yaml":      "sub catalog\n",
	"sub/templates/t.md":    "t\n",
	"sub/deep/ignored.txt":  "ignored\n",
	"big/big.bin":           strings.Repeat("b", MaxCatalogLen+1),
}

func TestRevValidation(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("ab", 20)
	for rev, want := range map[string]bool{sha: true, "v1.2.3": true, "release/1": true, "a..b": false, "-x": false, "": false, "v 1": false, "x;y": false} {
		if got := ValidRev(rev); got != want {
			t.Errorf("ValidRev(%q) = %v", rev, got)
		}
	}
	for rev, want := range map[string]bool{sha: true, "v1.2.3": false, sha[:39]: false, strings.ToUpper(sha): false} {
		if got := IsCommit(rev); got != want {
			t.Errorf("IsCommit(%q) = %v", rev, got)
		}
	}
	if treeObject("") != "FETCH_HEAD" || treeObject(".") != "FETCH_HEAD" || treeObject("sub") != "FETCH_HEAD:sub" {
		t.Error("treeObject")
	}
}

func TestGitFetch(t *testing.T) {
	t.Parallel()
	dir, commit := gitRepo(t, catalogFiles)
	fetch := GitFetch(context.Background())
	if b, err := fetch(dir, "v1.0.0", "catalog.yaml"); err != nil || string(b) != catalogFiles["catalog.yaml"] {
		t.Errorf("at a tag: %q %v", b, err)
	}
	if b, err := fetch(dir, commit, "templates/SECURITY.md"); err != nil || string(b) != catalogFiles["templates/SECURITY.md"] {
		t.Errorf("at a commit: %q %v", b, err)
	}
	cases := map[string]struct{ rev, path, wantErr string }{
		"missing file":  {"v1.0.0", "nope.yaml", "has no file"},
		"too large":     {"v1.0.0", "big/big.bin", "larger than"},
		"a branch":      {"main", "catalog.yaml", "git fetch"},
		"unknown sha":   {strings.Repeat("0", 40), "catalog.yaml", "git fetch"},
		"a directory":   {"v1.0.0", "templates", "git cat-file"},
		"a missing tag": {"v9.9.9", "catalog.yaml", "git fetch"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := fetch(dir, c.rev, c.path); err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("want %q, got %v", c.wantErr, err)
			}
		})
	}
	if _, err := fetch(t.TempDir(), "v1.0.0", "catalog.yaml"); err == nil {
		t.Error("not a repository")
	}
}

func TestGitTreeFetch(t *testing.T) {
	t.Parallel()
	dir, commit := gitRepo(t, catalogFiles)
	fetch := GitTreeFetch(context.Background())
	root, err := fetch(dir, "v1.0.0", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := keys(root); got != "catalog.yaml,templates/SECURITY.md,templates/sub/x.md" {
		t.Errorf("root: %s", got)
	}
	if string(root["templates/sub/x.md"]) != "x\n" {
		t.Errorf("content: %q", root["templates/sub/x.md"])
	}
	sub, err := fetch(dir, commit, "sub")
	if err != nil {
		t.Fatal(err)
	}
	if got := keys(sub); got != "catalog.yaml,templates/t.md" {
		t.Errorf("sub: %s", got)
	}
	if _, err := fetch(dir, "v1.0.0", "nope"); err == nil || !strings.Contains(err.Error(), "no directory") {
		t.Errorf("missing directory: %v", err)
	}
	if _, err := fetch(dir, "v1.0.0", "big"); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("oversized file: %v", err)
	}
	if _, err := fetch(dir, "main", ""); err == nil {
		t.Error("branches are never fetched")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := GitTreeFetch(ctx)(dir, "v1.0.0", ""); err == nil {
		t.Error("a cancelled context stops the fetch")
	}
}

func TestTooManyFiles(t *testing.T) {
	t.Parallel()
	files := map[string]string{}
	for i := range MaxFiles + 1 {
		files[fmt.Sprintf("presets/f%04d.yaml", i)] = "x"
	}
	dir, _ := gitRepo(t, files)
	// Each reads the files up to the limit, two git calls per file.
	t.Run("tree", func(t *testing.T) {
		t.Parallel()
		if _, err := GitTreeFetch(context.Background())(dir, "v1.0.0", "presets"); err == nil || !strings.Contains(err.Error(), "more than") {
			t.Errorf("tree: %v", err)
		}
	})
	t.Run("pin", func(t *testing.T) {
		t.Parallel()
		if _, _, err := PinFetch(context.Background(), t.TempDir())(dir, "v1.0.0"); err == nil || !strings.Contains(err.Error(), "more than") {
			t.Errorf("pin: %v", err)
		}
	})
}

// Each file stays under the per-file limit; together they pass the tree limit.
func TestTreeTooLarge(t *testing.T) {
	t.Parallel()
	files := map[string]string{"catalog.yaml": "apiVersion: fleetlint.org/v1\n"}
	for i := range MaxTreeLen/MaxCatalogLen + 1 {
		files[fmt.Sprintf("templates/big%d.bin", i)] = strings.Repeat(string(rune('a'+i)), MaxCatalogLen)
	}
	dir, _ := gitRepo(t, files)
	want := fmt.Sprintf("larger than %d bytes", MaxTreeLen)
	if _, err := GitTreeFetch(context.Background())(dir, "v1.0.0", ""); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("want %q, got %v", want, err)
	}
}

func keys(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}
