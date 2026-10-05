package config

import _ "embed"

// Schema is the JSON Schema for .fleetlint.yaml, served by `fleetlint schema`
// and usable for editor completion via a yaml-language-server comment.
//
//go:embed schema.json
var Schema []byte
