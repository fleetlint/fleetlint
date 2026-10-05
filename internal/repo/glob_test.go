package repo

import "testing"

func TestMatchGlob(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"*.log", "a.log", true},
		{"*.log", "deep/dir/a.log", true}, // bare patterns match the base name anywhere
		{"docs/*.md", "docs/a.md", true},
		{"docs/*.md", "docs/sub/a.md", false},
		{"docs/**/*.md", "docs/sub/deep/a.md", true},
		{"docs/**/*.md", "docs/a.md", true},
		{"**/node_modules/**", "web/node_modules/x/y.js", true},
		{"./src/*.go", "src/a.go", true},
		{"src/[", "src/x", false}, // malformed never matches
		{"[", "x", false},
	}
	for _, c := range cases {
		if got := MatchGlob(c.pattern, c.name); got != c.want {
			t.Errorf("MatchGlob(%q,%q)=%v want %v", c.pattern, c.name, got, c.want)
		}
	}
}

func TestValidGlob(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"*.log", "docs/**/*.md", "a/[ab]c", "**"} {
		if !ValidGlob(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"[", "docs/[a-", "**/[x"} {
		if ValidGlob(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}
