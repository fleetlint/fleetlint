// Package stacks reads the stack definitions a catalog ships in stacks/ and
// detects them in a repository. A stack is data: the files that mark it, and
// what `fleetlint fix` needs to give it an environment. Rules implement their
// capabilities per stack id, so a new stack is a catalog change only.
package stacks

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/goccy/go-yaml"

	"github.com/fleetlint/fleetlint/internal/repo"
	"github.com/fleetlint/fleetlint/internal/yamlsafe"
)

// Def is one stack: stacks/<id>.yaml in a catalog.
type Def struct {
	ID    string   `yaml:"id" json:"id"`
	Title string   `yaml:"title" json:"title"`
	Refs  []string `yaml:"refs,omitempty" json:"refs,omitempty"`
	// Detect lists the markers; any one that matches makes the stack present.
	Detect []Marker `yaml:"detect" json:"detect"`
	// Devcontainer is how `fleetlint fix` installs the toolchain in a dev
	// container: a feature, or an image when no feature exists.
	Devcontainer Devcontainer `yaml:"devcontainer,omitempty" json:"devcontainer,omitempty"`
	// RunImage is the image targets run in with `container: docker|podman`.
	RunImage string `yaml:"run_image,omitempty" json:"run_image,omitempty"`
	// LockfileMaintenance: the package manager keeps a lockfile Renovate can refresh.
	LockfileMaintenance bool `yaml:"lockfile_maintenance,omitempty" json:"lockfile_maintenance,omitempty"`
}

// Devcontainer names the dev container feature (with options) or image.
type Devcontainer struct {
	Feature string         `yaml:"feature,omitempty" json:"feature,omitempty"`
	Options map[string]any `yaml:"options,omitempty" json:"options,omitempty"`
	Image   string         `yaml:"image,omitempty" json:"image,omitempty"`
}

// Marker is one way to recognise a stack. File is a path at the scope root;
// Glob a project file at most two directories down, outside directories that
// hold examples, fixtures or dependencies. Contains and Lacks are regular
// expressions the file's text must or must not match; With lists paths that
// must exist as well. A marker with UnlessStack does not count when one of
// those stacks is present; stacks are decided in that order.
type Marker struct {
	File        string    `yaml:"file,omitempty" json:"file,omitempty"`
	Glob        string    `yaml:"glob,omitempty" json:"glob,omitempty"`
	Contains    string    `yaml:"contains,omitempty" json:"contains,omitempty"`
	Lacks       string    `yaml:"lacks,omitempty" json:"lacks,omitempty"`
	With        []string  `yaml:"with,omitempty" json:"with,omitempty"`
	UnlessStack StackRefs `yaml:"unless_stack,omitempty" json:"unless_stack,omitempty"`

	contains, lacks *regexp.Regexp
}

// StackRefs is one stack id or a list of them.
type StackRefs []string

// UnmarshalYAML accepts `unless_stack: flutter` and `unless_stack: [flutter, java]`.
func (s *StackRefs) UnmarshalYAML(unmarshal func(any) error) error {
	var one string
	if err := unmarshal(&one); err == nil {
		*s = StackRefs{one}
		return nil
	}
	var list []string
	if err := unmarshal(&list); err != nil {
		return err
	}
	*s = list
	return nil
}

// Any is the implementation key for "every stack without its own".
const Any = "any"

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// NotProjects are directory names whose manifests never make a stack or a
// workspace member: dependencies, fixtures, examples and build output.
var NotProjects = map[string]bool{
	"vendor": true, "third_party": true, "node_modules": true, "testdata": true, "test": true, "tests": true,
	"__tests__": true, "__fixtures__": true, "__mocks__": true, "fixtures": true, "fixture": true,
	"examples": true, "example": true, "samples": true, "sample": true, "docs": true, "doc": true,
	"build": true, "dist": true, "target": true, "out": true,
}

//go:embed legacy.yaml
var legacyYAML []byte

// Legacy is the detection of catalogs written before stacks became data
// (engine level 2 and older); those catalogs ship no stacks/ directory.
func Legacy() []Def { return slices.Clone(legacy()) }

var legacy = sync.OnceValue(func() []Def {
	var defs []Def
	if err := yamlsafe.Unmarshal(legacyYAML, &defs, yaml.Strict()); err != nil {
		panic("stacks: legacy.yaml: " + err.Error())
	}
	for i := range defs {
		if err := defs[i].validate(); err != nil {
			panic("stacks: legacy.yaml: " + err.Error())
		}
	}
	return defs
})

// Load reads stacks/*.yaml. A file system without stacks/ yields no
// definitions and no error; the caller falls back to Legacy.
func Load(fsys fs.FS) ([]Def, error) {
	if fsys == nil {
		return nil, nil
	}
	entries, err := fs.ReadDir(fsys, "stacks")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var defs []Def
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		d, err := loadDef(fsys, e.Name())
		if err != nil {
			return nil, err
		}
		defs = append(defs, d)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs, checkRefs(defs)
}

func loadDef(fsys fs.FS, name string) (Def, error) {
	p := path.Join("stacks", name)
	var d Def
	b, err := fs.ReadFile(fsys, p)
	if err != nil {
		return d, err
	}
	if err := yamlsafe.Unmarshal(b, &d, yaml.Strict()); err != nil {
		return d, fmt.Errorf("%s: %w", p, err)
	}
	if want := strings.TrimSuffix(name, ".yaml"); d.ID != want {
		return d, fmt.Errorf("%s: stack id %q must be %q (the file name spells the id)", p, d.ID, want)
	}
	if err := d.validate(); err != nil {
		return d, fmt.Errorf("%s: %w", p, err)
	}
	return d, nil
}

