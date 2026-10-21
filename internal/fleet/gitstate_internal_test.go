package fleet

import (
	"testing"

	"github.com/fleetlint/fleetlint/internal/testutil"
)

// The report counts every uncommitted change, untracked files included; a
// repository without git has none.
func TestDirtyCount(t *testing.T) {
	t.Parallel()
	r := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n"})
	testutil.WriteFiles(t, r.Root, map[string]string{"go.mod": "module x\n\ngo 1.26\n", "wip.txt": "x"})
	if n := dirtyCount(r); n != 2 {
		t.Errorf("dirty count %d, want 2", n)
	}
	if n := dirtyCount(testutil.Fixture(t, map[string]string{"go.mod": "module x\n"})); n != 0 {
		t.Errorf("dirty count without git %d, want 0", n)
	}
}
