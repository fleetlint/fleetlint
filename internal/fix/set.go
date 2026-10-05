package fix

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/fleetlint/fleetlint/internal/repo"
)

// SetAction adds one string value to an existing JSON or TOML file. It never
// changes a key that is already there and never creates the file.
type SetAction struct {
	// File is the manifest, relative to the scope: *.json or *.toml.
	File string `yaml:"file" json:"file"`
	// Key is a top-level key for JSON, `table.key` for TOML.
	Key string `yaml:"key" json:"key"`
	// Value is the string to set; `{license}` is the SPDX identifier of the
	// repository's LICENSE file.
	Value string `yaml:"value" json:"value"`
}

func decodeSet(v any) (*SetAction, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("set: want a map with file, key and value")
	}
	var s SetAction
	for k, val := range m {
		str, _ := val.(string)
		switch k {
		case "file":
			s.File = str
		case "key":
			s.Key = str
		case "value":
			s.Value = str
		default:
			return nil, fmt.Errorf("set: unknown key %q (want file, key, value)", k)
		}
	}
	ext := filepath.Ext(s.File)
	if s.File == "" || s.Key == "" || s.Value == "" || (ext != ".json" && ext != ".toml") {
		return nil, fmt.Errorf("set: file (*.json or *.toml), key and value are required")
	}
	return &s, nil
}

var placeholderRe = regexp.MustCompile(`\{[a-z]+\}`)

// planSet returns the edit, or nil when there is nothing to do: the file is
// absent, the key is already set, or a placeholder has no value here.
func planSet(r *repo.Repo, s SetAction, vars map[string]string) (*Change, error) {
	value := s.Value
	for name, v := range vars {
		value = strings.ReplaceAll(value, "{"+name+"}", v)
	}
	old, ok := r.Text(s.File)
	if !ok || placeholderRe.MatchString(value) {
		return nil, nil
	}
	var updated, added string
	var err error
	if filepath.Ext(s.File) == ".json" {
		updated, added, err = setJSON(old, s.Key, value)
	} else {
		updated, added, err = setTOML(old, s.Key, value)
	}
	if err != nil || updated == "" {
		return nil, err
	}
	abs, err := r.Abs(s.File)
	if err != nil {
		return nil, err
	}
	return &Change{
		Path: s.File, Kind: "edit", Diff: fmt.Sprintf("~~~ %s (insert)\n+ %s\n", s.File, strings.TrimSpace(added)),
		apply: func() error {
			return os.WriteFile(abs, []byte(updated), 0o644) //nolint:gosec // project manifests must be readable by other tools
		},
	}, nil
}

var firstIndentRe = regexp.MustCompile(`^\n([ \t]+)"`)

var jsonAnchorRe = regexp.MustCompile(`(?m)^(\s+)"(?:name|version)"\s*:.*,\s*$`)

// setJSON inserts a top-level member, after `name`/`version` when the file
// is laid out one member per line, else right after the opening brace. The
// result is accepted only if it parses to the old document plus the key.
func setJSON(old, key, value string) (updated, added string, err error) {
	var before map[string]any
	if json.Unmarshal([]byte(old), &before) != nil {
		return "", "", nil //nolint:nilerr // JSON with comments or a non-object is left to a person, not reported as a fix failure
	}
	if _, exists := before[key]; exists || strings.Contains(key, ".") {
		return "", "", nil
	}
	k, _ := json.Marshal(key)
	v, _ := json.Marshal(value)
	member := string(k) + ": " + string(v)
	open := strings.Index(old, "{")
	switch anchors := jsonAnchorRe.FindAllStringSubmatchIndex(old, -1); {
	case len(anchors) > 0 && topLevelIndent(old, old[anchors[0][2]:anchors[0][3]]):
		last := anchors[len(anchors)-1]
		for _, a := range anchors {
			if old[a[2]:a[3]] == old[anchors[0][2]:anchors[0][3]] {
				last = a
			}
		}
		added = old[last[2]:last[3]] + member + ","
		updated = old[:last[1]] + "\n" + added + old[last[1]:]
	case len(before) == 0:
		added = member
		updated = old[:open] + "{\n  " + member + "\n}" + strings.TrimLeft(old[strings.LastIndex(old, "}")+1:], " \t")
	default:
		added = member + ","
		lead := ""
		if m := firstIndentRe.FindStringSubmatch(old[open+1:]); m != nil {
			lead = "\n" + m[1]
		}
		updated = old[:open+1] + lead + added + old[open+1:]
	}
	var after map[string]any
	if err := json.Unmarshal([]byte(updated), &after); err != nil {
		return "", "", fmt.Errorf("edit of JSON key %s would not parse: %w", key, err)
	}
	before[key] = value
	if !reflect.DeepEqual(before, after) {
		return "", "", fmt.Errorf("edit of JSON key %s would change other members", key)
	}
	return updated, added, nil
}