// checkRefs refuses an unless_stack that names no defined stack.
func checkRefs(defs []Def) error {
	for _, d := range defs {
		for _, m := range d.Detect {
			for _, s := range m.UnlessStack {
				if _, ok := Find(defs, s); !ok {
					return fmt.Errorf("stacks/%s.yaml: unless_stack names %q, which is not a defined stack", d.ID, s)
				}
			}
		}
	}
	return nil
}

func (d *Def) validate() error {
	if !idRe.MatchString(d.ID) || d.ID == Any {
		return fmt.Errorf("stack id %q: lowercase letters, digits and dashes, and not %q", d.ID, Any)
	}
	if d.Title == "" || len(d.Detect) == 0 {
		return fmt.Errorf("stack %s: title and at least one detect marker are required", d.ID)
	}
	for i := range d.Detect {
		m := &d.Detect[i]
		if (m.File == "") == (m.Glob == "") {
			return fmt.Errorf("stack %s: detect[%d] needs exactly one of file and glob", d.ID, i)
		}
		if m.Glob != "" && !repo.ValidGlob(m.Glob) {
			return fmt.Errorf("stack %s: detect[%d]: glob %q is not valid", d.ID, i, m.Glob)
		}
		var err error
		if m.contains, err = compileOptional(m.Contains); err != nil {
			return fmt.Errorf("stack %s: detect[%d]: %w", d.ID, i, err)
		}
		if m.lacks, err = compileOptional(m.Lacks); err != nil {
			return fmt.Errorf("stack %s: detect[%d]: %w", d.ID, i, err)
		}
	}
	return nil
}

func compileOptional(rx string) (*regexp.Regexp, error) {
	if rx == "" {
		return nil, nil
	}
	return regexp.Compile(rx)
}

// IDs returns the stack ids in order.
func IDs(defs []Def) []string {
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.ID)
	}
	return out
}

// Find returns the definition of a stack.
func Find(defs []Def, id string) (Def, bool) {
	for _, d := range defs {
		if d.ID == id {
			return d, true
		}
	}
	return Def{}, false
}

// Detect returns the sorted ids of the stacks present at the repository's
// (scope) root. A stack is decided after the stacks its markers' unless_stack
// name, so "java unless kotlin" sees kotlin's result; a cycle is decided
// as if those stacks were absent.
func Detect(r *repo.Repo, defs []Def) []string {
	found, decided := map[string]bool{}, map[string]bool{}
	for len(decided) < len(defs) {
		if decideReady(r, defs, found, decided) {
			continue
		}
		for _, d := range defs { // a cycle: decide the rest without waiting
			if !decided[d.ID] {
				found[d.ID], decided[d.ID] = d.present(r, found), true
			}
		}
	}
	out := []string{}
	for id, ok := range found {
		if ok {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// decideReady decides every stack whose unless_stack stacks are decided and
// reports whether it decided any.
func decideReady(r *repo.Repo, defs []Def, found, decided map[string]bool) bool {
	progress := false
	for _, d := range defs {
		if !decided[d.ID] && d.ready(decided) {
			found[d.ID] = d.present(r, found)
			decided[d.ID], progress = true, true
		}
	}
	return progress
}

// ready reports whether every stack the definition's markers defer to is decided.
func (d Def) ready(decided map[string]bool) bool {
	for _, m := range d.Detect {
		for _, s := range m.UnlessStack {
			if !decided[s] && s != d.ID {
				return false
			}
		}
	}
	return true
}

func (d Def) present(r *repo.Repo, found map[string]bool) bool {
	return slices.ContainsFunc(d.Detect, func(m Marker) bool {
		return !slices.ContainsFunc(m.UnlessStack, func(s string) bool { return found[s] }) && m.matches(r)
	})
}

// Manifests are the names that mark a directory as a project, for
// workspace member discovery: every file marker and every glob marker,
// which may hold wildcards (`*.csproj`). A marker that needs another file
// (`with`) is a hint, not a manifest: Go sources next to go.mod.
func Manifests(defs []Def) []string {
	var out []string
	for _, d := range defs {
		for _, m := range d.Detect {
			if len(m.With) > 0 {
				continue
			}
			name := m.File + m.Glob
			if !slices.Contains(out, name) {
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

func (m Marker) matches(r *repo.Repo) bool {
	for _, w := range m.With {
		if !r.Has(w) {
			return false
		}
	}
	if m.File != "" {
		return r.Has(m.File) && m.content(r, m.File)
	}
	for _, f := range r.Glob(m.Glob) {
		if projectFile(f) && m.content(r, f) {
			return true
		}
	}
	return false
}

func (m Marker) content(r *repo.Repo, p string) bool {
	if m.contains == nil && m.lacks == nil {
		return true
	}
	text, ok := r.Text(p)
	if !ok {
		return false
	}
	if m.contains != nil && !m.contains.MatchString(text) {
		return false
	}
	return m.lacks == nil || !m.lacks.MatchString(text)
}

// projectFile accepts a file at most two directories down (src/App/App.csproj)
// outside directories that hold examples, fixtures or dependencies, so a
// testdata/x.csproj does not make a repository .NET.
func projectFile(f string) bool {
	dirs := strings.Split(f, "/")
	dirs = dirs[:len(dirs)-1]
	return len(dirs) <= 2 && !slices.ContainsFunc(dirs, func(d string) bool { return NotProjects[d] || strings.HasPrefix(d, ".") })
}
