package catalog

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/fleetlint/fleetlint/internal/yamlsafe"

	"github.com/goccy/go-yaml"

	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/stacks"
)

// The rule library is the catalog's rules/ directory: one rule per file at
// rules/<family>/<name>.yaml, the file's path spelling the rule's id. A
// preset (or any catalog) selects from it with `- use: <id or glob>` in its
// rules list instead of defining the rule inline.
//
// A capability (kind: capability) states a requirement; its implementations
// live next to it at rules/<family>/<name>/<stack>.yaml and inherit every
// field they do not set. Selecting the capability selects them all.

// useEntry is a `use:` item of a catalog's rules list and where it stood.
type useEntry struct {
	Pattern string
	Index   int
}

// Library is the parsed rules/ directory of a source.
type Library struct {
	rules  map[string]model.Rule
	source map[string]string // id -> where the text lives, for Rule.Source
	ids    []string
	// aliases map a renamed id to the ids that replace it.
	aliases map[string][]string
	// impls keeps each implementation's file, so a catalog that redefines
	// the capability can rebuild it on its own definition.
	impls map[string]implFile
}

type implFile struct {
	path string
	body []byte
}

// Library parses the source's rules/ directory once. A source without one
// has an empty library.
func (s *Source) Library() (*Library, error) {
	s.lib.once.Do(func() {
		s.lib.val, s.lib.err = loadLibrary(s.Rules, "library "+s.Version, nil)
		if s.lib.err == nil {
			s.lib.val.aliases, s.lib.err = s.Aliases()
		}
	})
	return s.lib.val, s.lib.err
}

// loadLibrary parses a rules/ tree. Implementations inherit from a
// capability in the same tree or, for a catalog's own library, in base.
func loadLibrary(fsys fs.FS, source string, base *Library) (*Library, error) {
	lib := &Library{rules: map[string]model.Rule{}, source: map[string]string{}, impls: map[string]implFile{}}
	if fsys == nil {
		return lib, nil
	}
	if _, err := fs.Stat(fsys, "rules"); errors.Is(err, fs.ErrNotExist) {
		return lib, nil // a source without a library
	} else if err != nil {
		return nil, err
	}
	impls, err := lib.readRules(fsys, source)
	if err != nil {
		return nil, err
	}
	if err := lib.inheritAll(impls, base, source); err != nil {
		return nil, err
	}
	sort.Strings(lib.ids)
	lib.link()
	return lib, nil
}

// readRules adds every plain rule and capability of the tree and returns
// the implementation files, which need their capability first.
func (l *Library) readRules(fsys fs.FS, source string) (map[string][]byte, error) {
	impls := map[string][]byte{}
	err := fs.WalkDir(fsys, "rules", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".yaml") {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		if _, _, ok := model.SplitImplementation(pathID(p)); ok {
			impls[p] = b
			return nil
		}
		r, err := parseLibraryRule(b, p)
		if err != nil {
			return err
		}
		l.add(r, source)
		return nil
	})
	return impls, err
}

// inheritAll builds each implementation from its capability, found in this
// library or, for a catalog's own library, in base.
func (l *Library) inheritAll(impls map[string][]byte, base *Library, source string) error {
	for p, b := range impls {
		capID, _, _ := model.SplitImplementation(pathID(p))
		capRule, ok := l.rules[capID]
		if !ok && base != nil {
			capRule, ok = base.rules[capID]
		}
		if !ok || capRule.Kind != model.KindCapability {
			return fmt.Errorf("%s: implements %s, which is not a capability in the library", p, capID)
		}
		r, err := inherit(capRule, b, p)
		if err != nil {
			return err
		}
		l.add(r, source)
		l.impls[r.ID] = implFile{path: p, body: b}
	}
	return nil
}

// rebuildOn rebuilds the shared implementations of every capability the own
// library redefines, so its severity, policy and gate reach them.
func (l *Library) rebuildOn(shared, own *Library) error {
	for capID, capRule := range own.rules {
		if capRule.Kind != model.KindCapability {
			continue
		}
		for id, f := range shared.impls {
			if c, _, _ := model.SplitImplementation(id); c != capID {
				continue
			}
			if _, mine := own.rules[id]; mine {
				continue
			}
			r, err := inherit(capRule, f.body, f.path)
			if err != nil {
				return err
			}
			l.rules[id] = r
		}
	}
	return nil
}

func (l *Library) add(r model.Rule, source string) {
	l.rules[r.ID] = r
	l.source[r.ID] = source
	l.ids = append(l.ids, r.ID)
}

// link records on each capability the stacks it has implementations for.
func (l *Library) link() {
	for id, r := range l.rules {
		if r.Kind != model.KindCapability {
			continue
		}
		r = r.Clone()
		r.Implementations = nil
		for _, other := range l.ids {
			if impl := l.rules[other]; impl.Capability == id {
				r.Implementations = append(r.Implementations, impl.Target())
			}
		}
		l.rules[id] = r
	}
}

