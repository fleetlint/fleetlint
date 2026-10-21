package fix_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/fix"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

// Only uncommitted changes to tracked files refuse a write; without git
// nothing does, and when git cannot tell the guard fails closed.
func TestCheckClean(t *testing.T) {
	t.Parallel()
	r := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n"})
	testutil.WriteFiles(t, r.Root, map[string]string{"wip.txt": "x"})
	if err := fix.CheckClean(r); err != nil {
		t.Fatalf("an untracked file is not in danger: %v", err)
	}
	testutil.WriteFiles(t, r.Root, map[string]string{"go.mod": "module x\n\ngo 1.26\n"})
	if err := fix.CheckClean(r); !errors.Is(err, fix.ErrDirty) {
		t.Fatalf("modified tracked file: %v, want ErrDirty", err)
	}
	if err := fix.CheckClean(testutil.Fixture(t, map[string]string{"go.mod": "module x\n"})); err != nil {
		t.Fatalf("without git: %v", err)
	}
	broken := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n"})
	testutil.WriteFiles(t, broken.Root, map[string]string{".git/index": "corrupt"})
	err := fix.CheckClean(broken)
	if err == nil || errors.Is(err, fix.ErrDirty) {
		t.Fatalf("corrupt index: %v, want the git error", err)
	}
	for _, want := range []string{"cannot tell whether the working tree is clean", "git status", "--allow-dirty"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q lacks %q", err, want)
		}
	}
}
