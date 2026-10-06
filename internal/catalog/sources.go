package catalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

// Layers say who owns a catalog or an override. Catalogs resolve in this
// order, so a later layer overrides an earlier one within the earlier
// layer's locks and floors.
const (
	LayerPreset = "preset"
	LayerOrg    = "org"
	LayerTeam   = "team"
	LayerRepo   = "repo"
)

var layerOrder = []string{LayerPreset, LayerOrg, LayerTeam, LayerRepo}

var teamAliasRe = regexp.MustCompile(`^team/[a-z0-9][a-z0-9._-]*$`)

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

// AliasLayer returns the layer an alias belongs to: `org` is the
// organization baseline, `team/<name>` a team catalog.
func AliasLayer(alias string) (string, bool) {
	switch {
	case alias == LayerOrg:
		return LayerOrg, true
	case teamAliasRe.MatchString(alias):
		return LayerTeam, true
	}
	return "", false
}

// RefLayer returns the layer of a reference written directly in `extends`.
func RefLayer(ref string) string {
	if strings.HasPrefix(ref, presetPrefix) {
		return LayerPreset
	}
	return LayerRepo
}

// LayerRank orders layers for resolution; unknown layers sort last.
func LayerRank(layer string) int {
	for i, l := range layerOrder {
		if l == layer {
			return i
		}
	}
	return len(layerOrder)
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
		local = ref
		if !filepath.IsAbs(local) {
			local = filepath.Join(l.BaseDir, local)
		}
		b, err = os.ReadFile(local) //nolint:gosec // the path comes from the repository's own .fleetlint.yaml
	}
	if err != nil {
		return nil, fmt.Errorf("sources %s: %w", ref, err)
	}
	var s Sources
	if err := yaml.UnmarshalWithOptions(b, &s, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("sources %s: %w", ref, err)
	}
	if err := s.validate(local); err != nil {
		return nil, fmt.Errorf("sources %s: %w", ref, err)
	}
	return &s, nil
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
		if _, ok := AliasLayer(alias); !ok {
			return fmt.Errorf("catalogs.%s: names are `org` or `team/<name>`", alias)
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
	if s.Catalog != nil && s.Catalog.Version == "" {
		return errors.New("catalog.version is required: a tag or a full commit SHA")
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