func pathID(p string) string { return strings.TrimSuffix(strings.TrimPrefix(p, "rules/"), ".yaml") }

// parseLibraryRule reads one rule file; the id must spell the path.
func parseLibraryRule(b []byte, p string) (model.Rule, error) {
	var r model.Rule
	if err := yamlsafe.Unmarshal(b, &r, yaml.Strict()); err != nil {
		return r, fmt.Errorf("%s: %w", p, err)
	}
	if want := pathID(p); r.ID != want {
		return r, fmt.Errorf("%s: rule id %q must be %q (the path spells the id)", p, r.ID, want)
	}
	if r.Use != "" {
		return r, fmt.Errorf("%s: a library rule cannot use another", p)
	}
	if err := requireSeverity(wrapRules(b)); err != nil {
		return r, fmt.Errorf("%s: %w", p, err)
	}
	if err := validateRule(&r, 0); err != nil {
		return r, fmt.Errorf("%s: %w", p, err)
	}
	if err := validatePolicy([]model.Rule{r}); err != nil {
		return r, fmt.Errorf("%s: %w", p, err)
	}
	return r, nil
}

// inherit builds an implementation from its file and its capability: every
// field the file sets wins, the rest comes from the capability. References
// add up; the capability's `when` becomes the implementation's gate.
func inherit(capRule model.Rule, b []byte, p string) (model.Rule, error) {
	var impl model.Rule
	if err := yamlsafe.Unmarshal(b, &impl, yaml.Strict()); err != nil {
		return impl, fmt.Errorf("%s: %w", p, err)
	}
	var set map[string]any
	if err := yamlsafe.Unmarshal(b, &set); err != nil {
		return impl, fmt.Errorf("%s: %w", p, err)
	}
	if err := checkImplementation(impl, set, p); err != nil {
		return impl, err
	}
	if _, target, _ := model.SplitImplementation(impl.ID); capRule.By == model.ByArtifact && target == stacks.Any {
		return impl, fmt.Errorf("%s: a capability keyed by artifact has no `any` implementation: what counts as signed, attested or described differs per artifact kind", p)
	}
	out := capRule.Clone()
	out.ID, out.Kind, out.When = impl.ID, impl.Kind, impl.When
	out.Gate = capRule.When
	out.Implementations = nil
	has := func(key string) bool { _, ok := set[key]; return ok }
	inheritScalars(&out, impl, has)
	for _, ref := range impl.Refs {
		if !slices.ContainsFunc(out.Refs, func(have model.Ref) bool { return have.URL == ref.URL }) {
			out.Refs = append(out.Refs, ref)
		}
	}
	if has("fix") {
		out.Fix.Human = firstNonEmpty(impl.Fix.Human, capRule.Fix.Human)
		out.Fix.Agent = firstNonEmpty(impl.Fix.Agent, capRule.Fix.Agent)
		out.Fix.Actions = impl.Fix.Actions
	}
	if err := validateRule(&out, 0); err != nil {
		return out, fmt.Errorf("%s: %w", p, err)
	}
	if err := validatePolicy([]model.Rule{out}); err != nil {
		return out, fmt.Errorf("%s: %w", p, err)
	}
	return out, nil
}

// checkImplementation refuses what an implementation file may not say.
func checkImplementation(impl model.Rule, set map[string]any, p string) error {
	if want := pathID(p); impl.ID != want {
		return fmt.Errorf("%s: rule id %q must be %q (the path spells the id)", p, impl.ID, want)
	}
	if _, ok := set["kind"]; !ok || impl.Kind == model.KindCapability {
		return fmt.Errorf("%s: an implementation states its kind (expr, outcome, go or command)", p)
	}
	partials, _ := set["partials"].([]any)
	for i, pt := range partials {
		if pm, _ := pt.(map[string]any); pm["severity"] == nil {
			return fmt.Errorf("%s: partials[%d]: severity is required", p, i)
		}
	}
	for _, key := range []string{"use", "stacks", "combine", "by"} {
		if _, ok := set[key]; ok {
			return fmt.Errorf("%s: an implementation takes no %s: (the path names its stack or artifact; combine and by come from the capability)", p, key)
		}
	}
	return nil
}

