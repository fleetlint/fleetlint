package repo_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/repo"
	"github.com/fleetlint/fleetlint/internal/rootfs"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

func TestOpenErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := repo.Open(context.Background(), filepath.Join(dir, "missing")); err == nil {
		t.Error("a missing root must fail")
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Open(context.Background(), file); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("a file as root: %v", err)
	}
}

func TestNamesSizesAndDirectories(t *testing.T) {
	t.Parallel()
	r := testutil.Fixture(t, map[string]string{"a/b.txt": "hello", "c": "x"})
	if r.Name() != filepath.Base(r.Root) || r.ScopeRoot() != r.Root {
		t.Errorf("name %q, scope root %q", r.Name(), r.ScopeRoot())
	}
	sc, err := r.Scoped("a")
	if err != nil {
		t.Fatal(err)
	}
	if sc.ScopeRoot() != filepath.Join(r.Root, "a") || sc.Name() != r.Name() {
		t.Errorf("scoped root %q name %q", sc.ScopeRoot(), sc.Name())
	}
	if !r.IsDir("a") || r.IsDir("a/b.txt") || r.IsDir("zz") || r.IsDir("../x") {
		t.Error("IsDir")
	}
	for rel, want := range map[string]int64{"a/b.txt": 5, "a": 0, "missing": 0, "../x": 0} {
		if got := r.Size(rel); got != want {
			t.Errorf("Size(%s) = %d, want %d", rel, got, want)
		}
	}
	if sc.Size("b.txt") != 5 {
		t.Error("Size resolves relative to the scope")
	}
}

