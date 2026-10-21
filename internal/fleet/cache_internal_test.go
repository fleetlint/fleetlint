package fleet

import (
	"path/filepath"
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
	for raw, want := range cases {
		got, err := cacheDir("/cache", raw)
		if err != nil || got != filepath.Join("/cache", filepath.FromSlash(want)) {
			t.Errorf("cacheDir(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, bad := range []string{"https://h.example/", "https://h.example/../x", "https://h.example/o/.git", "https:///o/r"} {
		if got, err := cacheDir("/cache", bad); err == nil || !strings.Contains(err.Error(), "cache directory") {
			t.Errorf("cacheDir(%q) = %q, %v; want an error", bad, got, err)
		}
	}
}