// topLevelIndent reports whether indent is the indentation of the first
// member of the document, so an anchor is not a nested object's member.
func topLevelIndent(doc, indent string) bool {
	rest := doc[strings.Index(doc, "{")+1:]
	nl := strings.Index(rest, "\n")
	if nl < 0 || strings.TrimSpace(rest[:nl]) != "" {
		return false
	}
	line := rest[nl+1:]
	return strings.HasPrefix(line, indent) && strings.HasPrefix(strings.TrimLeft(line, " \t"), `"`) && len(line)-len(strings.TrimLeft(line, " \t")) == len(indent)
}

var tomlHeaderRe = regexp.MustCompile(`^\s*\[`)

// tomlInsertLine returns the line after which a new key of the table goes:
// its last `name` or `version` line, else its header; -1 without a header.
func tomlInsertLine(lines []string, table string) int {
	for i, line := range lines {
		if strings.TrimSpace(line) != "["+table+"]" {
			continue
		}
		at := i
		for j := i + 1; j < len(lines) && !tomlHeaderRe.MatchString(lines[j]); j++ {
			if name := strings.TrimSpace(strings.SplitN(lines[j], "=", 2)[0]); name == "name" || name == "version" {
				at = j
			}
		}
		return at
	}
	return -1
}

// setTOML inserts `key = "value"` into an existing table, after its `name`
// or `version` line when present. The result is accepted only if it parses
// to the old document plus the key.
func setTOML(old, dotted, value string) (updated, added string, err error) {
	table, key, ok := strings.Cut(dotted, ".")
	if !ok || strings.Contains(key, ".") {
		return "", "", fmt.Errorf("set: TOML key %q must be table.key", dotted)
	}
	var before map[string]any
	if _, derr := toml.Decode(old, &before); derr != nil {
		return "", "", nil //nolint:nilerr // a manifest that does not parse is left to a person, not reported as a fix failure
	}
	tbl, isTable := before[table].(map[string]any)
	if _, exists := tbl[key]; !isTable || exists {
		return "", "", nil
	}
	lines := strings.Split(old, "\n")
	at := tomlInsertLine(lines, table)
	if at < 0 {
		return "", "", nil // inline or dotted table: leave it to a person
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(map[string]string{key: value}); err != nil {
		return "", "", err
	}
	added = strings.TrimSpace(buf.String())
	lines = append(lines[:at+1], append([]string{added}, lines[at+1:]...)...)
	updated = strings.Join(lines, "\n")
	var after map[string]any
	if _, err := toml.Decode(updated, &after); err != nil {
		return "", "", fmt.Errorf("edit of TOML key %s would not parse: %w", dotted, err)
	}
	tbl[key] = value
	if !reflect.DeepEqual(before, after) {
		return "", "", fmt.Errorf("edit of TOML key %s would change other keys", dotted)
	}
	return updated, added, nil
}
