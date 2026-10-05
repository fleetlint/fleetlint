package repo

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/goccy/go-yaml"
)

// Format names a structured document syntax.
type Format string

// Supported document formats.
const (
	FormatYAML Format = "yaml"
	FormatTOML Format = "toml"
	FormatJSON Format = "json"
)

// Doc parses a structured file into generic Go values (map[string]any,
// []any, scalars) suitable for CEL. Results are cached per path. A missing
// file yields nil, false; a present but unparsable file yields an error so a
// rule can distinguish "absent" from "broken".
func (r *Repo) Doc(format Format, rel string) (any, bool, error) {
	key := string(format) + ":" + rel
	r.mu.Lock()
	if d, ok := r.docs[key]; ok {
		r.mu.Unlock()
		return d, true, nil
	}
	r.mu.Unlock()

	text, ok := r.Text(rel)
	if !ok {
		return nil, false, nil
	}
	v, err := parseDoc(format, text)
	if err != nil {
		return nil, true, fmt.Errorf("%s: %w", rel, err)
	}
	v = normalize(v)
	r.mu.Lock()
	r.docs[key] = v
	r.mu.Unlock()
	return v, true, nil
}

func parseDoc(format Format, text string) (any, error) {
	var v any
	switch format {
	case FormatYAML:
		if err := yaml.Unmarshal([]byte(text), &v); err != nil {
			return nil, fmt.Errorf("parse yaml: %w", err)
		}
	case FormatTOML:
		m := map[string]any{}
		if err := toml.Unmarshal([]byte(text), &m); err != nil {
			return nil, fmt.Errorf("parse toml: %w", err)
		}
		v = m
	case FormatJSON:
		if err := json.Unmarshal([]byte(text), &v); err != nil {
			return nil, fmt.Errorf("parse json: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported format %q", format)
	}
	return v, nil
}

// Normalize converts parser-specific containers into map[string]any / []any
// with string keys, and every integer kind into int64, so CEL sees one shape
// whether a value came from a document or from rule params.
func Normalize(v any) any { return normalize(v) }

func normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = normalize(val)
		}
		return t
	case map[any]any:
		m := make(map[string]any, len(t))
		for k, val := range t {
			m[fmt.Sprint(k)] = normalize(val)
		}
		return m
	case []any:
		for i, val := range t {
			t[i] = normalize(val)
		}
		return t
	case float64:
		if t == float64(int64(t)) {
			return int64(t)
		}
		return t
	case uint64:
		if t > math.MaxInt64 {
			return float64(t)
		}
		return int64(t)
	case int:
		return int64(t)
	}
	return v
}

var makeTargetRe = regexp.MustCompile(`(?m)^([A-Za-z0-9_.][A-Za-z0-9_./ -]*?)\s*:(?:[^=:]|$)`)

// Makefile describes the targets declared in a Makefile.
type Makefile struct {
	Targets []string
	// Deps maps a target to its prerequisites as written on the rule line.
	Deps map[string][]string
	// Recipes maps a target to its recipe lines.
	Recipes map[string][]string
}

// Makefile parses targets, prerequisites and recipes from a Makefile. It is a
// structural reader, not a make implementation: includes and conditionals
// are not expanded, which is enough to check a task-runner contract.
func (r *Repo) Makefile(rel string) (*Makefile, bool) {
	text, ok := r.Text(rel)
	if !ok {
		return nil, false
	}
	return parseMakefile(text), true
}

func parseMakefile(text string) *Makefile {
	mf := &Makefile{Deps: map[string][]string{}, Recipes: map[string][]string{}}
	var current string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "\t") && current != "" {
			mf.Recipes[current] = append(mf.Recipes[current], strings.TrimSpace(line))
			continue
		}
		if m := makeTargetRe.FindStringSubmatch(line); m != nil && !strings.HasPrefix(m[1], ".") {
			current = mf.addTargets(strings.Fields(m[1]), parseDeps(line))
			continue
		}
		if strings.TrimSpace(line) == "" {
			current = ""
		}
	}
	sort.Strings(mf.Targets)
	return mf
}

// addTargets records the targets of one rule line and returns the one that
// owns the following recipe lines.
func (mf *Makefile) addTargets(names, deps []string) string {
	for _, name := range names {
		if _, seen := mf.Deps[name]; !seen {
			mf.Targets = append(mf.Targets, name)
		}
		mf.Deps[name] = append(mf.Deps[name], deps...)
	}
	return names[len(names)-1]
}

func parseDeps(line string) []string {
	after := line[strings.Index(line, ":")+1:]
	if i := strings.Index(after, "#"); i >= 0 {
		after = after[:i]
	}
	return strings.Fields(after)
}
