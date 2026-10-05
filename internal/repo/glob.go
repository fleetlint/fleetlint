package repo

import (
	"path"
	"strings"
)

// MatchGlob matches a slash-separated path against a pattern that supports
// `*` (within a segment), `?`, character classes and `**` (any number of
// segments, including none). A pattern without a slash matches the base name
// anywhere in the tree, which is what people mean by `*.bak`.
func MatchGlob(pattern, name string) bool {
	pattern = strings.TrimPrefix(pattern, "./")
	if !strings.Contains(pattern, "/") {
		ok, err := path.Match(pattern, path.Base(name))
		return err == nil && ok
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

// ValidGlob reports whether a pattern is well-formed; callers that accept
// patterns from configuration use it to turn a typo into an error.
func ValidGlob(pattern string) bool {
	for _, seg := range strings.Split(strings.TrimPrefix(pattern, "./"), "/") {
		if seg == "**" {
			continue
		}
		if _, err := path.Match(seg, ""); err != nil {
			return false
		}
	}
	return true
}

func matchSegments(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			return matchDoubleStar(pat[1:], segs)
		}
		if len(segs) == 0 {
			return false
		}
		if ok, err := path.Match(pat[0], segs[0]); err != nil || !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}

// matchDoubleStar tries the remaining pattern at every suffix of segs.
func matchDoubleStar(rest, segs []string) bool {
	if len(rest) == 0 {
		return true
	}
	for i := 0; i <= len(segs); i++ {
		if matchSegments(rest, segs[i:]) {
			return true
		}
	}
	return false
}
