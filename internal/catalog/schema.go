package catalog

import _ "embed"

// CatalogSchema and SourcesSchema are the JSON Schemas for catalog files
// and sources files, served by `fleetlint schema catalog` and `fleetlint
// schema sources`.
var (
	//go:embed catalog.schema.json
	CatalogSchema []byte
	//go:embed sources.schema.json
	SourcesSchema []byte
)
