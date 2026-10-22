// Package fix applies the declarative fix actions a rule carries. It is a
// dry run unless told otherwise: it computes the changes, renders them as a
// diff, and writes nothing. Actions are deliberately few and mechanical;
// anything else is a prompt for a person or an agent.
package fix

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/repo"
)

// Action is one declarative fix step from a rule's `fix.actions`.
type Action struct {
	// Template creates a file from the catalog's templates when it is absent.
	// `{stack}` in the name selects the stack-specific variant.
	Template string `yaml:"template,omitempty" json:"template,omitempty"`
	// To is the destination for Template (defaults to the template's base name).
	To string `yaml:"to,omitempty" json:"to,omitempty"`
	// Untrack removes matching tracked files from the index (not the disk).
	Untrack string `yaml:"untrack,omitempty" json:"untrack,omitempty"`
	// Gitignore appends patterns to .gitignore if missing.
	Gitignore []string `yaml:"gitignore,omitempty" json:"gitignore,omitempty"`
	// Set adds a value to an existing JSON or TOML file.
	Set *SetAction `yaml:"set,omitempty" json:"set,omitempty"`
	// Append adds a template's block to an existing TOML or YAML file.
	Append *AppendAction `yaml:"append,omitempty" json:"append,omitempty"`
	// Merge adds a JSON template's missing keys to an existing JSON file.
	Merge *MergeAction `yaml:"merge,omitempty" json:"merge,omitempty"`
}

// Decode converts a rule's generic action maps into Actions, rejecting
// unknown keys so a typo in a catalog is caught at load time.
func Decode(raw []map[string]any) ([]Action, error) {
	out := make([]Action, 0, len(raw))
	for i, m := range raw {
		var a Action
		for k, v := range m {
			if err := a.set(k, v); err != nil {
				return nil, fmt.Errorf("actions[%d]: %w", i, err)
			}
		}
		if a.Template == "" && a.Untrack == "" && len(a.Gitignore) == 0 && a.Set == nil && a.Append == nil && a.Merge == nil {
			return nil, fmt.Errorf("actions[%d]: empty action", i)
		}
		if err := a.validTargets(); err != nil {
			return nil, fmt.Errorf("actions[%d]: %w", i, err)
		}
		out = append(out, a)
	}
	return out, nil
}

func (a *Action) set(key string, v any) error {
	switch key {
	case "template":
		a.Template, _ = v.(string)
	case "to":
		a.To, _ = v.(string)
	case "untrack":
		a.Untrack, _ = v.(string)
	case "gitignore":
		for _, item := range toList(v) {
			if s, ok := item.(string); ok {
				a.Gitignore = append(a.Gitignore, s)
			}
		}
	case "set":
		s, err := decodeSet(v)
		if err != nil {
			return err
		}
		a.Set = s
	case "append":
		ap, err := decodeAppend(v)
		if err != nil {
			return err
		}
		a.Append = ap
	case "merge":
		m, err := decodeMerge(v)
		if err != nil {
			return err
		}
		a.Merge = m
	default:
		return fmt.Errorf("unknown key %q (want template, to, untrack, gitignore, set, append, merge)", key)
	}
	return nil
}

// appendable are the files an append may extend: formats whose result is
// parsed before writing, and Gemfile.
var appendable = map[string]bool{".toml": true, ".yaml": true, ".yml": true}

// validTargets refuses paths a catalog must not write: outside the
// repository or inside .git, where a config or hook would run code.
func (a *Action) validTargets() error {
	targets := []string{a.To}
	if a.Set != nil {
		targets = append(targets, a.Set.File)
	}
	if a.Merge != nil {
		targets = append(targets, a.Merge.File)
	}
	if a.Append != nil {
		targets = append(targets, a.Append.File)
		if !appendable[filepath.Ext(a.Append.File)] && path.Base(a.Append.File) != "Gemfile" {
			return fmt.Errorf("append: %s is not a TOML or YAML file or a Gemfile", a.Append.File)
		}
	}
	for _, t := range targets {
		if t == "" {
			continue
		}
		if !fs.ValidPath(t) || repo.InGitDir(t) {
			return fmt.Errorf("%q is not a path inside the repository that fleetlint may write", t)
		}
	}
	return nil
}

func toList(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	return nil
}

// Templates resolves a template name to its content; the catalog provides it.
type Templates interface {
	Template(name string) ([]byte, error)
}

// Change is one proposed modification.
type Change struct {
	Path string
	Kind string // create | append | untrack | edit
	// Diff is a unified-style rendering for the dry run.
	Diff string
	// Old and New are the file's text before and after; both are empty for
	// an untrack.
	Old, New string
	// apply performs the change.
	apply func() error
}

// Nested is a further stack the templates must cover: a project in a
// subdirectory, or (with an empty Path) a second stack in the same directory.
type Nested struct {
	Path, Stack string
}

// Project is what templates are rendered for: the scope's stack and the
// projects nested below it.
type Project struct {
	Stack  string
	Nested []Nested
	// Runner is the task runner kind templates are written for (make, just,
	// task); Container is where its targets run (none, devcontainer, docker,
	// podman).
	Runner, Container string
	// Vars are the values for placeholders in `set` actions, e.g. license.
	Vars map[string]string
}

// Composer is implemented by template sources that can assemble a file for
// a project (its stacks, its dev container flag); ok is false when the named
// template is a plain file. A nil body with ok means this project does not
// get the file.
type Composer interface {
	Compose(name string, p Project) (body []byte, ok bool, err error)
}

// Plan computes the changes for a rule's actions against a repository scope
// without touching anything.
func Plan(r *repo.Repo, stack string, actions []Action, tpl Templates, findings []model.Finding) ([]Change, error) {
	return PlanFor(r, Project{Stack: stack}, actions, tpl, findings)
}

