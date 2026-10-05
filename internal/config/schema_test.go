package config_test

import (
	"encoding/json"
	"testing"

	"github.com/fleetlint/fleetlint/internal/config"
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