func TestWriteAndAppend(t *testing.T) {
	t.Parallel()
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := testutil.Fixture(t, map[string]string{"dir/keep": "k"})
	if err := os.Symlink(outside, filepath.Join(r.Root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := r.WriteFile("new/deep/f.txt", []byte("a"), true); err != nil {
		t.Fatal(err)
	}
	if err := r.WriteFile("new/deep/f.txt", []byte("b"), true); !errors.Is(err, os.ErrExist) {
		t.Errorf("create refuses an existing file: %v", err)
	}
	if err := r.WriteFile("new/deep/f.txt", []byte("b"), false); err != nil {
		t.Fatal(err)
	}
	if err := r.AppendFile("new/deep/f.txt", []byte("c")); err != nil {
		t.Fatal(err)
	}
	if err := r.AppendFile("fresh.txt", []byte("d")); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]string{"new/deep/f.txt": "bc", "fresh.txt": "d"} {
		if got, ok := r.Text(rel); !ok || got != want {
			t.Errorf("%s = %q (%v), want %q", rel, got, ok, want)
		}
	}
	if err := r.WriteFile(".git/hooks/pre-commit", []byte("x"), false); !errors.Is(err, repo.ErrGitDir) {
		t.Errorf("writes into .git: %v", err)
	}
	if err := r.WriteFile("../escape", []byte("x"), false); err == nil {
		t.Error("writes outside the repository must fail")
	}
	if err := r.WriteFile("link", []byte("x"), false); !errors.Is(err, rootfs.ErrNotRegular) {
		t.Errorf("a symlink is never written through: %v", err)
	}
	if err := r.WriteFile("dir", []byte("x"), false); !errors.Is(err, rootfs.ErrNotRegular) {
		t.Errorf("a directory is not a file: %v", err)
	}
	if b, err := os.ReadFile(outside); err != nil || string(b) != "s" {
		t.Errorf("the file outside is untouched: %q %v", b, err)
	}
	sc, err := r.Scoped("dir")
	if err != nil {
		t.Fatal(err)
	}
	if err := sc.WriteFile("scoped.txt", []byte("s"), true); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Text("dir/scoped.txt"); !ok {
		t.Error("a scoped write lands below the scope")
	}
}

func TestInGitDir(t *testing.T) {
	t.Parallel()
	for rel, want := range map[string]bool{".git/config": true, "a/.GIT/x": true, ".git": true, "a/.github/x": false, "git/x": false, "a.git": false} {
		if got := repo.InGitDir(rel); got != want {
			t.Errorf("InGitDir(%s) = %v", rel, got)
		}
	}
}

func TestGlobAndFilesWithoutGit(t *testing.T) {
	t.Parallel()
	r := testutil.Fixture(t, map[string]string{"a.go": "", "docs/x.md": "", "docs/sub/y.md": "", "node_modules/m.js": "", "build/out.bin": ""})
	if got := r.Files(); !reflect.DeepEqual(got, []string{"a.go", "docs/sub/y.md", "docs/x.md"}) {
		t.Errorf("files: %v", got)
	}
	if got := r.Glob("docs/*.md"); !reflect.DeepEqual(got, []string{"docs/x.md"}) {
		t.Errorf("glob: %v", got)
	}
	if got := r.Glob("**/*.md"); !reflect.DeepEqual(got, []string{"docs/sub/y.md", "docs/x.md"}) {
		t.Errorf("glob: %v", got)
	}
	if got := r.Glob("*.rs"); got != nil {
		t.Errorf("no match: %v", got)
	}
	sc, err := r.Scoped("docs")
	if err != nil {
		t.Fatal(err)
	}
	if got := sc.Files(); !reflect.DeepEqual(got, []string{"sub/y.md", "x.md"}) {
		t.Errorf("scoped files: %v", got)
	}
}

func TestFilesWithGit(t *testing.T) {
	t.Parallel()
	r := testutil.GitFixture(t, map[string]string{".gitignore": "ignored.txt\n", "tracked.txt": "t", "vendor/v.go": "v", "sub/s.txt": "s"})
	testutil.WriteFiles(t, r.Root, map[string]string{"ignored.txt": "i", "new.txt": "n"})
	want := []string{".gitignore", "new.txt", "sub/s.txt", "tracked.txt"}
	if got := r.Files(); !reflect.DeepEqual(got, want) {
		t.Errorf("files: %v", got)
	}
	if got := r.Files(); !reflect.DeepEqual(got, want) {
		t.Errorf("cached files: %v", got)
	}
	if got := r.Tracked(); !reflect.DeepEqual(got, []string{".gitignore", "sub/s.txt", "tracked.txt", "vendor/v.go"}) {
		t.Errorf("tracked: %v", got)
	}
	sc, err := r.Scoped("sub")
	if err != nil {
		t.Fatal(err)
	}
	if got := sc.Glob("*.txt"); !reflect.DeepEqual(got, []string{"s.txt"}) {
		t.Errorf("scoped glob: %v", got)
	}
}

func TestGitAccessors(t *testing.T) {
	t.Parallel()
	r := testutil.GitFixture(t, map[string]string{"README.md": "x"})
	gitRun(t, r.Root, "remote", "add", "origin", "https://example.com/acme/x.git")
	gitRun(t, r.Root, "tag", "v1.0.0")
	gitRun(t, r.Root, "tag", "v1.10.0")
	gitRun(t, r.Root, "tag", "-a", "-m", "plain", "v1.2.0")
	fakeSignedTag(t, r.Root, "v0.1.0")
	if got := r.RemoteURL(); got != "https://example.com/acme/x.git" {
		t.Errorf("remote: %q", got)
	}
	if got := r.GitConfig("nope.key"); got != "" {
		t.Errorf("unset config: %q", got)
	}
	if got := r.Tags(); !reflect.DeepEqual(got, []string{"v1.10.0", "v1.2.0", "v1.0.0", "v0.1.0"}) {
		t.Errorf("tags: %v", got)
	}
	for name, want := range map[string]bool{"v0.1.0": true, "v1.0.0": false, "v1.2.0": false, "missing": false, "": false, "-v": false} {
		if got := r.TagSigned(name); got != want {
			t.Errorf("TagSigned(%q) = %v", name, got)
		}
	}
}

// A .git directory that is not a repository: git is there but every
// command fails, and the accessors report that instead of guessing.
func TestBrokenGit(t *testing.T) {
	t.Parallel()
	r := testutil.Fixture(t, map[string]string{".git/placeholder": "", "a.txt": "a", "sub/b.txt": "b"})
	if !r.HasGit() {
		t.Fatal("a .git directory counts as git")
	}
	if _, err := r.Git("status"); err == nil {
		t.Error("git must fail")
	}
	if files, err := r.TrackedFiles(); err == nil || files != nil {
		t.Errorf("tracked: %v %v", files, err)
	}
	if got := r.Tracked(); got != nil {
		t.Errorf("tracked without the error: %v", got)
	}
	if got := r.Files(); !reflect.DeepEqual(got, []string{"a.txt", "sub/b.txt"}) {
		t.Errorf("the index falls back to a walk: %v", got)
	}
	if r.GitConfig("fleetlint.unset") != "" || r.RemoteURL() != "" || r.Tags() != nil || r.TagSigned("v1") {
		t.Error("git-backed accessors are empty when git fails")
	}
	plain := testutil.Fixture(t, map[string]string{"a.txt": "a"})
	if _, err := plain.Git("status"); !errors.Is(err, repo.ErrNotARepo) {
		t.Errorf("without .git: %v", err)
	}
}

func TestTextErrTellsUnreadableFromAbsent(t *testing.T) {
	t.Parallel()
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := testutil.Fixture(t, map[string]string{"ok.txt": "fine"})
	for link, target := range map[string]string{"out.txt": outside, "dangling.txt": "nowhere"} {
		if err := os.Symlink(target, filepath.Join(r.Root, link)); err != nil {
			t.Fatal(err)
		}
	}
	if text, ok, err := r.TextErr("ok.txt"); !ok || err != nil || text != "fine" {
		t.Errorf("ok.txt: %q %v %v", text, ok, err)
	}
	if text, ok, err := r.TextErr("ok.txt"); !ok || err != nil || text != "fine" {
		t.Errorf("cached ok.txt: %q %v %v", text, ok, err)
	}
	if _, ok, err := r.TextErr("missing.txt"); ok || err != nil {
		t.Errorf("missing: %v %v", ok, err)
	}
	for _, link := range []string{"out.txt", "dangling.txt"} {
		if _, ok, err := r.TextErr(link); !ok || err == nil {
			t.Errorf("%s: ok=%v err=%v, want an error", link, ok, err)
		}
	}
}

func TestDocFormatsAndErrors(t *testing.T) {
	t.Parallel()
	r := testutil.Fixture(t, map[string]string{"bad.yaml": "a: [\n", "x.toml": "[tool]\nn = 1\n", "x.json": "{\"a\": 1}"})
	if _, ok, err := r.Doc(repo.FormatYAML, "bad.yaml"); !ok || err == nil {
		t.Errorf("broken yaml: ok=%v err=%v", ok, err)
	}
	v, ok, err := r.Doc(repo.FormatTOML, "x.toml")
	if !ok || err != nil || !reflect.DeepEqual(v, map[string]any{"tool": map[string]any{"n": int64(1)}}) {
		t.Errorf("toml: %#v %v %v", v, ok, err)
	}
	if again, _, _ := r.Doc(repo.FormatTOML, "x.toml"); !reflect.DeepEqual(again, v) {
		t.Error("documents are cached")
	}
	if _, ok, err := r.Doc(repo.FormatTOML, "x.json"); !ok || err == nil {
		t.Errorf("json as toml: ok=%v err=%v", ok, err)
	}
	if _, ok, err := r.Doc(repo.Format("xml"), "x.json"); !ok || err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("unsupported format: ok=%v err=%v", ok, err)
	}
	if v, ok, err := r.Doc(repo.FormatJSON, "missing.json"); v != nil || ok || err != nil {
		t.Errorf("missing: %v %v %v", v, ok, err)
	}
	if mf, ok := r.Makefile("missing"); mf != nil || ok {
		t.Error("a missing Makefile is absent")
	}
	if got := repo.Normalize(map[string]any{"n": 1, "f": 1.5}); !reflect.DeepEqual(got, map[string]any{"n": int64(1), "f": 1.5}) {
		t.Errorf("Normalize: %#v", got)
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

// fakeSignedTag writes an annotated tag whose message carries a signature
// block, the way `git tag -s` does; nothing verifies it.
func fakeSignedTag(t *testing.T, dir, name string) {
	t.Helper()
	head := exec.CommandContext(context.Background(), "git", "-C", dir, "rev-parse", "HEAD")
	head.Env = testutil.GitEnv()
	sha, err := head.Output()
	if err != nil {
		t.Fatal(err)
	}
	body := "object " + strings.TrimSpace(string(sha)) + "\ntype commit\ntag " + name + "\ntagger t <t@t> 0 +0000\n\n" + name +
		"\n-----BEGIN PGP SIGNATURE-----\nabc=\n-----END PGP SIGNATURE-----\n"
	mk := exec.CommandContext(context.Background(), "git", "-C", dir, "mktag")
	mk.Env = testutil.GitEnv()
	mk.Stdin = strings.NewReader(body)
	obj, err := mk.Output()
	if err != nil {
		t.Fatalf("mktag: %v", err)
	}
	gitRun(t, dir, "update-ref", "refs/tags/"+name, strings.TrimSpace(string(obj)))
}
