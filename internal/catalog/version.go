package catalog

import "runtime/debug"

// ModulePath is the Go module the embedded presets and templates come from.
const ModulePath = "github.com/fleetlint/catalog"

// ModuleVersion returns the version of the catalog module this binary was
// built against, as the build recorded it: a tag, or for an untagged commit
// a pseudo-version ending in the commit. A binary built against a local
// checkout of the catalog says so.
func ModuleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep.Path != ModulePath {
			continue
		}
		if dep.Replace != nil || dep.Version == "" || dep.Version == "(devel)" {
			return "local checkout"
		}
		return dep.Version
	}
	return "unknown"
}
