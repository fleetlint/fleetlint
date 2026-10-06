// Package repo gives rules a safe, cached view of one repository: its files,
// its git metadata and parsed configuration documents. Rules never touch the
// filesystem or run git themselves; everything goes through a Repo so the
// same data is read once and so tests can use fixture directories.
package repo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrNotARepo is returned when the root has no .git directory.
var ErrNotARepo = errors.New("not a git repository")

const (
	gitTimeout = 20 * time.Second
	// MaxTextBytes bounds files read into memory by rules; larger files read as absent.
	MaxTextBytes = 4 << 20
)

// Repo is a read-only, memoized view of a repository rooted at Root.
type Repo struct {
	// ctx bounds git subprocesses; set by Open from the caller's context.
	ctx  context.Context
	Root string
	// Scope is the sub-path rules are evaluated under ("" for the repo root).
	Scope string

	// git is shared by every scoped view of the same checkout.
	git *gitState

	mu    sync.Mutex
	docs  map[string]any
	texts map[string]string
}

// gitState caches git answers for one checkout under a single mutex, so
// scoped views created concurrently never race on the shared maps.
type gitState struct {
	mu      sync.Mutex
	tracked []string
	trackOK bool
	cfg     map[string]string
	hasGit  *bool
}

// Open creates a Repo for root. root must exist; it need not be a git
// repository, but git-dependent accessors then return empty results.
func Open(ctx context.Context, root string) (*Repo, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", root, err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("open repository: %w", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("open repository: %s is not a directory", abs)
	}
	return &Repo{
		ctx:   ctx,
		Root:  abs,
		git:   &gitState{cfg: map[string]string{}},
		docs:  map[string]any{},
		texts: map[string]string{},
	}, nil
}

// Scoped returns a view of the same repository with rules evaluated under sub,
// which must be a relative path inside the repository. File paths given to
// accessors are resolved relative to the scope; git data is shared with the parent.
func (r *Repo) Scoped(sub string) (*Repo, error) {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(sub)))
	if clean == "." {
		clean = ""
	}
	if filepath.IsAbs(sub) || clean == ".." || strings.HasPrefix(clean, "../") {
		return nil, fmt.Errorf("scope %q escapes the repository", sub)
	}
	return &Repo{
		ctx:   r.ctx,
		Root:  r.Root,
		Scope: clean,
		git:   r.git,
		docs:  map[string]any{},
		texts: map[string]string{},
	}, nil
}

// Name is the repository directory name.
func (r *Repo) Name() string { return filepath.Base(r.Root) }

// ScopeRoot is the absolute path rules see as their root.
func (r *Repo) ScopeRoot() string {
	if r.Scope == "" {
		return r.Root
	}
	return filepath.Join(r.Root, filepath.FromSlash(r.Scope))
}

// Abs resolves a scope-relative path and refuses to leave the repository.
func (r *Repo) Abs(rel string) (string, error) {
	rel = filepath.FromSlash(rel)
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q must be relative to the repository", rel)
	}
	abs := filepath.Join(r.ScopeRoot(), rel)
	relToRoot, err := filepath.Rel(r.Root, abs)
	if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the repository", rel)
	}
	return abs, nil
}

// Has reports whether a regular file or directory exists at rel.
func (r *Repo) Has(rel string) bool {
	abs, err := r.Abs(rel)
	if err != nil {
		return false
	}
	_, err = os.Stat(abs)
	return err == nil
}

// IsDir reports whether rel exists and is a directory.
func (r *Repo) IsDir(rel string) bool {
	abs, err := r.Abs(rel)
	if err != nil {
		return false
	}
	st, err := os.Stat(abs)
	return err == nil && st.IsDir()
}

// Text returns the contents of a file, cached. Missing files return "" and
// ok=false rather than an error, because "file absent" is a normal outcome
// for a rule.
func (r *Repo) Text(rel string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.texts[rel]; ok {
		return t, true
	}
	abs, err := r.Abs(rel)
	if err != nil {
		return "", false
	}
	if st, err := os.Stat(abs); err != nil || st.Size() > MaxTextBytes {
		return "", false
	}
	b, err := os.ReadFile(abs) //nolint:gosec // abs is confined to the repository by Abs
	if err != nil {
		return "", false
	}
	s := string(b)
	r.texts[rel] = s
	return s, true
}

