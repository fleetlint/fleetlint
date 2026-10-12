// Package rootfs reads and writes files below a directory without leaving
// it: paths go through os.Root, so neither ".." nor a symlink reaches
// outside, and only regular files are read.
package rootfs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrTooLarge is returned for a file above the read limit.
var ErrTooLarge = errors.New("file too large")

// ErrNotRegular is returned for a device, pipe, socket or directory.
var ErrNotRegular = errors.New("not a regular file")

// Rel turns path (absolute, or relative to base) into a path relative to
// dir and refuses one that leaves it.
func Rel(dir, base, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%s is outside %s", path, dir)
	}
	return rel, nil
}

// ReadFile reads rel below root, at most limit bytes.
func ReadFile(root *os.Root, rel string, limit int64) ([]byte, error) {
	f, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // a read-only handle
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", rel, ErrNotRegular)
	}
	if st.Size() > limit {
		return nil, fmt.Errorf("%s: %w (%d bytes, limit %d)", rel, ErrTooLarge, st.Size(), limit)
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s: %w (limit %d)", rel, ErrTooLarge, limit)
	}
	return b, nil
}

// ReadIn reads rel below dir; see ReadFile.
func ReadIn(dir, rel string, limit int64) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close() //nolint:errcheck // a read-only handle
	return ReadFile(root, rel, limit)
}

// Stat describes rel below root, following symlinks only inside it.
func Stat(root *os.Root, rel string) (fs.FileInfo, error) {
	return root.Stat(rel)
}

// Lstat describes rel below root without following a final symlink.
func Lstat(root *os.Root, rel string) (fs.FileInfo, error) {
	return root.Lstat(rel)
}
