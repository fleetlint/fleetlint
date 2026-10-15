package config_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

func TestSchemaIsValidJSON(t *testing.T) {
	t.Parallel()
	var v map[string]any
	if err := json.Unmarshal(config.Schema, &v); err != nil {
		t.Fatal(err)
	}
	props, _ := v["properties"].(map[string]any)
	for _, key := range []string{"version", "catalog", "sources", "extends", "facts", "scopes", "rules", "exceptions", "baseline"} {
		if _, ok := props[key]; !ok {
			t.Errorf("schema lacks top-level key %q that the loader accepts", key)
		}
	}
}

// The schema and the Go types the loader decodes into describe the same
// keys, in both directions and at every level, so the editor support the
// schema gives can neither reject a valid file nor accept a key the loader
// refuses.
func TestSchemaMatchesTheTypes(t *testing.T) {
	t.Parallel()
	testutil.SchemaMatches(t, reflect.TypeFor[config.File](), config.Schema, refusedHere)
}

// refused are keys the shared types decode but a repository's file may not
// set: catalogs use them, the loader rejects them here with an explanation,
// and the schema leaves them out so an editor flags them as well.
var refused = map[string]bool{
	"locked": true, "min_severity": true, "exceptions": true, "when": true, "tiers": true,
}

func refusedHere(at, name string) bool {
	switch {
	case strings.HasSuffix(at, "overrides.*"):
		return refused[name]
	case at == ".rules[]":
		return name == "locked" || name == "min_severity" || name == "exceptions" || name == "combine"
	}
	return false
}
