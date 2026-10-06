package catalog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	gitPrefix       = "git+"
	gitFetchTimeout = 2 * time.Minute
)

// ErrGitPinRequired is returned for a git reference whose revision can move.
var ErrGitPinRequired = errors.New("git catalogs at a tag must be pinned with #sha256-<digest>; a full commit SHA needs no digest")

var (
	commitRe = regexp.MustCompile(`^[0-9a-f]{40}$`)
	tagRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
)

// gitRef is a parsed `git+<url>//<path>@<rev>[#sha256-<digest>]` reference.
type gitRef struct {
	URL, Path, Rev, Digest string
}

func (g gitRef) isCommit() bool { return commitRe.MatchString(g.Rev) }

// parseGitRef splits a git catalog reference. The repository URL must use
// https or ssh, the revision is a tag or a full commit SHA, and the path is
// relative to the repository root.
func parseGitRef(ref string) (gitRef, error) {
	rest, digest, pinned := strings.Cut(strings.TrimPrefix(ref, gitPrefix), "#")
	if pinned && !strings.HasPrefix(digest, "sha256-") {
		return gitRef{}, fmt.Errorf("%s: the fragment must be sha256-<digest>", ref)
	}
	scheme, tail, ok := strings.Cut(rest, "://")
	if !ok || (scheme != "https" && scheme != "ssh") {
		return gitRef{}, fmt.Errorf("%s: git catalogs are read over https or ssh", ref)
	}
	repo, pathRev, ok := strings.Cut(tail, "//")
	if !ok || repo == "" {
		return gitRef{}, fmt.Errorf("%s: want git+<url>//<path>@<tag or commit>", ref)
	}
	at := strings.LastIndex(pathRev, "@")
	if at <= 0 || at == len(pathRev)-1 {
		return gitRef{}, fmt.Errorf("%s: want git+<url>//<path>@<tag or commit>", ref)
	}
	g := gitRef{URL: scheme + "://" + repo, Path: pathRev[:at], Rev: pathRev[at+1:], Digest: strings.TrimPrefix(digest, "sha256-")}
	if err := g.validate(); err != nil {
		return gitRef{}, fmt.Errorf("%s: %w", ref, err)
	}
	return g, nil
}

func (g gitRef) validate() error {
	if !tagRe.MatchString(g.Rev) || strings.Contains(g.Rev, "..") {
		return fmt.Errorf("revision %q is not a tag or a full commit SHA", g.Rev)
	}
	for _, seg := range strings.Split(g.Path, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("path %q must be relative to the repository root", g.Path)
		}
	}
	if !g.isCommit() && g.Digest == "" {
		return ErrGitPinRequired
	}
	return nil
}

// GitFetch returns a FetchGit function for Loader. It fetches the one
// revision into a temporary bare repository with the credentials git already
// has (ssh agent, credential helper) and reads the file from it. A revision
// that is not a full commit SHA is fetched as a tag, never as a branch.
func GitFetch(ctx context.Context) func(repoURL, rev, path string) ([]byte, error) {
	return func(repoURL, rev, path string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, gitFetchTimeout)
		defer cancel()
		dir, err := os.MkdirTemp("", "fleetlint-catalog-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(dir) //nolint:errcheck // best-effort cleanup of a temp dir

		if err := fetchRev(ctx, dir, repoURL, rev); err != nil {
			return nil, err
		}
		object := "FETCH_HEAD:" + path
		size, err := gitOut(ctx, dir, "cat-file", "-s", object)
		if err != nil {
			return nil, fmt.Errorf("%s at %s has no file %s: %w", repoURL, rev, path, err)
		}
		if n, err := strconv.Atoi(strings.TrimSpace(string(size))); err != nil || n > maxCatalogLen {
			return nil, fmt.Errorf("%s: %s is larger than %d bytes", repoURL, path, maxCatalogLen)
		}
		return gitOut(ctx, dir, "cat-file", "blob", object)
	}
}

// GitTreeFetch returns a FetchGitTree function for Loader: the files of one
// directory at a tag or commit, by path relative to that directory. It
// serves the catalog file and the templates/ tree next to it in one fetch.
func GitTreeFetch(ctx context.Context) func(repoURL, rev, dir string) (map[string][]byte, error) {
	return func(repoURL, rev, dir string) (map[string][]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, gitFetchTimeout)
		defer cancel()
		tmp, err := os.MkdirTemp("", "fleetlint-catalog-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tmp) //nolint:errcheck // best-effort cleanup of a temp dir
		if err := fetchRev(ctx, tmp, repoURL, rev); err != nil {
			return nil, err
		}
		files, err := readTree(ctx, tmp, dir)
		if err != nil {
			return nil, fmt.Errorf("%s at %s: %w", repoURL, rev, err)
		}
		return files, nil
	}
}

// readTree returns the files of dir in the fetched revision: its own files
// and the templates/ tree below it, nothing else.
func readTree(ctx context.Context, tmp, dir string) (map[string][]byte, error) {
	listing, err := gitOut(ctx, tmp, "ls-tree", "-r", "-z", "--name-only", treeObject(dir))
	if err != nil {
		return nil, fmt.Errorf("no directory %q: %w", dir, err)
	}
	files := map[string][]byte{}
	for _, name := range strings.Split(strings.TrimRight(string(listing), "\x00"), "\x00") {
		if name == "" || (strings.Contains(name, "/") && !strings.HasPrefix(name, templatesDir)) {
			continue
		}
		if len(files) >= ociMaxFiles {
			return nil, fmt.Errorf("more than %d files under %q", ociMaxFiles, dir)
		}
		if files[name], err = readBlob(ctx, tmp, treeObject(path.Join(dir, name))); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	return files, nil
}

// treeObject names a path of the fetched revision; "" is the root tree.
func treeObject(p string) string {
	if p == "" || p == "." {
		return "FETCH_HEAD"
	}
	return "FETCH_HEAD:" + p
}

// readBlob returns a blob of the fetched revision, refusing oversized ones.
func readBlob(ctx context.Context, dir, object string) ([]byte, error) {
	size, err := gitOut(ctx, dir, "cat-file", "-s", object)
	if err != nil {
		return nil, err
	}
	if n, err := strconv.Atoi(strings.TrimSpace(string(size))); err != nil || n > maxCatalogLen {
		return nil, fmt.Errorf("larger than %d bytes", maxCatalogLen)
	}
	return gitOut(ctx, dir, "cat-file", "blob", object)
}

// fetchRev fetches one tag or commit into the bare repository at dir and
// leaves it in FETCH_HEAD.
func fetchRev(ctx context.Context, dir, repoURL, rev string) error {
	refspec := rev
	if !commitRe.MatchString(rev) {
		refspec = "refs/tags/" + rev
	}
	if _, err := gitOut(ctx, dir, "init", "-q", "--bare"); err != nil {
		return err
	}
	if _, err := gitOut(ctx, dir, "fetch", "-q", "--depth=1", "--no-tags", "--", repoURL, refspec); err != nil {
		return err
	}
	if !commitRe.MatchString(rev) {
		return nil
	}
	got, err := gitOut(ctx, dir, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(got)) != rev {
		return fmt.Errorf("%s: fetched %s instead of commit %s", repoURL, strings.TrimSpace(string(got)), rev)
	}
	return nil
}

func gitOut(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // the binary is fixed; arguments are validated by parseGitRef and passed without a shell
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(errOut.String()))
	}
	return out, nil
}
