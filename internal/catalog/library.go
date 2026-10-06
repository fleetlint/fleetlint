package catalog

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/goccy/go-yaml"

	"github.com/fleetlint/fleetlint/internal/model"
)

// The rule library is the catalog's rules/ directory: one rule per file at
// rules/<family>/<name>.yaml, the file's path spelling the rule's id. A
// preset (or any catalog) selects from it with `- use: <id or glob>` in its
// rules list instead of defining the rule inline.

// useEntry is a `use:` item of a catalog's rules list and where it stood.
type useEntry struct {
	Pattern string
	Index   int
}

// Library is the parsed rules/ directory of a source.
type Library struct {
	rules map[string]model.Rule
	ids   []string
}

var (
	libraries   = map[*Source]*Library{}
	librariesMu sync.Mutex
)

// Library parses the source's rules/ directory once. A source without one
// has an empty library.
func (s *Source) Library() (*Library, error) {
	librariesMu.Lock()
	defer librariesMu.Unlock()
	if lib, ok := libraries[s]; ok {
		return lib, nil
	}
	lib, err := loadLibrary(s.Rules)
	if err != nil {
		return nil, err
	}
	libraries[s] = lib
	return lib, nil
}

func loadLibrary(fsys fs.FS) (*Library, error) {
	lib := &Library{rules: map[string]model.Rule{}}
	if fsys == nil {
		return lib, nil
	}
	if _, err := fs.Stat(fsys, "rules"); errors.Is(err, fs.ErrNotExist) {
		return lib, nil // a source without a library
	} else if err != nil {
		return nil, err
	}
	err := fs.WalkDir(fsys, "rules", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".yaml") {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		r, err := parseLibraryRule(b, p)
		if err != nil {
			return err
		}
		lib.rules[r.ID] = r
		lib.ids = append(lib.ids, r.ID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(lib.ids)
	return lib, nil
}

// parseLibraryRule reads one rule file; the id must spell the path.
func parseLibraryRule(b []byte, p string) (model.Rule, error) {
	var r model.Rule
	if err := yaml.UnmarshalWithOptions(b, &r, yaml.Strict()); err != nil {
		return r, fmt.Errorf("%s: %w", p, err)
	}
	want := strings.TrimSuffix(strings.TrimPrefix(p, "rules/"), ".yaml")
	if r.ID != want {
		return r, fmt.Errorf("%s: rule id %q must be %q (the path spells the id)", p, r.ID, want)
	}
	if r.Use != "" {
		return r, fmt.Errorf("%s: a library rule cannot use another", p)
	}
	if err := requireSeverity(wrapRules(b)); err != nil {
		return r, fmt.Errorf("%s: %w", p, err)
	}
	if err := validateRule(&r); err != nil {
		return r, fmt.Errorf("%s: %w", p, err)
	}
	if err := validatePolicy([]model.Rule{r}); err != nil {
		return r, fmt.Errorf("%s: %w", p, err)
	}
	return r, nil
}

// wrapRules indents a single rule document into a `rules:` list so the
// list-level checks apply to it.
func wrapRules(b []byte) []byte {
	var sb strings.Builder
	sb.WriteString("rules:\n  - ")
	sb.WriteString(strings.ReplaceAll(strings.TrimRight(string(b), "\n"), "\n", "\n    "))
	sb.WriteString("\n")
	return []byte(sb.String())
}

// IDs returns every rule id in the library, sorted.
func (l *Library) IDs() []string { return l.ids }

// Select resolves a use pattern: an exact id must exist, a glob must match
// at least one rule. `*` spans the family separator.
func (l *Library) Select(pattern string) ([]string, error) {
	if !strings.ContainsAny(pattern, "*?") {
		if _, ok := l.rules[pattern]; !ok {
			return nil, fmt.Errorf("use %s: no such rule in the library (it has %d rules)", pattern, len(l.ids))
		}
		return []string{pattern}, nil
	}
	rx, err := regexp.Compile(GlobRegexp(pattern))
	if err != nil {
		return nil, fmt.Errorf("use %s: %w", pattern, err)
	}
	var out []string
	for _, id := range l.ids {
		if rx.MatchString(id) {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("use %s: matches no rule in the library", pattern)
	}
	return out, nil
}

// GlobRegexp turns a rule-id pattern into a regular expression; `*` spans
// the family separator, so "*" is every rule and "lint/*" a family.
func GlobRegexp(pattern string) string {
	var b strings.Builder
	b.WriteString("^")
	for _, r := range pattern {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	return b.String()
}

// expand replaces the catalog's `use:` entries with the library rules they
// select, in place, keeping the order of the list. Each rule carries the
// catalog as its source, like an inline rule.
func (l Loader) expand(c *Catalog) error {
	if len(c.uses) == 0 {
		return nil
	}
	lib, err := l.source().Library()
	if err != nil {
		return err
	}
	if len(lib.ids) == 0 {
		return errors.New("`use:` needs a catalog source with a rules/ library")
	}
	have := map[string]bool{}
	for _, r := range c.Rules {
		have[r.ID] = true
	}
	var out []model.Rule
	next := 0
	for _, u := range c.uses {
		out = append(out, c.Rules[next:u.Index]...)
		next = u.Index
		ids, err := lib.Select(u.Pattern)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if have[id] {
				continue
			}
			have[id] = true
			r := lib.rules[id]
			r.Source = c.Metadata.Name + "@" + c.Metadata.Version
			out = append(out, r)
			c.used[id] = true
		}
	}
	c.Rules = append(out, c.Rules[next:]...)
	c.uses = nil
	return nil
}