// Glob returns scope-relative paths matching a doublestar-style pattern,
// walking the working tree and skipping .git and common vendored directories.
func (r *Repo) Glob(pattern string) []string {
	var out []string
	root, err := r.Abs(".")
	if err != nil {
		return nil
	}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // unreadable entries are skipped, not fatal
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if MatchGlob(pattern, rel) {
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "target", "build", "dist", ".dart_tool", ".gradle", ".venv", "__pycache__":
		return true
	}
	return false
}

// HasGit reports whether the repository root contains a git checkout.
func (r *Repo) HasGit() bool {
	g := r.git
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.hasGit != nil {
		return *g.hasGit
	}
	_, err := os.Stat(filepath.Join(r.Root, ".git"))
	v := err == nil
	g.hasGit = &v
	return v
}

// Git runs a git command in the repository root and returns trimmed stdout.
func (r *Repo) Git(args ...string) (string, error) {
	if !r.HasGit() {
		return "", ErrNotARepo
	}
	ctx, cancel := context.WithTimeout(r.ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", r.Root}, args...)...) //nolint:gosec // callers pass fixed git verbs; the repository root is the only variable
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Tracked returns all git-tracked paths relative to the repository root.
// Without git it falls back to walking the working tree.
func (r *Repo) Tracked() []string {
	g := r.git
	g.mu.Lock()
	if g.trackOK {
		defer g.mu.Unlock()
		return g.tracked
	}
	g.mu.Unlock()

	var files []string
	if out, err := r.Git("ls-files", "-z"); err == nil {
		for _, f := range strings.Split(out, "\x00") {
			if f != "" {
				files = append(files, f)
			}
		}
	} else if rootView, serr := r.Scoped(""); serr == nil {
		files = rootView.Glob("**")
	}
	sort.Strings(files)

	g.mu.Lock()
	g.tracked, g.trackOK = files, true
	g.mu.Unlock()
	return files
}

// TrackedInScope returns tracked paths under the current scope, scope-relative.
func (r *Repo) TrackedInScope() []string {
	all := r.Tracked()
	if r.Scope == "" {
		return all
	}
	prefix := r.Scope + "/"
	var out []string
	for _, f := range all {
		if strings.HasPrefix(f, prefix) {
			out = append(out, strings.TrimPrefix(f, prefix))
		}
	}
	return out
}

// GitConfig returns a git config value or "" when unset.
func (r *Repo) GitConfig(key string) string {
	g := r.git
	g.mu.Lock()
	if v, ok := g.cfg[key]; ok {
		g.mu.Unlock()
		return v
	}
	g.mu.Unlock()
	v, err := r.Git("config", "--get", key)
	if err != nil {
		v = ""
	}
	g.mu.Lock()
	g.cfg[key] = v
	g.mu.Unlock()
	return v
}

// RemoteURL returns the URL of the origin remote, or "".
func (r *Repo) RemoteURL() string { return r.GitConfig("remote.origin.url") }

// Tags returns tag names sorted by version, newest first.
func (r *Repo) Tags() []string {
	out, err := r.Git("tag", "--sort=-v:refname")
	if err != nil || out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// TagSigned reports whether a tag is an annotated tag with a PGP, SSH or
// X.509 signature block. It does not verify the signature.
func (r *Repo) TagSigned(name string) bool {
	if name == "" || strings.HasPrefix(name, "-") {
		return false
	}
	out, err := r.Git("cat-file", "-p", "refs/tags/"+name)
	if err != nil {
		return false
	}
	for _, marker := range []string{"-----BEGIN PGP SIGNATURE-----", "-----BEGIN SSH SIGNATURE-----", "-----BEGIN SIGNED MESSAGE-----"} {
		if strings.Contains(out, marker) {
			return true
		}
	}
	return false
}

// Size returns a file's size in bytes, or 0 when it is absent, a directory,
// or outside the repository.
func (r *Repo) Size(rel string) int64 {
	abs, err := r.Abs(rel)
	if err != nil {
		return 0
	}
	st, err := os.Stat(abs)
	if err != nil || st.IsDir() {
		return 0
	}
	return st.Size()
}
