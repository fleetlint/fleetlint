package testutil

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// SchemaMatches checks that a JSON Schema and the Go type the loader decodes
// into describe the same keys, in both directions and at every level.
// refused names keys the type decodes but the loader rejects in this kind
// of file; the schema leaves them out on purpose.
func SchemaMatches(t *testing.T, typ reflect.Type, schemaJSON []byte, refused func(at, name string) bool) {
	t.Helper()
	var schema map[string]any
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		t.Fatal(err)
	}
	compareKeys(t, "", typ, schema, schema, refused)
}

func compareKeys(t *testing.T, at string, typ reflect.Type, node, root map[string]any, refused func(at, name string) bool) {
	t.Helper()
	node = resolve(node, root)
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() == reflect.Slice {
		if items, ok := node["items"].(map[string]any); ok {
			compareKeys(t, at+"[]", typ.Elem(), items, root, refused)
		}
		return
	}
	if typ.Kind() == reflect.Map {
		if values, ok := node["additionalProperties"].(map[string]any); ok {
			compareKeys(t, at+".*", typ.Elem(), values, root, refused)
		}
		return
	}
	if props, ok := node["properties"].(map[string]any); ok && typ.Kind() == reflect.Struct {
		compareStruct(t, at, typ, props, root, refused)
	}
}

// compareStruct holds a struct's yaml keys and a schema's properties to
// each other and recurses into both.
func compareStruct(t *testing.T, at string, typ reflect.Type, props, root map[string]any, refused func(at, name string) bool) {
	t.Helper()
	fields := map[string]bool{}
	for i := range typ.NumField() {
		f := typ.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if name == "-" || !f.IsExported() {
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		fields[name] = true
		sub, ok := props[name].(map[string]any)
		switch {
		case ok:
			compareKeys(t, at+"."+name, f.Type, sub, root, refused)
		case refused == nil || !refused(at, name):
			t.Errorf("%s.%s: the loader reads it, the schema does not have it", at, name)
		}
	}
	for name := range props {
		if !fields[name] {
			t.Errorf("%s.%s: the schema has it, the loader does not read it", at, name)
		}
	}
}

func resolve(node, root map[string]any) map[string]any {
	ref, ok := node["$ref"].(string)
	if !ok {
		return node
	}
	cur := any(root)
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		cur = cur.(map[string]any)[part]
	}
	return cur.(map[string]any)
}
