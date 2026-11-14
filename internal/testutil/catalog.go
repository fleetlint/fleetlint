package testutil

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// CatalogPackage is the catalog module the binary embeds, as `go list` sees
// it: its source directory and its //go:embed patterns.
type CatalogPackage struct {
	Dir           string
	EmbedPatterns []string
}

// BuiltinCatalog lists the catalog package this build uses (go.work or the
// module cache).
func BuiltinCatalog(t *testing.T) CatalogPackage {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "go", "list", "-json", "github.com/fleetlint/catalog")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list the catalog module: %v", err)
	}
	var p CatalogPackage
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatal(err)
	}
	if p.Dir == "" || len(p.EmbedPatterns) == 0 {
		t.Fatalf("go list found no catalog source or embeds: %s", out)
	}
	return p
}

// BuiltinCatalogRepo is a git repository holding every file of the built-in
// catalog's module, tagged tag: what a pin of that version fetches.
func BuiltinCatalogRepo(t *testing.T, tag string) string {
	t.Helper()
	src := BuiltinCatalog(t).Dir
	files := map[string]string{}
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.Name() == ".git" && d.IsDir():
			return filepath.SkipDir
		case d.Name() == ".git" || !d.Type().IsRegular(): // a worktree's .git is a file
			return nil
		}
		b, err := os.ReadFile(p) //nolint:gosec // test helper reading the catalog module
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		files[filepath.ToSlash(rel)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := GitFixture(t, files).Root
	Git(t, dir, "tag", tag)
	return dir
}
