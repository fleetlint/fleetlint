package fetch

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fleetlint/fleetlint/internal/proc"
)

const (
	gitFetchTimeout = 2 * time.Minute
	// TemplatesDir is where a catalog keeps its fix templates.
	TemplatesDir = "templates/"
	// MaxFiles bounds the files read from one catalog source.
	MaxFiles = 512
)

var (
	commitRe = regexp.MustCompile(`^[0-9a-f]{40}$`)
	tagRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
)

// IsCommit reports whether rev is a full commit SHA.
func IsCommit(rev string) bool { return commitRe.MatchString(rev) }

// ValidRev reports whether rev is a tag or a full commit SHA that is safe to
// pass to git.
func ValidRev(rev string) bool { return tagRe.MatchString(rev) && !strings.Contains(rev, "..") }

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
		if n, err := strconv.Atoi(strings.TrimSpace(string(size))); err != nil || n > MaxCatalogLen {
			return nil, fmt.Errorf("%s: %s is larger than %d bytes", repoURL, path, MaxCatalogLen)
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
		if name == "" || (strings.Contains(name, "/") && !strings.HasPrefix(name, TemplatesDir)) {
			continue
		}
		if len(files) >= MaxFiles {
			return nil, fmt.Errorf("more than %d files under %q", MaxFiles, dir)
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
	if n, err := strconv.Atoi(strings.TrimSpace(string(size))); err != nil || n > MaxCatalogLen {
		return nil, fmt.Errorf("larger than %d bytes", MaxCatalogLen)
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
	cmd := proc.Command(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = proc.GitEnv()
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(errOut.String()))
	}
	return out, nil
}