// PlanFor is Plan for a project with nested projects.
func PlanFor(r *repo.Repo, p Project, actions []Action, tpl Templates, findings []model.Finding) ([]Change, error) {
	var out []Change
	for _, a := range actions {
		changes, err := planAction(r, p, a, tpl, findings)
		if err != nil {
			return nil, err
		}
		out = append(out, changes...)
		if len(a.Gitignore) > 0 {
			if c := planGitignore(r, a.Gitignore); c != nil {
				out = append(out, *c)
			}
		}
	}
	return out, nil
}

// planAction plans the one main step of an action.
func planAction(r *repo.Repo, p Project, a Action, tpl Templates, findings []model.Finding) ([]Change, error) {
	var c *Change
	var err error
	switch {
	case a.Template != "":
		c, err = planTemplate(r, p, a, tpl)
	case a.Untrack != "":
		return planUntrack(r, a.Untrack, findings), nil
	case a.Set != nil:
		c, err = planSet(r, *a.Set, p.Vars)
	case a.Append != nil:
		c, err = planAppend(r, *a.Append, p, tpl)
	case a.Merge != nil:
		c, err = planMerge(r, *a.Merge, p, tpl)
	}
	if err != nil || c == nil {
		return nil, err
	}
	return []Change{*c}, nil
}

// Apply performs planned changes in order and stops at the first error.
func Apply(changes []Change) error {
	for _, c := range changes {
		if c.apply == nil {
			continue
		}
		if err := c.apply(); err != nil {
			return fmt.Errorf("%s %s: %w", c.Kind, c.Path, err)
		}
	}
	return nil
}

// render returns a template's content, assembled for the project when the
// source can do that.
func render(tpl Templates, name string, p Project) ([]byte, error) {
	if c, ok := tpl.(Composer); ok {
		if body, composed, err := c.Compose(name, p); composed {
			return body, err
		}
	}
	return tpl.Template(name)
}

func planTemplate(r *repo.Repo, p Project, a Action, tpl Templates) (*Change, error) {
	name := strings.ReplaceAll(a.Template, "{stack}", p.Stack)
	name = strings.ReplaceAll(name, "{taskrunner}", repo.RunnerFile(p.Runner))
	// A repository without a stack (documentation, configuration) still gets
	// the templates that are assembled, in their stack-less form; a template
	// that exists only per stack has nothing to offer it.
	perStackOnly := strings.Contains(a.Template, "{stack}") && p.Stack == ""
	dest := a.To
	if dest == "" {
		dest = filepath.Base(name)
	}
	if r.Has(dest) {
		return nil, nil // never overwrite
	}
	body, err := render(tpl, name, p)
	if perStackOnly && err != nil {
		return nil, nil //nolint:nilerr // no stack-less form of this template: the rule is then not mechanically fixable
	}
	if err != nil || body == nil {
		return nil, err
	}
	return &Change{
		Path: dest, Kind: "create", Diff: renderCreate(dest, string(body)), New: string(body),
		apply: func() error { return r.WriteFile(dest, body, true) },
	}, nil
}

func planUntrack(r *repo.Repo, pattern string, findings []model.Finding) []Change {
	paths := map[string]bool{}
	for _, f := range findings {
		if f.Path != "" && (pattern == "*" || repo.MatchGlob(pattern, f.Path)) {
			paths[f.Path] = true
		}
	}
	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)
	out := make([]Change, 0, len(sorted))
	for _, p := range sorted {
		p := p
		repoRel := p
		if r.Scope != "" {
			repoRel = r.Scope + "/" + p
		}
		out = append(out, Change{
			Path: p, Kind: "untrack", Diff: "- tracked: " + p + " (file stays on disk)\n",
			apply: func() error {
				_, err := r.Git("--literal-pathspecs", "rm", "--cached", "--quiet", "--", repoRel)
				return err
			},
		})
	}
	return out
}

func planGitignore(r *repo.Repo, patterns []string) *Change {
	existing, _ := r.Text(".gitignore")
	add := missingLines(existing, patterns)
	if len(add) == 0 {
		return nil
	}
	block := strings.Join(add, "\n") + "\n"
	prefix := ""
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		prefix = "\n"
	}
	return &Change{
		Path: ".gitignore", Kind: "append", Diff: renderAppend(".gitignore", block), Old: existing, New: existing + prefix + block,
		apply: func() error {
			current, _ := r.ReadFile(".gitignore")
			missing := missingLines(string(current), add)
			if len(missing) == 0 {
				return nil // an earlier change in this plan added them
			}
			prefix := ""
			if len(current) > 0 && !strings.HasSuffix(string(current), "\n") {
				prefix = "\n"
			}
			return r.AppendFile(".gitignore", []byte(prefix+strings.Join(missing, "\n")+"\n"))
		},
	}
}

// missingLines returns the patterns that are not a line of text yet.
func missingLines(text string, patterns []string) []string {
	have := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		have[strings.TrimSpace(line)] = true
	}
	var out []string
	for _, p := range patterns {
		if !have[p] {
			out = append(out, p)
			have[p] = true
		}
	}
	return out
}

func renderCreate(path, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "+++ %s (new file, %d lines)\n", path, strings.Count(body, "\n"))
	for i, line := range strings.SplitAfter(body, "\n") {
		if i >= 12 {
			b.WriteString("+ (more lines)\n")
			break
		}
		if line != "" {
			b.WriteString("+ " + strings.TrimRight(line, "\n") + "\n")
		}
	}
	return b.String()
}

func renderAppend(path, block string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "~~~ %s (append)\n", path)
	for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
		b.WriteString("+ " + line + "\n")
	}
	return b.String()
}
