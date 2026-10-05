package fix_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/fix"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

type fakeTemplates map[string]string

func (f fakeTemplates) Template(name string) ([]byte, error) {
	if b, ok := f[name]; ok {
		return []byte(b), nil
	}
	return nil, os.ErrNotExist
}

func TestTemplateNeverOverwrites(t *testing.T) {
	t.Parallel()
	r := testutil.Fixture(t, map[string]string{".editorconfig": "mine"})
	tpl := fakeTemplates{"editorconfig": "theirs", "go/pre-commit-config.yaml": "hooks"}
	changes, err := fix.Plan(r, "go", []fix.Action{
		{Template: "editorconfig", To: ".editorconfig"},
		{Template: "{stack}/pre-commit-config.yaml", To: ".pre-commit-config.yaml"},
	}, tpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Path != ".pre-commit-config.yaml" || changes[0].Kind != "create" {
		t.Fatalf("expected one create for the missing file, got %+v", changes)
	}
	if err := fix.Apply(changes); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(r.Root, ".pre-commit-config.yaml"))
	mine, _ := os.ReadFile(filepath.Join(r.Root, ".editorconfig"))
	if string(got) != "hooks" || string(mine) != "mine" {
		t.Fatalf("apply wrote wrong content: %q / %q", got, mine)
	}
}

// A template that exists only per stack has nothing for a repository without
// one: no change is planned, and nothing fails.
func TestStackTemplateWithoutStack(t *testing.T) {
	t.Parallel()
	r := testutil.Fixture(t, map[string]string{})
	changes, err := fix.Plan(r, "", []fix.Action{{Template: "{stack}/x", To: "x"}}, fakeTemplates{}, nil)
	if err != nil || len(changes) != 0 {
		t.Fatalf("changes=%v err=%v", changes, err)
	}
}

func TestGitignoreAppendsOnlyMissing(t *testing.T) {
	t.Parallel()
	r := testutil.Fixture(t, map[string]string{".gitignore": "bin/\n*.log"})
	changes, err := fix.Plan(r, "", []fix.Action{{Gitignore: []string{"*.log", ".DS_Store"}}}, fakeTemplates{}, nil)
	if err != nil || len(changes) != 1 || changes[0].Kind != "append" {
		t.Fatalf("changes=%+v err=%v", changes, err)
	}
	if err := fix.Apply(changes); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(r.Root, ".gitignore"))
	if string(got) != "bin/\n*.log\n.DS_Store\n" {
		t.Fatalf("got %q", got)
	}
}

func TestUntrackUsesFindings(t *testing.T) {
	t.Parallel()
	r := testutil.GitFixture(t, map[string]string{"a.log": "x", "keep.txt": "y", "sub/b.log": "z"})
	findings := []model.Finding{{Path: "a.log"}, {Path: "sub/b.log"}}
	changes, err := fix.Plan(r, "", []fix.Action{{Untrack: "*"}}, fakeTemplates{}, findings)
	if err != nil || len(changes) != 2 {
		t.Fatalf("changes=%+v err=%v", changes, err)
	}
	if err := fix.Apply(changes); err != nil {
		t.Fatal(err)
	}
	out, _ := r.Git("ls-files")
	if strings.Contains(out, "a.log") || !strings.Contains(out, "keep.txt") {
		t.Fatalf("index after untrack: %q", out)
	}
	if _, err := os.Stat(filepath.Join(r.Root, "a.log")); err != nil {
		t.Fatal("untrack must keep the file on disk")
	}
}

func TestDecodeRejectsUnknownKeys(t *testing.T) {
	t.Parallel()
	if _, err := fix.Decode([]map[string]any{{"templat": "x"}}); err == nil {
		t.Fatal("typo must be rejected")
	}
	if _, err := fix.Decode([]map[string]any{{}}); err == nil {
		t.Fatal("empty action must be rejected")
	}
	a, err := fix.Decode([]map[string]any{{"template": "t", "to": "x", "gitignore": []any{"a", "b"}}})
	if err != nil || len(a) != 1 || len(a[0].Gitignore) != 2 {
		t.Fatalf("decode: %+v %v", a, err)
	}
}
