package fleet

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The cache directory follows the URL's host and path segments, so URLs
// that only differ in where a separator sits never share a clone.
func TestCacheDirFollowsHostAndPath(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"https://github.com/a/b_c.git":           "github.com/a/b_c",
		"https://github.com/a_b/c":               "github.com/a_b/c",
		"git@github.com:a/b.git":                 "github.com/a/b",
		"ssh://git@git.example:2222/o/r.git":     "git.example_2222/o/r",
		"https://gitlab.example/group/sub/r.git": "gitlab.example/group/sub/r",
		"https://h.example/o/we%20ird":           "h.example/o/we_20ird",
		"https://h.example/o/%2F":                "h.example/o/_2F",
		"/srv/git/x/y":                           "local/srv/git/x/y",
	}
	hash := regexp.MustCompile(`-[0-9a-f]{8}$`)
	for raw, want := range cases {
		got, err := cacheDir("/cache", raw)
		if err != nil || hash.ReplaceAllString(got, "") != filepath.Join("/cache", filepath.FromSlash(want)) || !hash.MatchString(got) {
			t.Errorf("cacheDir(%q) = %q, %v; want %q-<hash>", raw, got, err, want)
		}
	}
	for _, bad := range []string{"https://h.example/", "https://h.example/../x", "https://h.example/o/.git", "https:///o/r"} {
		if got, err := cacheDir("/cache", bad); err == nil || !strings.Contains(err.Error(), "cache directory") {
			t.Errorf("cacheDir(%q) = %q, %v; want an error", bad, got, err)
		}
	}
}

// URLs that sanitise to the same name, or differ only in case (one
// directory on a case-insensitive filesystem), keep their own clones; the
// same repository under another URL form shares one.
func TestCacheDirNeverCollides(t *testing.T) {
	t.Parallel()
	dir := func(raw string) string {
		t.Helper()
		d, err := cacheDir("/cache", raw)
		if err != nil {
			t.Fatal(err)
		}
		return strings.ToLower(d)
	}
	for _, pair := range [][2]string{
		{"https://h.example/o/we%20ird", "https://h.example/o/we_20ird"},
		{"https://github.com/Org/Repo", "https://github.com/org/repo"},
		{"https://h.example/o/a+b", "https://h.example/o/a_b"},
	} {
		if dir(pair[0]) == dir(pair[1]) {
			t.Errorf("%s and %s share %s", pair[0], pair[1], dir(pair[0]))
		}
	}
	if dir("https://github.com/a/b.git") != dir("git@github.com:a/b") || dir("https://GitHub.com/a/b") != dir("https://github.com/a/b") {
		t.Error("one repository under another URL form or host case shares its clone")
	}
}
