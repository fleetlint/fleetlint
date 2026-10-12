package testutil

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// JSONKeys lists the key paths of a JSON document, one per line, sorted;
// array elements are written as []. Values do not matter: this pins the
// shape of an output format. Below the data paths given (maps keyed by
// rule id, fact sources) only the path itself is listed, since their keys
// come from the catalog, not the format.
func JSONKeys(t *testing.T, doc []byte, data ...string) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(doc, &v); err != nil {
		t.Fatal(err)
	}
	set := map[string]bool{}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch x := v.(type) {
		case map[string]any:
			if slices.Contains(data, prefix) {
				return
			}
			for k, val := range x {
				set[prefix+"."+k] = true
				walk(prefix+"."+k, val)
			}
		case []any:
			for _, val := range x {
				walk(prefix+"[]", val)
			}
		}
	}
	walk("", v)
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return strings.Join(keys, "\n") + "\n"
}

// Golden compares got with the file at path; UPDATE_GOLDEN=1 rewrites it.
func Golden(t *testing.T, path, got string) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path) //nolint:gosec // a test's own golden file
	if err != nil {
		t.Fatalf("%v (run with UPDATE_GOLDEN=1 to create it)", err)
	}
	if string(want) != got {
		t.Errorf("%s changed: a format change needs a new version and a CHANGELOG entry; regenerate with UPDATE_GOLDEN=1.\ngot:\n%s", path, got)
	}
}