// inheritScalars copies the fields the implementation file sets.
func inheritScalars(out *model.Rule, impl model.Rule, has func(string) bool) {
	for key, set := range map[string]func(){
		"title":        func() { out.Title = impl.Title },
		"severity":     func() { out.Severity = impl.Severity },
		"tiers":        func() { out.Tiers = impl.Tiers },
		"scope":        func() { out.Scope = impl.Scope },
		"requirement":  func() { out.Requirement = impl.Requirement },
		"rationale":    func() { out.Rationale = impl.Rationale },
		"message":      func() { out.Message = impl.Message },
		"since":        func() { out.Since = impl.Since },
		"locked":       func() { out.Locked = impl.Locked },
		"min_severity": func() { out.MinSeverity = impl.MinSeverity },
		"exceptions":   func() { out.AllowExceptions = impl.AllowExceptions },
	} {
		if has(key) {
			set()
		}
	}
	// The capability's params are defaults; the implementation's keys win.
	if len(impl.Params) > 0 && out.Params == nil {
		out.Params = map[string]any{}
	}
	for k, v := range impl.Params {
		out.Params[k] = v
	}
	out.Foreach, out.Expr, out.Run = impl.Foreach, impl.Expr, impl.Run
	out.Satisfiers, out.Partials = impl.Satisfiers, impl.Partials
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

// Rule returns one definition by id, implementations included.
func (l *Library) Rule(id string) (model.Rule, bool) {
	r, ok := l.rules[id]
	return r, ok
}

// Select resolves a use pattern: an exact id must exist, a glob must match
// at least one rule. `*` spans the family separator. A capability brings
// its implementations along.
func (l *Library) Select(pattern string) ([]string, error) {
	if !strings.ContainsAny(pattern, "*?") {
		if _, ok := l.rules[pattern]; ok {
			return l.withImplementations(pattern), nil
		}
		var out []string
		for _, id := range l.aliases[pattern] { // a renamed id selects what replaced it
			if _, ok := l.rules[id]; ok {
				out = append(out, l.withImplementations(id)...)
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("use %s: no such rule in the library (it has %d rules)", pattern, len(l.ids))
		}
		return out, nil
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

// withImplementations is the id, followed by its implementations when it
// names a capability.
func (l *Library) withImplementations(id string) []string {
	out := []string{id}
	if l.rules[id].Kind != model.KindCapability {
		return out
	}
	for _, other := range l.ids {
		if l.rules[other].Capability == id {
			out = append(out, other)
		}
	}
	return out
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

// libraryFor is the library a catalog's `use:` entries search: the rules/
// tree shipped with the catalog, then the source's; the catalog's own rule
// wins when both spell the same id.
func (l Loader) libraryFor(c *Catalog) (*Library, error) {
	shared, err := l.source().Library()
	if err != nil {
		return nil, err
	}
	if c.Library == nil {
		return shared, nil
	}
	own, err := loadLibrary(c.Library, c.Metadata.Name+"@"+c.Metadata.Version+" rules/", shared)
	if err != nil {
		return nil, fmt.Errorf("catalog %s: %w", c.Metadata.Name, err)
	}
	merged := &Library{rules: map[string]model.Rule{}, source: map[string]string{}, aliases: shared.aliases}
	for id, r := range shared.rules {
		merged.rules[id], merged.source[id] = r, shared.source[id]
	}
	for id, r := range own.rules {
		merged.rules[id], merged.source[id] = r, own.source[id]
	}
	if err := merged.rebuildOn(shared, own); err != nil {
		return nil, fmt.Errorf("catalog %s: %w", c.Metadata.Name, err)
	}
	for id := range merged.rules {
		merged.ids = append(merged.ids, id)
	}
	sort.Strings(merged.ids)
	merged.link()
	return merged, nil
}

// selected is the library rule as the catalog takes it in: a copy marked
// with its source and the catalog that selected it. A catalog written for
// engine level 7 or later may not take in a rule with `combine:`.
func (l *Library) selected(id string, c *Catalog) (model.Rule, error) {
	r := l.rules[id].Clone()
	if r.Combine != "" && c.Metadata.Engine >= combineGone {
		return model.Rule{}, fmt.Errorf("%s: rule %s (%s): combine is gone since engine level %d", c.Metadata.Name, id, l.source[id], combineGone)
	}
	r.Source = l.source[id]
	r.SelectedBy = c.Metadata.Name + "@" + c.Metadata.Version
	return r, nil
}

// expand replaces the catalog's `use:` entries with the library rules they
// select, in place, keeping the order of the list. Each rule carries the
// catalog as its source, like an inline rule.
func (l Loader) expand(c *Catalog) error {
	if len(c.uses) == 0 {
		return nil
	}
	lib, err := l.libraryFor(c)
	if err != nil {
		return err
	}
	if len(lib.ids) == 0 {
		return errors.New("`use:` needs a rules/ library: shipped with the catalog or in the catalog source")
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
			r, err := lib.selected(id, c)
			if err != nil {
				return err
			}
			out = append(out, r)
			c.used[id] = true
		}
	}
	c.Rules = append(out, c.Rules[next:]...)
	c.uses = nil
	return nil
}
