package catalog_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

const pinnedPreset = "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: pinned, version: 9.9.9}\nrules:\n  - {id: v9/only, title: t, kind: expr, severity: warning, expr: 'file(\"V9\")', message: m, requirement: r, fix: {human: h, actions: [{template: editorconfig, to: .editorconfig}]}}\n"

func TestPinFetchCachesOneVersion(t *testing.T) {
	t.Parallel()
	src := testutil.GitFixture(t, map[string]string{
		"presets/recommended.yaml": pinnedPreset, "templates/editorconfig": "root = true\n# v9\n", "README.md": "not needed\n",
	}).Root
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", src}, args...)...)
		cmd.Env = testutil.GitEnv()
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("tag", "v9.9.9")
	head := git("rev-parse", "HEAD")
	cache := t.TempDir()
	fetch := catalog.PinFetch(context.Background(), cache)

	dir, commit, err := fetch(src, "v9.9.9")
	if err != nil || commit != head {
		t.Fatalf("fetch by tag: commit=%s err=%v", commit, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "presets/recommended.yaml")); string(b) != pinnedPreset {
		t.Fatalf("preset not in the cache: %q", b)
	}
	for _, absent := range []string{"README.md", ".git"} {
		if _, err := os.Stat(filepath.Join(dir, absent)); err == nil {
			t.Errorf("only presets and templates are kept, found %s", absent)
		}
	}
	// The second use needs neither the repository nor the network.
	if err := os.RemoveAll(src); err != nil {
		t.Fatal(err)
	}
	again, commit, err := fetch(src, "v9.9.9")
	if err != nil || again != dir || commit != head {
		t.Fatalf("cached use: dir=%s commit=%s err=%v", again, commit, err)
	}
	if _, _, err := fetch(src, "v0.0.1"); err == nil {
		t.Fatal("a version that is neither cached nor fetchable must fail")
	}
	if _, _, err := fetch(src, "--upload-pack=x"); err == nil {
		t.Fatal("a version that is not a tag or commit must be refused")
	}
	entries, _ := os.ReadDir(filepath.Dir(dir))
	if len(entries) != 1 {
		t.Errorf("failed fetches must leave nothing behind: %v", entries)
	}
}

func TestPinSwitchesPresetsAndTemplates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.WriteFiles(t, dir, map[string]string{"presets/recommended.yaml": pinnedPreset, "templates/editorconfig": "# v9\n"})
	l := catalog.Loader{FetchCatalog: func(repo, rev string) (string, string, error) {
		if repo != catalog.DefaultRepo || rev != "v9.9.9" {
			t.Errorf("fetch asked for %s %s", repo, rev)
		}
		return dir, strings.Repeat("ab", 20), nil
	}}
	src, err := l.Pin("", "v9.9.9")
	if err != nil {
		t.Fatal(err)
	}
	if got := src.Describe(); got != "v9.9.9 (abababababab), pinned in .fleetlint.yaml" {
		t.Errorf("describe: %q", got)
	}
	cats, err := l.Load("fleetlint:recommended")
	if err != nil || len(cats) != 1 || cats[0].Rules[0].ID != "v9/only" {
		t.Fatalf("presets must come from the pinned catalog: %v %v", cats, err)
	}
	if _, err := l.Load("fleetlint:slop"); err == nil || !strings.Contains(err.Error(), "v9.9.9") {
		t.Errorf("a preset the pinned version lacks must say which version: %v", err)
	}
	if b, err := src.FixTemplates().Template("editorconfig"); err != nil || string(b) != "# v9\n" {
		t.Errorf("templates must come from the pinned catalog: %q %v", b, err)
	}
	if _, err := (&catalog.Loader{}).Pin("", "v9.9.9"); err == nil {
		t.Error("pinning must be refused where fetching is disabled")
	}
}
