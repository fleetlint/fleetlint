package fix

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/goccy/go-yaml"

	"github.com/fleetlint/fleetlint/internal/repo"
)

// AppendAction adds a template's text to the end of an existing TOML or
// YAML file when nothing in the file matches Unless: the way lint
// configuration lands in pyproject.toml or Cargo.toml without a rewrite.
// The file must exist and the result must still parse.
type AppendAction struct {
	File     string `yaml:"file" json:"file"`
	Template string `yaml:"template" json:"template"`
	Unless   string `yaml:"unless" json:"unless"`
}

// MergeAction adds the keys of a JSON template that an existing JSON file
// lacks, recursively; keys the file has keep their value. For tsconfig.json
// and the like. The file must be strict JSON.
type MergeAction struct {
	File     string `yaml:"file" json:"file"`
	Template string `yaml:"template" json:"template"`
}

func decodeAppend(v any) (*AppendAction, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("append: want a map with file, template and unless")
	}
	a := &AppendAction{}
	for k, val := range m {
		s, _ := val.(string)
		switch k {
		case "file":
			a.File = s
		case "template":
			a.Template = s
		case "unless":
			a.Unless = s
		default:
			return nil, fmt.Errorf("append: unknown key %q (want file, template, unless)", k)
		}
	}
	if a.File == "" || a.Template == "" || a.Unless == "" {
		return nil, errors.New("append: file, template and unless are required")
	}
	if _, err := regexp.Compile(a.Unless); err != nil {
		return nil, fmt.Errorf("append: unless: %w", err)
	}
	return a, nil
}

func decodeMerge(v any) (*MergeAction, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("merge: want a map with file and template")
	}
	a := &MergeAction{}
	for k, val := range m {
		s, _ := val.(string)
		switch k {
		case "file":
			a.File = s
		case "template":
			a.Template = s
		default:
			return nil, fmt.Errorf("merge: unknown key %q (want file, template)", k)
		}
	}
	if a.File == "" || a.Template == "" {
		return nil, errors.New("merge: file and template are required")
	}
	return a, nil
}

// planAppend returns the change, or nil when the file is absent or already
// has what the template would add.
func planAppend(r *repo.Repo, a AppendAction, p Project, tpl Templates) (*Change, error) {
	old, ok := r.Text(a.File)
	if !ok || regexp.MustCompile(a.Unless).MatchString(old) {
		return nil, nil
	}
	body, err := render(tpl, strings.ReplaceAll(a.Template, "{stack}", p.Stack), p)
	if err != nil {
		return nil, err
	}
	block := strings.TrimRight(string(body), "\n") + "\n"
	sep := "\n"
	if old == "" || strings.HasSuffix(old, "\n\n") {
		sep = ""
	} else if !strings.HasSuffix(old, "\n") {
		sep = "\n\n"
	}
	updated := old + sep + block
	if err := parses(a.File, updated); err != nil {
		return nil, fmt.Errorf("%s: appending %s would not parse: %w", a.File, a.Template, err)
	}
	abs, err := r.Abs(a.File)
	if err != nil {
		return nil, err
	}
	return &Change{
		Path: a.File, Kind: "append", Diff: renderAppend(a.File, block),
		apply: func() error {
			return os.WriteFile(abs, []byte(updated), 0o644) //nolint:gosec // project configuration must be readable by other tools
		},
	}, nil
}

// parses checks the whole document in the file's own format.
func parses(file, text string) error {
	var doc any
	switch filepath.Ext(file) {
	case ".toml":
		return toml.Unmarshal([]byte(text), &doc)
	case ".yaml", ".yml":
		return yaml.Unmarshal([]byte(text), &doc)
	case ".json":
		return json.Unmarshal([]byte(text), &doc)
	}
	return nil
}

// planMerge returns the change, or nil when the file is absent or has every
// key of the template. A file that is not strict JSON (comments, trailing
// commas) is reported, not rewritten.
func planMerge(r *repo.Repo, a MergeAction, p Project, tpl Templates) (*Change, error) {
	old, ok := r.Text(a.File)
	if !ok {
		return nil, nil
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(old), &doc); err != nil {
		return nil, fmt.Errorf("%s is not strict JSON (comments or trailing commas?); add the keys of template %s by hand", a.File, a.Template)
	}
	body, err := render(tpl, strings.ReplaceAll(a.Template, "{stack}", p.Stack), p)
	if err != nil {
		return nil, err
	}
	var want map[string]any
	if err := json.Unmarshal(body, &want); err != nil {
		return nil, fmt.Errorf("template %s: %w", a.Template, err)
	}
	added := mergeMissing(doc, want, "")
	if len(added) == 0 {
		return nil, nil
	}
	sort.Strings(added)
	out, err := json.MarshalIndent(doc, "", indentOf(old))
	if err != nil {
		return nil, err
	}
	updated := string(out) + "\n"
	abs, err := r.Abs(a.File)
	if err != nil {
		return nil, err
	}
	return &Change{
		Path: a.File, Kind: "edit", Diff: fmt.Sprintf("~~~ %s (insert)\n+ %s\n", a.File, strings.Join(added, "\n+ ")),
		apply: func() error {
			return os.WriteFile(abs, []byte(updated), 0o644) //nolint:gosec // project configuration must be readable by other tools
		},
	}, nil
}

// mergeMissing adds want's keys that dst lacks and returns their dotted
// paths with values; nested objects recurse, anything else is left as is.
func mergeMissing(dst, want map[string]any, prefix string) []string {
	var added []string
	for k, v := range want {
		cur, ok := dst[k]
		if !ok {
			dst[k] = v
			b, _ := json.Marshal(v)
			added = append(added, prefix+k+": "+string(b))
			continue
		}
		sub, isMap := v.(map[string]any)
		curMap, curIsMap := cur.(map[string]any)
		if isMap && curIsMap {
			added = append(added, mergeMissing(curMap, sub, prefix+k+".")...)
		}
	}
	return added
}

var jsonIndentRe = regexp.MustCompile(`\n([ \t]+)"`)

func indentOf(doc string) string {
	if m := jsonIndentRe.FindStringSubmatch(doc); m != nil {
		return m[1]
	}
	return "  "
}
