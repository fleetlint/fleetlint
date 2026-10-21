package catalog

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/fleetlint/fleetlint/internal/catalog/fetch"

	"github.com/fleetlint/fleetlint/internal/yamlsafe"

	"github.com/goccy/go-yaml"
	"golang.org/x/mod/semver"

	"github.com/fleetlint/fleetlint/internal/rootfs"
)

// Labels say who owns a catalog, for attribution in reports and for
// `--require`. Presets are `preset` and the repository's own file is `repo`;
// any other catalog carries the label it declares in metadata.label, or the
// sources-file name it was loaded under, or its own name. Labels do not
// order catalogs: the order of includes and of `extends` does.
const (
	LabelPreset = "preset"
	LabelRepo   = "repo"
)

// aliasRe is the shape of a sources-file name: a word, optionally with
// slashes (`org`, `team/mobile`, `compliance/pci`), never a path or a URL.
var aliasRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)*$`)

// LooksLikeAlias reports whether ref has the shape of a sources-file name
// rather than of a preset, a pinned reference or a file.
func LooksLikeAlias(ref string) bool {
	if strings.HasPrefix(ref, presetPrefix) || isPinnedRef(ref) || strings.HasSuffix(ref, ".yaml") || strings.HasSuffix(ref, ".yml") {
		return false
	}
	return aliasRe.MatchString(ref)
}

// ValidLabel checks a catalog's metadata.label: one line of plain text.
func ValidLabel(label string) error {
	if strings.TrimSpace(label) != label || label == "" || len(label) > 64 || strings.ContainsAny(label, "\n\r\t") {
		return fmt.Errorf("metadata.label %q must be a short label without surrounding spaces or line breaks", label)
	}
	return nil
}

// Sources maps the names repositories write in `extends` to catalog
// references, so that URLs and pins live in one file per organization.
type Sources struct {
	Version int `yaml:"version"`
	// Catalog pins the fleetlint catalog version every repository using this
	// file runs with; a repository may repeat it, not change it.
	Catalog *Pin `yaml:"catalog,omitempty"`
	// Extends is the baseline every repository gets, in front of its own
	// extends; names from Catalogs or fleetlint:<preset>.
	Extends []string `yaml:"extends,omitempty"`
	// Signers are the identities whose cosign signature makes an oci catalog
	// trustworthy without a digest pin; an entry may name its own instead.
	Signers  []Signer         `yaml:"signers,omitempty"`
	Catalogs map[string]Entry `yaml:"catalogs"`
}

// Entry is one `catalogs:` target: a reference, or a reference with
// its own signers and a version floor.
type Entry struct {
	Ref string `yaml:"ref"`
	// Signers replace the file's signers for this catalog alone.
	Signers []Signer `yaml:"signers,omitempty"`
	// MinVersion is the lowest metadata.version the catalog may report, so
	// a tag moved back to an older release is noticed.
	MinVersion string `yaml:"min_version,omitempty"`
}

// UnmarshalYAML accepts `<reference>` and `{ref: ..., signers: ..., min_version: ...}`.
func (e *Entry) UnmarshalYAML(unmarshal func(any) error) error {
	var ref string
	if err := unmarshal(&ref); err == nil {
		*e = Entry{Ref: ref}
		return nil
	}
	type plain Entry
	var full plain
	if err := unmarshal(&full); err != nil {
		return err
	}
	*e = Entry(full)
	return nil
}

// validate checks the reference, the entry's own signers and the floor.
func (e Entry) validate() error {
	if e.Ref == "" {
		return errors.New("ref is required")
	}
	for i, sg := range e.Signers {
		if err := sg.validate(); err != nil {
			return fmt.Errorf("signers[%d]: %w", i, err)
		}
	}
	if len(e.Signers) > 0 && !strings.HasPrefix(e.Ref, ociPrefix) {
		return fmt.Errorf("signers apply to oci:// references, not to %q", e.Ref)
	}
	if e.MinVersion != "" && !semver.IsValid(semverV(e.MinVersion)) {
		return fmt.Errorf("min_version %q is not a semantic version", e.MinVersion)
	}
	return nil
}

// CheckVersion holds a catalog loaded through this entry to its floor: an
// older version means the reference was moved back.
func (e Entry) CheckVersion(alias string, c *Catalog) error {
	if e.MinVersion == "" {
		return nil
	}
	have := semverV(c.Metadata.Version)
	if !semver.IsValid(have) {
		return fmt.Errorf("catalog %s (%s) is version %q, not a semantic version, so min_version %s cannot be checked", alias, c.Ref, c.Metadata.Version, e.MinVersion)
	}
	if semver.Compare(have, semverV(e.MinVersion)) < 0 {
		return fmt.Errorf("catalog %s (%s) is version %s, an older version than the sources file allows (min_version %s); a tag may have been moved back", alias, c.Ref, c.Metadata.Version, e.MinVersion)
	}
	return nil
}

// semverV gives a version the leading v that x/mod/semver expects.
func semverV(v string) string {
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

// ForEntry returns the loader for a sources-file entry: with the entry's
// own signers when it names some, else with the file's.
func (l Loader) ForEntry(e Entry) Loader {
	if len(e.Signers) > 0 {
		l.Signers = e.Signers
	}
	return l
}

// RefLabel returns the label of a reference written directly in `extends`:
// preset for a preset, repo for anything else until the catalog says.
func RefLabel(ref string) string {
	if strings.HasPrefix(ref, presetPrefix) {
		return LabelPreset
	}
	return LabelRepo
}

// LoadSources reads a sources file from a local path, an https URL with a
// digest or a git reference. A remote sources file may only name presets
// and pinned remote catalogs; a local one may also name local paths, which
// resolve relative to it.
func (l Loader) LoadSources(ref string) (*Sources, error) {
	var (
		b     []byte
		err   error
		local string
	)
	if isPinnedRef(ref) {
		b, err = l.readPinned(ref)
	} else {
		b, local, err = l.readLocalSources(ref)
	}
	if err != nil {
		return nil, fmt.Errorf("sources %s: %w", ref, err)
	}
	var s Sources
	if err := yamlsafe.Unmarshal(b, &s, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("sources %s: %w", ref, err)
	}
	if err := s.validate(local); err != nil {
		return nil, fmt.Errorf("sources %s: %w", ref, err)
	}
	return &s, nil
}

// readLocalSources reads a sources file inside the repository and returns
// its absolute path.
func (l Loader) readLocalSources(ref string) ([]byte, string, error) {
	root, rel, err := l.openLocal(ref)
	if err != nil {
		return nil, "", err
	}
	defer root.Close() //nolint:errcheck // a read-only handle
	b, err := rootfs.ReadFile(root, rel, fetch.MaxCatalogLen)
	return b, filepath.Join(root.Name(), rel), err
}

// validate checks names and targets; local is the file's path, or "" for a
// remote sources file.
func (s *Sources) validate(local string) error {
	if s.Version != 1 {
		return fmt.Errorf("version must be 1 (got %d)", s.Version)
	}
	if len(s.Catalogs) == 0 {
		return errors.New("catalogs is empty")
	}
	if err := s.validateBaseline(); err != nil {
		return err
	}
	for _, alias := range s.Aliases() {
		entry := s.Catalogs[alias]
		target := entry.Ref
		if !aliasRe.MatchString(alias) {
			return fmt.Errorf("catalogs.%s: a name is lowercase words joined by `/` (`org`, `team/mobile`, `compliance/pci`)", alias)
		}
		if err := entry.validate(); err != nil {
			return fmt.Errorf("catalogs.%s: %w", alias, err)
		}
		switch {
		case strings.HasPrefix(target, presetPrefix), isPinnedRef(target):
		case strings.Contains(target, "://"):
			return fmt.Errorf("catalogs.%s: %q is not a catalog reference", alias, target)
		case local == "":
			return fmt.Errorf("catalogs.%s: a remote sources file cannot name the local path %q", alias, target)
		case !filepath.IsAbs(target):
			entry.Ref = filepath.Join(filepath.Dir(local), target)
			s.Catalogs[alias] = entry
		}
	}
	return nil
}

// validateBaseline checks the parts that apply to every repository using the
// file: signers, the catalog pin and the baseline extends.
func (s *Sources) validateBaseline() error {
	for i, sg := range s.Signers {
		if err := sg.validate(); err != nil {
			return fmt.Errorf("signers[%d]: %w", i, err)
		}
	}
	if s.Catalog != nil {
		if err := s.Catalog.Validate(); err != nil {
			return err
		}
	}
	if s.Catalog != nil && s.Catalog.Repo != "" {
		if err := ValidRepoURL(s.Catalog.Repo); err != nil {
			return err
		}
	}
	for _, ref := range s.Extends {
		if _, isAlias := s.Catalogs[ref]; !isAlias && !strings.HasPrefix(ref, presetPrefix) {
			return fmt.Errorf("extends: %q must be a preset or a name from catalogs", ref)
		}
	}
	return nil
}

// Aliases returns the catalog names in a stable order.
func (s *Sources) Aliases() []string {
	names := make([]string, 0, len(s.Catalogs))
	for name := range s.Catalogs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
