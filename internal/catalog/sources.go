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
	// trustworthy without a digest pin.
	Signers  []Signer          `yaml:"signers,omitempty"`
	Catalogs map[string]string `yaml:"catalogs"`
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
		target := s.Catalogs[alias]
		if !aliasRe.MatchString(alias) {
			return fmt.Errorf("catalogs.%s: a name is lowercase words joined by `/` (`org`, `team/mobile`, `compliance/pci`)", alias)
		}
		switch {
		case strings.HasPrefix(target, presetPrefix), isPinnedRef(target):
		case target == "" || strings.Contains(target, "://"):
			return fmt.Errorf("catalogs.%s: %q is not a catalog reference", alias, target)
		case local == "":
			return fmt.Errorf("catalogs.%s: a remote sources file cannot name the local path %q", alias, target)
		case !filepath.IsAbs(target):
			s.Catalogs[alias] = filepath.Join(filepath.Dir(local), target)
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
