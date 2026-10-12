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
