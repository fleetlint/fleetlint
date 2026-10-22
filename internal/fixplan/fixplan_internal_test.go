package fixplan

import (
	"testing"

	"github.com/fleetlint/fleetlint/internal/fix"
)

func TestFileEdits(t *testing.T) {
	t.Parallel()
	create := fix.Change{Path: "a", Kind: "create", New: "x\n"}
	if got := fileEdits([]fix.Change{create, {Path: "b", Kind: "edit", Old: "1", New: "12"}}); len(got) != 2 || got[0].New != "x\n" || got[1].Old != "1" {
		t.Errorf("one edit per file: %v", got)
	}
	if got := fileEdits([]fix.Change{create, {Path: "c", Kind: "untrack"}}); got != nil {
		t.Errorf("an untrack is no text edit: %v", got)
	}
	if got := fileEdits([]fix.Change{create, {Path: "a", Kind: "append", Old: "x\n", New: "x\ny\n"}}); got != nil {
		t.Errorf("two changes to one file are no single edit: %v", got)
	}
}
