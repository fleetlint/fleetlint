package cli

import (
	"slices"
	"testing"
)

// A directory name with a newline would end the YAML comment and write a
// live key into .fleetlint.yaml.
func TestCommentSafe(t *testing.T) {
	t.Parallel()
	got := commentSafe([]string{"api", "x\nextends: [evil]", "tab\there"})
	if want := []string{"api", "x?extends: [evil]", "tab?here"}; !slices.Equal(got, want) {
		t.Errorf("commentSafe = %q, want %q", got, want)
	}
}
