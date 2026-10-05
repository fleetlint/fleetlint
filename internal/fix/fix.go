// Package fix applies the declarative fix actions a rule carries. It is a
// dry run unless told otherwise: it computes the changes, renders them as a
// diff, and writes nothing. Actions are deliberately few and mechanical;
// anything else is a prompt for a person or an agent.
package fix

import (
	"fmt"
	"os"
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
		if a.Template == "" && a.Untrack == "" && len(a.Gitignore) == 0 && a.Set == nil {
			return nil, fmt.Errorf("actions[%d]: empty action", i)
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
	default:
		return fmt.Errorf("unknown key %q (want template, to, untrack, gitignore, set)", key)
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
	stack := p.Stack
	name := strings.ReplaceAll(a.Template, "{stack}", stack)
	name = strings.ReplaceAll(name, "{taskrunner}", repo.RunnerFile(p.Runner))
	if strings.Contains(a.Template, "{stack}") && stack == "" {
		return nil, fmt.Errorf("template %s needs a stack, but none was detected", a.Template)
	}
	dest := a.To
	if dest == "" {
		dest = filepath.Base(name)
	}
	if r.Has(dest) {
		return nil, nil // never overwrite
	}
	body, err := render(tpl, name, p)
	if err != nil || body == nil {
		return nil, err
	}
	abs, err := r.Abs(dest)
	if err != nil {
		return nil, err
	}
	return &Change{
		Path: dest, Kind: "create", Diff: renderCreate(dest, string(body)),
		apply: func() error {
			if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
				return err
			}
			return os.WriteFile(abs, body, 0o644) //nolint:gosec // project files must be readable by other tools
		},
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
				_, err := r.Git("rm", "--cached", "--quiet", "--", repoRel)
				return err
			},
		})
	}
	return out
}

func planGitignore(r *repo.Repo, patterns []string) *Change {
	existing, _ := r.Text(".gitignore")
	have := map[string]bool{}
	for _, line := range strings.Split(existing, "\n") {
		have[strings.TrimSpace(line)] = true
	}
	var add []string
	for _, p := range patterns {
		if !have[p] {
			add = append(add, p)
		}
	}
	if len(add) == 0 {
		return nil
	}
	abs, err := r.Abs(".gitignore")
	if err != nil {
		return nil
	}
	block := strings.Join(add, "\n") + "\n"
	return &Change{
		Path: ".gitignore", Kind: "append", Diff: renderAppend(".gitignore", block),
		apply: func() error {
			prefix := ""
			if existing != "" && !strings.HasSuffix(existing, "\n") {
				prefix = "\n"
			}
			f, err := os.OpenFile(abs, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec // .gitignore is a shared project file
			if err != nil {
				return err
			}
			defer f.Close() //nolint:errcheck // close error after a successful write is not actionable here
			_, err = f.WriteString(prefix + block)
			return err
		},
	}
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
