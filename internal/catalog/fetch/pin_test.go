package fetch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPinFetch(t *testing.T) {
	t.Parallel()
	repoDir, commit := gitRepo(t, catalogFiles)
	cacheRoot := t.TempDir()
	fetch := PinFetch(context.Background(), cacheRoot)
	dir, got, err := fetch(repoDir, "v1.0.0")
	if err != nil || got != commit || dir != CachePath(cacheRoot, repoDir, "v1.0.0") {
		t.Fatalf("dir=%s commit=%s err=%v", dir, got, err)
	}
	for _, name := range []string{"presets/p.yaml", "templates/SECURITY.md", "templates/sub/x.md", "rules/r.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, name := range []string{"catalog.yaml", "other/z.txt", "sub", "big"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s was cached", name)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, commitFile)); string(b) != commit+"\n" {
		t.Errorf("COMMIT: %q", b)
	}
	// A modified entry is fetched again: the stray file goes, the link too.
	stray := filepath.Join(dir, "presets", "stray.yaml")
	if err := os.WriteFile(stray, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fetch(repoDir, "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stray); err == nil {
		t.Error("a modified entry is replaced")
	}
	link := filepath.Join(dir, "presets", "link.yaml")
	if err := os.Symlink("p.yaml", link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fetch(repoDir, "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); err == nil {
		t.Error("an entry with a symlink is replaced")
	}
	// A complete entry is served without the repository.
	if err := os.RemoveAll(repoDir); err != nil {
		t.Fatal(err)
	}
	if _, got, err := fetch(repoDir, "v1.0.0"); err != nil || got != commit {
		t.Errorf("offline: %s %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rules", "r.yaml"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fetch(repoDir, "v1.0.0"); err == nil {
		t.Error("a changed entry without the repository fails")
	}
	for _, rev := range []string{"../x", "a..b", "-v", "v1;rm"} {
		if _, _, err := fetch(repoDir, rev); err == nil || !strings.Contains(err.Error(), "not a tag") {
			t.Errorf("%q: %v", rev, err)
		}
	}
}

func TestPinFetchNeedsPresets(t *testing.T) {
	t.Parallel()
	repoDir, _ := gitRepo(t, map[string]string{"catalog.yaml": "x\n", "templates/a.md": "a\n"})
	if _, _, err := PinFetch(context.Background(), t.TempDir())(repoDir, "v1.0.0"); err == nil || !strings.Contains(err.Error(), "no presets/") {
		t.Errorf("without presets: %v", err)
	}
	if _, _, err := PinFetch(context.Background(), t.TempDir())(t.TempDir(), "v1.0.0"); err == nil {
		t.Error("not a repository")
	}
}

func TestCacheDir(t *testing.T) {
	if got, err := cacheDir("/explicit"); err != nil || got != "/explicit" {
		t.Errorf("explicit: %s %v", got, err)
	}
	t.Setenv(CacheEnv, filepath.Join("/", "ci", "cache"))
	if got, err := cacheDir(""); err != nil || got != filepath.Join("/", "ci", "cache", "catalogs") {
		t.Errorf("from the environment: %s %v", got, err)
	}
	t.Setenv(CacheEnv, "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	got, err := cacheDir("")
	if err != nil || !strings.HasSuffix(got, filepath.Join("fleetlint", "catalogs")) {
		t.Errorf("the user's cache: %s %v", got, err)
	}
}

func TestCachePath(t *testing.T) {
	t.Parallel()
	a := CachePath("/root", "https://x/a/b", "v1")
	if a == CachePath("/root", "https://x/a_b", "v1") || a == CachePath("/root", "https://x/a/b", "v2") {
		t.Error("repository and version each change the path")
	}
	if !strings.HasPrefix(a, "/root/") || !strings.Contains(filepath.Base(a), "v1-") {
		t.Errorf("path: %s", a)
	}
	if base := filepath.Base(CachePath("/root", "r", "release/1")); !strings.HasPrefix(base, "release_1-") {
		t.Errorf("a slash in the version: %s", base)
	}
}

func TestWriteAndReadCacheEntry(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	files := map[string][]byte{"presets/p.yaml": []byte("p"), "templates/t.md": []byte("t"), "rules/r.yaml": []byte("r")}
	dest := filepath.Join(root, "entry")
	if err := WriteCacheEntry(dest, files, "abc"); err != nil {
		t.Fatal(err)
	}
	if got, err := readCacheEntry(dest); err != nil || got != "abc" {
		t.Errorf("read back: %s %v", got, err)
	}
	// Written again while another run already filled it: theirs is kept.
	if err := WriteCacheEntry(dest, files, "abc"); err != nil {
		t.Errorf("a second write of a complete entry: %v", err)
	}
	asFile := filepath.Join(root, "file")
	if err := os.WriteFile(asFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteCacheEntry(asFile, files, "abc"); err == nil || !strings.Contains(err.Error(), "store the catalog") {
		t.Errorf("a file in the way: %v", err)
	}
	if err := WriteCacheEntry(filepath.Join(root, "bad"), map[string][]byte{"../escape": []byte("x")}, "abc"); err == nil || !strings.Contains(err.Error(), "not a relative path") {
		t.Errorf("a path that leaves the entry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dest, "rules", "r.yaml"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCacheEntry(dest); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Errorf("a changed file: %v", err)
	}
	if err := os.Remove(filepath.Join(dest, digestFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := readCacheEntry(dest); err == nil {
		t.Error("no digest, no entry")
	}
	if _, err := readCacheEntry(filepath.Join(root, "missing")); err == nil {
		t.Error("a missing entry")
	}
	// An entry without any pinned directory is still complete.
	empty := filepath.Join(root, "empty")
	if err := WriteCacheEntry(empty, map[string][]byte{}, "def"); err != nil {
		t.Fatal(err)
	}
	if got, err := readCacheEntry(empty); err != nil || got != "def" {
		t.Errorf("empty entry: %s %v", got, err)
	}
}

func TestTreeDigest(t *testing.T) {
	t.Parallel()
	a := treeDigest(map[string][]byte{"x": []byte("1"), "y": []byte("2")})
	if a != treeDigest(map[string][]byte{"y": []byte("2"), "x": []byte("1")}) {
		t.Error("order does not matter")
	}
	if a == treeDigest(map[string][]byte{"x": []byte("1"), "y": []byte("3")}) || a == treeDigest(map[string][]byte{"x": []byte("1")}) {
		t.Error("content and names matter")
	}
}
