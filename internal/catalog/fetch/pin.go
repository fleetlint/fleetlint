package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// commitFile records, inside a cached catalog, the commit it was taken
// from; digestFile the digest of its files, checked on every read.
const (
	commitFile = "COMMIT"
	digestFile = "DIGEST"
)

// pinnedDirs are the parts of a catalog repository a run reads.
var pinnedDirs = []string{"presets", "templates", "rules"}

// maxPinnedBytes bounds one cached catalog version.
const maxPinnedBytes = 64 << 20

// PinFetch returns a FetchCatalog function for Loader. A catalog version
// is fetched once with git (shallow, with the credentials git already has)
// into cacheRoot and read from there afterwards, so a pinned repository
// works offline after its first run. An empty cacheRoot is the user's cache
// directory. A cache entry whose files no longer match their digest is
// fetched again.
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
		if commit, err := readCacheEntry(dir); err == nil {
			return dir, commit, nil
		}
		// Missing, incomplete or modified: replace it.
		if err := os.RemoveAll(dir); err != nil {
			return "", "", err
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

// CachePath is where one version of one catalog repository is kept: the
// version stays readable, and a hash of it keeps `a/b` and `a_b` apart.
func CachePath(root, repoURL, rev string) string {
	repoSum := sha256.Sum256([]byte(repoURL))
	revSum := sha256.Sum256([]byte(rev))
	return filepath.Join(root, hex.EncodeToString(repoSum[:8]), strings.ReplaceAll(rev, "/", "_")+"-"+hex.EncodeToString(revSum[:4]))
}

// WriteCacheEntry stores a catalog version's files with its commit and
// their digest. It builds the directory next to dest and renames it into
// place, so an interrupted write never leaves a half-filled entry.
func WriteCacheEntry(dest string, files map[string][]byte, commit string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dest), ".fetch-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp) //nolint:errcheck // best-effort cleanup; after a successful rename there is nothing left
	all := maps.Clone(files)
	all[commitFile] = []byte(commit + "\n")
	all[digestFile] = []byte(treeDigest(files) + "\n")
	if err := writeTree(tmp, all); err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		// Another run filled the entry first: use theirs.
		if _, statErr := readCacheEntry(dest); statErr == nil {
			return nil
		}
		return errors.Join(errors.New("store the catalog in the cache"), err)
	}
	return nil
}

// writeTree writes files below dir, refusing paths that leave it.
func writeTree(dir string, files map[string][]byte) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close() //nolint:errcheck // nothing to do on close failure after the writes
	for name, body := range files {
		if !fs.ValidPath(name) {
			return fmt.Errorf("%q is not a relative path", name)
		}
		if err := root.MkdirAll(path.Dir(name), 0o750); err != nil {
			return err
		}
		if err := root.WriteFile(name, body, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// readCacheEntry returns the commit of a complete, unmodified entry. A
// changed, added or missing file fails, and the entry is fetched again.
func readCacheEntry(dir string) (string, error) {
	commit, err := os.ReadFile(filepath.Join(dir, commitFile)) //nolint:gosec // a file fleetlint wrote in its own cache
	if err != nil {
		return "", err
	}
	want, err := os.ReadFile(filepath.Join(dir, digestFile)) //nolint:gosec // a file fleetlint wrote in its own cache
	if err != nil {
		return "", err
	}
	files := map[string][]byte{}
	fsys := os.DirFS(dir)
	for _, top := range pinnedDirs {
		err := fs.WalkDir(fsys, top, func(p string, d fs.DirEntry, err error) error {
			switch {
			case errors.Is(err, fs.ErrNotExist) && p == top:
				return fs.SkipDir
			case err != nil:
				return err
			case !d.Type().IsRegular() && !d.IsDir():
				return fmt.Errorf("%s is not a regular file", p)
			case d.IsDir():
				return nil
			}
			files[p], err = fs.ReadFile(fsys, p)
			return err
		})
		if err != nil {
			return "", err
		}
	}
	if treeDigest(files) != strings.TrimSpace(string(want)) {
		return "", errors.New("cached catalog does not match its digest")
	}
	return strings.TrimSpace(string(commit)), nil
}

// treeDigest hashes file names and contents in a fixed order.
func treeDigest(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		sum := sha256.Sum256(files[n])
		_, _ = fmt.Fprintf(h, "%s\x00%x\n", n, sum) // hash writes never fail
	}
	return hex.EncodeToString(h.Sum(nil))
}

// fetchCatalogTree reads presets/, templates/ and rules/ of one revision
// from git's objects, without a checkout, so no filter or hook configured
// for the user runs on fetched content, and stores them in dest.
func fetchCatalogTree(ctx context.Context, repoURL, rev, dest string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitFetchTimeout)
	defer cancel()
	tmp, err := os.MkdirTemp("", "fleetlint-pin-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp) //nolint:errcheck // best-effort cleanup of a temp dir
	if err := fetchRev(ctx, tmp, repoURL, rev); err != nil {
		return "", err
	}
	out, err := gitOut(ctx, tmp, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	if err != nil {
		return "", err
	}
	commit := strings.TrimSpace(string(out))
	listing, err := gitOut(ctx, tmp, append([]string{"ls-tree", "-r", "-z", "--name-only", "FETCH_HEAD", "--"}, pinnedDirs...)...)
	if err != nil {
		return "", err
	}
	files := map[string][]byte{}
	total := 0
	for _, name := range strings.Split(strings.TrimRight(string(listing), "\x00"), "\x00") {
		if name == "" {
			continue
		}
		if len(files) >= MaxFiles {
			return "", fmt.Errorf("more than %d files", MaxFiles)
		}
		if files[name], err = readBlob(ctx, tmp, treeObject(name)); err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		if total += len(files[name]); total > maxPinnedBytes {
			return "", fmt.Errorf("larger than %d bytes", maxPinnedBytes)
		}
	}
	if !slices.ContainsFunc(slices.Collect(maps.Keys(files)), func(n string) bool { return strings.HasPrefix(n, "presets/") }) {
		return "", fmt.Errorf("the repository has no presets/ at %s", rev)
	}
	return commit, WriteCacheEntry(dest, files, commit)
}
