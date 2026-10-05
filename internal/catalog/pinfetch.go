package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// commitFile records, inside a cached catalog, the commit it was taken from.
const commitFile = "COMMIT"

// PinFetch returns a FetchCatalog function for Loader. A catalog version
// is fetched once with git (shallow, with the credentials git already has)
// into cacheRoot and read from there afterwards, so a pinned repository
// works offline after its first run. An empty cacheRoot is the user's cache
// directory.
func PinFetch(ctx context.Context, cacheRoot string) func(repoURL, rev string) (dir, commit string, err error) {
	return func(repoURL, rev string) (string, string, error) {
		if !tagRe.MatchString(rev) || strings.Contains(rev, "..") {
			return "", "", fmt.Errorf("version %q is not a tag or a full commit SHA", rev)
		}
		root, err := cacheDir(cacheRoot)
		if err != nil {
			return "", "", err
		}
		dir := CachePath(root, repoURL, rev)
		if b, err := os.ReadFile(filepath.Join(dir, commitFile)); err == nil { //nolint:gosec // a file fleetlint wrote in its own cache
			return dir, strings.TrimSpace(string(b)), nil
		}
		commit, err := fetchCatalogTree(ctx, repoURL, rev, dir)
		return dir, commit, err
	}
}

// CacheEnv names a directory for pinned catalogs instead of the user's cache
// directory; CI uses it to keep the cache between runs.
const CacheEnv = "FLEETLINT_CACHE_DIR"

func cacheDir(root string) (string, error) {
	if root != "" {
		return root, nil
	}
	if env := os.Getenv(CacheEnv); env != "" {
		return filepath.Join(env, "catalogs"), nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("no cache directory for pinned catalogs (set %s): %w", CacheEnv, err)
	}
	return filepath.Join(base, "fleetlint", "catalogs"), nil
}

// CachePath is where one version of one catalog repository is kept.
func CachePath(root, repoURL, rev string) string {
	sum := sha256.Sum256([]byte(repoURL))
	return filepath.Join(root, hex.EncodeToString(sum[:8]), strings.ReplaceAll(rev, "/", "_"))
}

// fetchCatalogTree checks presets/ and templates/ of one revision out into
// dest. It builds the directory next to dest and renames it into place, so
// an interrupted fetch never leaves a half-filled cache entry.
func fetchCatalogTree(ctx context.Context, repoURL, rev, dest string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitFetchTimeout)
	defer cancel()
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dest), ".fetch-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp) //nolint:errcheck // best-effort cleanup; after a successful rename there is nothing left
	if _, err := gitOut(ctx, tmp, "init", "-q"); err != nil {
		return "", err
	}
	refspec := rev
	if !commitRe.MatchString(rev) {
		refspec = "refs/tags/" + rev
	}
	if _, err := gitOut(ctx, tmp, "fetch", "-q", "--depth=1", "--no-tags", "--", repoURL, refspec); err != nil {
		return "", err
	}
	out, err := gitOut(ctx, tmp, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	if err != nil {
		return "", err
	}
	commit := strings.TrimSpace(string(out))
	if commitRe.MatchString(rev) && commit != rev {
		return "", fmt.Errorf("fetched %s instead of commit %s", commit, rev)
	}
	if _, err := gitOut(ctx, tmp, "checkout", "-q", "FETCH_HEAD", "--", "presets", "templates"); err != nil {
		return "", fmt.Errorf("the repository has no presets/ and templates/ at %s: %w", rev, err)
	}
	if err := os.RemoveAll(filepath.Join(tmp, ".git")); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmp, commitFile), []byte(commit+"\n"), 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		// Another run filled the entry first: use theirs.
		if _, statErr := os.Stat(filepath.Join(dest, commitFile)); statErr == nil {
			return commit, nil
		}
		return "", errors.Join(fmt.Errorf("store the catalog in the cache"), err)
	}
	return commit, nil
}
