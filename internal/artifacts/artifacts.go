// Package artifacts reads the artifact definitions a catalog ships in
// artifacts/ and detects them in a repository. An artifact is what a release
// publishes (files on a forge release, a container image, an npm package,
// ...), recognised by the workflow steps that publish it. Release
// capabilities implement their checks per artifact id, so a new registry or
// publish action is a catalog change only.
package artifacts

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/fleetlint/fleetlint/internal/celenv"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/yamlsafe"
)

// Def is one artifact kind: artifacts/<id>.yaml in a catalog.
type Def struct {
	ID    string   `yaml:"id" json:"id"`
	Title string   `yaml:"title" json:"title"`
	Refs  []string `yaml:"refs,omitempty" json:"refs,omitempty"`
	// Summary says in words what Detect looks for, for the documentation.
	Summary string `yaml:"summary,omitempty" json:"summary,omitempty"`
	// Detect is a CEL expression yielding the CI files that publish the
	// artifact (`workflows().filter(w, ...)`); an empty list means it is not
	// published. Evaluated once per repository, at the root.
	Detect string `yaml:"detect" json:"detect"`
	// Instances, when set, is a CEL expression evaluated once per detected
	// CI file (as `item`) that yields the names of the artifacts the file
	// publishes: image references, chart paths. Each name is one published
	// artifact, checked on its own; a file that yields no name publishes
	// one named after the file. Without it the kind is one unnamed
	// artifact.
	Instances string `yaml:"instances,omitempty" json:"instances,omitempty"`
}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Load reads artifacts/*.yaml. A file system without artifacts/ (a catalog
// written before engine level 5) yields no definitions and no error.
func Load(fsys fs.FS) ([]Def, error) {
	if fsys == nil {
		return nil, nil
	}
	names, err := fs.Glob(fsys, "artifacts/*.yaml")
	if err != nil || len(names) == 0 {
		return nil, nil //nolint:nilerr // no directory is no definitions
	}
	sort.Strings(names)
	defs := make([]Def, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		d, err := loadDef(fsys, name)
		if err != nil {
			return nil, err
		}
		if seen[d.ID] {
			return nil, fmt.Errorf("%s: duplicate artifact id %q", name, d.ID)
		}
		seen[d.ID] = true
		defs = append(defs, d)
	}
	return defs, nil
}

func loadDef(fsys fs.FS, name string) (Def, error) {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return Def{}, err
	}
	var d Def
	if err := yamlsafe.Unmarshal(b, &d, yaml.Strict()); err != nil {
		return Def{}, fmt.Errorf("%s: %w", name, err)
	}
	if want := strings.TrimSuffix(path.Base(name), ".yaml"); d.ID != want {
		return Def{}, fmt.Errorf("%s: artifact id %q must be %q (the file name spells the id)", name, d.ID, want)
	}
	if err := d.validate(); err != nil {
		return Def{}, fmt.Errorf("%s: %w", name, err)
	}
	return d, nil
}

func (d *Def) validate() error {
	switch {
	case !idRe.MatchString(d.ID):
		return fmt.Errorf("artifact id %q: lowercase letters, digits and dashes", d.ID)
	case d.Title == "":
		return errors.New("title is required")
	case strings.TrimSpace(d.Detect) == "":
		return errors.New("detect is required")
	}
	if err := celenv.CheckList(d.Detect); err != nil {
		return fmt.Errorf("detect: %w", err)
	}
	if strings.TrimSpace(d.Instances) != "" {
		if err := celenv.CheckList(d.Instances); err != nil {
			return fmt.Errorf("instances: %w", err)
		}
	}
	return nil
}

// IDs lists the artifact ids in definition order.
func IDs(defs []Def) []string {
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.ID)
	}
	return out
}

// Find returns the definition of an id.
func Find(defs []Def, id string) (Def, bool) {
	for _, d := range defs {
		if d.ID == id {
			return d, true
		}
	}
	return Def{}, false
}

// Detect evaluates every definition and returns the published artifacts,
// in definition order and, within a kind, in the order their names were
// first seen. pinned replaces detection: what the pin lists, as it says.
func Detect(env *celenv.Env, defs []Def, pinned *[]facts.Artifact) ([]facts.Artifact, error) {
	if pinned != nil {
		return append([]facts.Artifact{}, *pinned...), nil
	}
	var out []facts.Artifact
	for _, d := range defs {
		files, err := d.detect(env)
		if err != nil {
			return nil, fmt.Errorf("artifact %s: detect: %w", d.ID, err)
		}
		if len(files) == 0 {
			continue
		}
		if strings.TrimSpace(d.Instances) == "" {
			out = append(out, facts.Artifact{Kind: d.ID, Workflows: files})
			continue
		}
		instances, err := d.instances(env, files)
		if err != nil {
			return nil, fmt.Errorf("artifact %s: instances: %w", d.ID, err)
		}
		out = append(out, instances...)
	}
	return out, nil
}

// detect evaluates the definition and returns the publishing CI files, sorted.
func (d Def) detect(env *celenv.Env) ([]string, error) {
	prg, err := env.CompileList(d.Detect)
	if err != nil {
		return nil, err
	}
	items, err := env.EvalList(prg, nil)
	if err != nil {
		return nil, err
	}
	return names(items), nil
}

// instances names the artifacts each publishing file publishes and merges
// them by name: one artifact per name, with every file that publishes it.
func (d Def) instances(env *celenv.Env, files []string) ([]facts.Artifact, error) {
	prg, err := env.CompileList(d.Instances)
	if err != nil {
		return nil, err
	}
	var out []facts.Artifact
	index := map[string]int{}
	for _, file := range files {
		items, err := env.EvalListWith(prg, nil, file)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		found := names(items)
		if len(found) == 0 {
			found = []string{file}
		}
		for _, name := range found {
			i, ok := index[name]
			if !ok {
				index[name] = len(out)
				out = append(out, facts.Artifact{Kind: d.ID, Name: name})
				i = index[name]
			}
			out[i].Workflows = append(out[i].Workflows, file)
		}
	}
	return out, nil
}

// names keeps the non-empty strings of a list, trimmed, each once, sorted.
func names(items []any) []string {
	var out []string
	for _, it := range items {
		s, ok := it.(string)
		s = strings.TrimSpace(s)
		if ok && s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
