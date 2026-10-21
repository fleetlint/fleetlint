package rootfs_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/fleetlint/fleetlint/internal/rootfs"
)

func TestReadFileStaysInside(t *testing.T) {
	t.Parallel()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	dir := t.TempDir()
	for name, body := range map[string]string{secret: "s", filepath.Join(dir, "ok"): "ok", filepath.Join(dir, "big"): "0123456789"} {
		if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for link, target := range map[string]string{"out": secret, "zero": os.DevNull} {
		if err := os.Symlink(target, filepath.Join(dir, link)); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close() //nolint:errcheck // a read-only handle
	if b, err := rootfs.ReadFile(root, "ok", 5); err != nil || string(b) != "ok" {
		t.Fatalf("ok = %q, %v", b, err)
	}
	if _, err := rootfs.ReadFile(root, "big", 5); !errors.Is(err, rootfs.ErrTooLarge) {
		t.Errorf("big: %v", err)
	}
	for _, name := range []string{"out", "zero", "../" + filepath.Base(outside) + "/secret"} {
		if b, err := rootfs.ReadFile(root, name, 5); err == nil {
			t.Errorf("%s was read: %q", name, b)
		}
	}
}

func TestRel(t *testing.T) {
	t.Parallel()
	if rel, err := rootfs.Rel("/r", "/r/sub", "../c.yaml"); err != nil || rel != "c.yaml" {
		t.Errorf("rel = %q, %v", rel, err)
	}
	for _, p := range []string{"../x", "/etc/passwd", "/r/../x"} {
		if _, err := rootfs.Rel("/r", "/r", p); err == nil {
			t.Errorf("%s is accepted", p)
		}
	}
}

func TestReadIn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := rootfs.ReadIn(dir, "a.txt", 10); err != nil || string(b) != "abc" {
		t.Fatalf("a.txt = %q, %v", b, err)
	}
	if _, err := rootfs.ReadIn(dir, "a.txt", 2); !errors.Is(err, rootfs.ErrTooLarge) {
		t.Errorf("limit: %v", err)
	}
	if _, err := rootfs.ReadIn(dir, "missing", 10); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}
	if _, err := rootfs.ReadIn(dir, ".", 10); !errors.Is(err, rootfs.ErrNotRegular) {
		t.Errorf("a directory: %v", err)
	}
	if _, err := rootfs.ReadIn(filepath.Join(dir, "nodir"), "a.txt", 10); err == nil {
		t.Error("a missing root is an error")
	}
}

func TestStatAndLstat(t *testing.T) {
	t.Parallel()
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real"), []byte("real!"), 0o600); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{"inside": "real", "out": outside} {
		if err := os.Symlink(target, filepath.Join(dir, link)); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close() //nolint:errcheck // a read-only handle
	if st, err := rootfs.Stat(root, "inside"); err != nil || st.Size() != 5 || st.Mode()&os.ModeSymlink != 0 {
		t.Errorf("Stat follows a link inside: %v %v", st, err)
	}
	if st, err := rootfs.Lstat(root, "inside"); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Errorf("Lstat describes the link itself: %v %v", st, err)
	}
	if _, err := rootfs.Stat(root, "out"); err == nil {
		t.Error("Stat must not follow a link outside the root")
	}
	if st, err := rootfs.Lstat(root, "out"); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Errorf("Lstat sees the link that points outside: %v %v", st, err)
	}
	if _, err := rootfs.Stat(root, "missing"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}
}
