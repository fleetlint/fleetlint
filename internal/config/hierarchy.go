package config

import (
	"github.com/fleetlint/fleetlint/internal/catalog"
)

// CatalogInfo describes one catalog of the run in the order it applied:
// what it is, how it was pinned, how it came in and what it contributed.
// Reports and `fleetlint hierarchy` show the list so a definition can be
// followed back to the exact catalog content afterwards.
type CatalogInfo struct {
	// Order is the catalog's position in the run, from 1; a later catalog
	// adjusts an earlier one within the earlier one's policy.
	Order int    `json:"order"`
	Ref   string `json:"ref"`
	Name  string `json:"name"`
	// Version is what the catalog calls itself; Digest the sha256 of its
	// file and Artifact the digest of the OCI artifact it came from.
	Version  string `json:"version"`
	Digest   string `json:"digest,omitempty"`
	Artifact string `json:"artifact,omitempty"`
	// Label is the catalog's label (preset, repo, or what it declares or is
	// named), Alias the sources-file name it was referenced by.
	Label string `json:"label"`
	Alias string `json:"alias,omitempty"`
	// Origin is preset, fetched (pinned by digest, commit or signature) or
	// repository (a file of the repository). Via is the extends entry that
	// brought the catalog in, for an include the entry it was included by.
	Origin string `json:"origin"`
	Via    string `json:"via,omitempty"`
	// Includes are the catalogs this one builds on, as it names them.
	Includes []string `json:"includes,omitempty"`
	// Rules, Overrides and Exceptions count what the catalog contributes.
	Rules      int `json:"rules"`
	Overrides  int `json:"overrides"`
	Exceptions int `json:"exceptions"`
}

// Origins of a catalog.
const (
	OriginPreset     = "preset"
	OriginFetched    = "fetched"
	OriginRepository = "repository"
)

// Hierarchy lists the run's catalogs in the order they applied.
func (e *Effective) Hierarchy() []CatalogInfo {
	out := make([]CatalogInfo, 0, len(e.Catalogs))
	for i, c := range e.Catalogs {
		info := CatalogInfo{
			Order: i + 1, Ref: c.Ref, Name: c.Metadata.Name, Version: c.Metadata.Version,
			Digest: c.Digest, Artifact: c.Artifact, Label: c.Label, Alias: c.Alias, Via: c.Via,
			Includes: c.Metadata.Includes, Rules: len(c.Rules), Overrides: len(c.Overrides), Exceptions: len(c.Exceptions),
		}
		switch {
		case catalog.RefLabel(c.Ref) == catalog.LabelPreset:
			info.Origin = OriginPreset
		case notFromRepository(c):
			info.Origin = OriginFetched
		default:
			info.Origin = OriginRepository
		}
		if info.Via == info.Ref {
			info.Via = ""
		}
		out = append(out, info)
	}
	return out
}
