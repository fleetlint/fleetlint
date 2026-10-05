// Package catalog loads rule catalogs: the presets embedded in the binary,
// local files, and remote files over https or from a git repository, pinned
// by digest or commit. A catalog is data; it can
// add and tighten rules but, unless it is the repo's own file, it cannot
// declare anything that executes or weaken anything.
package catalog

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/fleetlint/fleetlint/internal/fix"
	"github.com/fleetlint/fleetlint/internal/model"
)

// APIVersion is the catalog format this binary reads.
const APIVersion = "fleetlint.org/v1"

//go:embed presets/*.yaml
var presets embed.FS

//go:embed templates
var templates embed.FS

// EmbeddedTemplates serves the fix templates shipped with the presets.
type EmbeddedTemplates struct{}

// Template returns the content of an embedded template by name, e.g.
// "editorconfig" or "go/pre-commit-config.yaml".
func (e EmbeddedTemplates) Template(name string) ([]byte, error) {
	if body, ok, err := e.Compose(name, fix.Project{}); ok {
		return body, err
	}
	b, err := templates.ReadFile("templates/" + name)
	if err != nil {
		return nil, fmt.Errorf("no template %q in this binary", name)
	}
	return b, nil
}

// Catalog is a versioned set of rules.
type Catalog struct {
	APIVersion string       `yaml:"apiVersion"`
	Kind       string       `yaml:"kind"`
	Metadata   Metadata     `yaml:"metadata"`
	Rules      []model.Rule `yaml:"rules"`
	// Ref is how the catalog was referenced; Digest is its sha256 (hex).
	Ref    string `yaml:"-"`
	Digest string `yaml:"-"`
	// Trusted is true only for the repo's own config and local rule files it
	// points to; presets and remote catalogs are untrusted.
	Trusted bool `yaml:"-"`
	// Layer is who owns the catalog (preset, org, team or repo); Alias is the
	// sources-file name it was referenced by, if any. Both are set by config.
	Layer string `yaml:"-"`
	Alias string `yaml:"-"`
}

const presetPrefix = "fleetlint:"

// isPinnedRef reports whether ref names an https or git source.
func isPinnedRef(ref string) bool {
	return strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, gitPrefix)
}

// Metadata identifies a catalog.
type Metadata struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Description string `yaml:"description,omitempty"`
	// Includes names catalogs this one builds on; they are loaded first.
	Includes []string `yaml:"includes,omitempty"`
}

// ErrDigestRequired is returned for a remote reference without a digest.
var ErrDigestRequired = errors.New("remote catalogs must be pinned with #sha256-<digest>")

// Presets returns the names of the embedded catalogs.
func Presets() []string {
	entries, _ := fs.ReadDir(presets, "presets")
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".yaml"))
	}
	sort.Strings(names)
	return names
}

// Loader resolves catalog references. Fetch is injected so tests and the
// offline hook path never touch the network.
type Loader struct {
	// BaseDir resolves relative local references (the directory of .fleetlint.yaml).
	BaseDir string
	// Fetch retrieves a remote catalog body; nil disables remote catalogs.
	Fetch func(url string) ([]byte, error)
	// FetchGit reads one file from a git repository at a tag or commit; nil
	// disables git catalogs.
	FetchGit func(repoURL, rev, path string) ([]byte, error)
}

// RemoteLoader is the Loader for commands that may use the network.
func RemoteLoader(ctx context.Context) Loader {
	return Loader{Fetch: HTTPFetch(ctx), FetchGit: GitFetch(ctx)}
}

// Load resolves one reference: `fleetlint:<preset>`, a local path (file or
// directory of *.yaml), an https URL with `#sha256-<digest>`, or
// `git+<url>//<path>@<tag or commit>`.
func (l Loader) Load(ref string) ([]*Catalog, error) {
	return l.load(ref, map[string]bool{})
}

func (l Loader) load(ref string, seen map[string]bool) ([]*Catalog, error) {
	if seen[ref] {
		return nil, fmt.Errorf("catalog %s includes itself (cycle)", ref)
	}
	seen[ref] = true
	own, err := l.loadOne(ref)
	if err != nil {
		return nil, err
	}
	out := make([]*Catalog, 0, len(own))
	for _, c := range own {
		for _, inc := range c.Metadata.Includes {
			// An untrusted catalog may only include presets or other pinned remotes.
			if !c.Trusted && !strings.HasPrefix(inc, presetPrefix) && !isPinnedRef(inc) {
				return nil, fmt.Errorf("catalog %s: include %q must be a preset, an https URL or a git reference", ref, inc)
			}
			deps, err := l.load(inc, seen)
			if err != nil {
				return nil, fmt.Errorf("catalog %s: %w", ref, err)
			}
			out = append(out, deps...)
		}
		out = append(out, c)
	}
	return out, nil
}

func (l Loader) loadOne(ref string) ([]*Catalog, error) {
	switch {
	case strings.HasPrefix(ref, presetPrefix):
		c, err := loadPreset(strings.TrimPrefix(ref, presetPrefix))
		if err != nil {
			return nil, err
		}
		return []*Catalog{c}, nil
	case isPinnedRef(ref):
		return l.loadPinned(ref)
	case strings.HasPrefix(ref, "http://"):
		return nil, fmt.Errorf("%s: plain http is not allowed for catalogs", ref)
	default:
		return l.loadLocal(ref)
	}
}

func loadPreset(name string) (*Catalog, error) {
	b, err := presets.ReadFile("presets/" + name + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("unknown preset %q (available: %s)", name, strings.Join(Presets(), ", "))
	}
	c, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("preset %s: %w", name, err)
	}
	c.Ref = "fleetlint:" + name
	return c, nil
}

func (l Loader) loadLocal(ref string) ([]*Catalog, error) {
	path := ref
	if !filepath.IsAbs(path) {
		path = filepath.Join(l.BaseDir, path)
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("catalog %s: %w", ref, err)
	}
	var files []string
	if st.IsDir() {
		files, _ = filepath.Glob(filepath.Join(path, "*.yaml"))
		sort.Strings(files)
	} else {
		files = []string{path}
	}
	out := make([]*Catalog, 0, len(files))
	for _, f := range files {
		b, err := os.ReadFile(f) //nolint:gosec // the path comes from the repository's own .fleetlint.yaml
		if err != nil {
			return nil, fmt.Errorf("catalog %s: %w", f, err)
		}
		c, err := Parse(b)
		if err != nil {
			return nil, fmt.Errorf("catalog %s: %w", f, err)
		}
		c.Ref, c.Trusted = ref, true
		out = append(out, c)
	}
	return out, nil
}

// loadPinned loads an https or git catalog.
func (l Loader) loadPinned(ref string) ([]*Catalog, error) {
	b, err := l.readPinned(ref)
	if err != nil {
		return nil, err
	}
	c, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("catalog %s: %w", ref, err)
	}
	c.Ref = ref
	return []*Catalog{c}, nil
}

// readPinned returns the body of an https or git reference after checking
// it against its pin.
func (l Loader) readPinned(ref string) ([]byte, error) {
	if strings.HasPrefix(ref, gitPrefix) {
		return l.readGit(ref)
	}
	url, want, ok := strings.Cut(ref, "#")
	if !ok || !strings.HasPrefix(want, "sha256-") {
		return nil, fmt.Errorf("%s: %w", ref, ErrDigestRequired)
	}
	if l.Fetch == nil {
		return nil, fmt.Errorf("%s: remote catalogs are disabled in this mode", url)
	}
	b, err := l.Fetch(url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	if err := VerifyDigest(b, strings.TrimPrefix(want, "sha256-")); err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	return b, nil
}

func (l Loader) readGit(ref string) ([]byte, error) {
	g, err := parseGitRef(ref)
	if err != nil {
		return nil, err
	}
	if l.FetchGit == nil {
		return nil, fmt.Errorf("%s: git catalogs are disabled in this mode", ref)
	}
	b, err := l.FetchGit(g.URL, g.Rev, g.Path)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", ref, err)
	}
	if g.Digest != "" {
		if err := VerifyDigest(b, g.Digest); err != nil {
			return nil, fmt.Errorf("%s: %w", ref, err)
		}
	}
	return b, nil
}

// Parse decodes and validates a catalog document.
func Parse(b []byte) (*Catalog, error) {
	var c Catalog
	if err := yaml.UnmarshalWithOptions(b, &c, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if c.APIVersion != APIVersion {
		return nil, fmt.Errorf("apiVersion %q not supported (want %s)", c.APIVersion, APIVersion)
	}
	if c.Kind != "Catalog" {
		return nil, fmt.Errorf("kind %q not supported (want Catalog)", c.Kind)
	}
	if c.Metadata.Name == "" || c.Metadata.Version == "" {
		return nil, errors.New("metadata.name and metadata.version are required")
	}
	c.Digest = Digest(b)
	if err := requireSeverity(b); err != nil {
		return nil, err
	}
	if err := validatePolicy(c.Rules); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i := range c.Rules {
		r := &c.Rules[i]
		r.Source = c.Metadata.Name + "@" + c.Metadata.Version
		if err := validateRule(r); err != nil {
			return nil, err
		}
		if seen[r.ID] {
			return nil, fmt.Errorf("rule %s: duplicate id", r.ID)
		}
		seen[r.ID] = true
	}
	return &c, nil
}

func validateRule(r *model.Rule) error {
	if r.ID == "" {
		return errors.New("rule without id")
	}
	if !strings.Contains(r.ID, "/") {
		return fmt.Errorf("rule %s: id must be namespaced as family/name", r.ID)
	}
	if r.Title == "" || r.Requirement == "" {
		return fmt.Errorf("rule %s: title and requirement are required", r.ID)
	}
	if err := validateKind(r); err != nil {
		return err
	}
	if r.Fix.Human == "" {
		return fmt.Errorf("rule %s: fix.human is required", r.ID)
	}
	if _, err := fix.Decode(r.Fix.Actions); err != nil {
		return fmt.Errorf("rule %s: fix.%w", r.ID, err)
	}
	for _, t := range r.Tiers {
		if t < 1 || t > 3 {
			return fmt.Errorf("rule %s: tier %d out of range 1..3", r.ID, t)
		}
	}
	switch r.Scope {
	case "", model.ScopeEach, model.ScopeRoot, model.ScopeMembers:
	default:
		return fmt.Errorf("rule %s: scope must be each, root or members (got %q)", r.ID, r.Scope)
	}
	return nil
}

func validateKind(r *model.Rule) error {
	switch r.Kind {
	case model.KindExpr:
		if r.Expr == "" {
			return fmt.Errorf("rule %s: kind expr needs expr", r.ID)
		}
	case model.KindGo:
		if r.Expr != "" || r.Run != "" || r.Foreach != "" {
			return fmt.Errorf("rule %s: kind go takes neither expr nor run", r.ID)
		}
	case model.KindCommand:
		if r.Run == "" {
			return fmt.Errorf("rule %s: kind command needs run", r.ID)
		}
	case model.KindOutcome:
		return validateOutcome(r)
	default:
		return fmt.Errorf("rule %s: unknown kind %q", r.ID, r.Kind)
	}
	return nil
}

func validateOutcome(r *model.Rule) error {
	if len(r.Satisfiers) == 0 {
		return fmt.Errorf("rule %s: kind outcome needs at least one satisfier", r.ID)
	}
	if r.Expr != "" || r.Foreach != "" {
		return fmt.Errorf("rule %s: kind outcome takes satisfiers, not expr", r.ID)
	}
	names := map[string]bool{}
	for i, s := range r.Satisfiers {
		if s.Name == "" || s.Expr == "" {
			return fmt.Errorf("rule %s: satisfiers[%d] needs name and expr", r.ID, i)
		}
		if names[s.Name] {
			return fmt.Errorf("rule %s: duplicate satisfier %q", r.ID, s.Name)
		}
		names[s.Name] = true
	}
	for i, p := range r.Partials {
		if p.Name == "" || p.Expr == "" || p.Message == "" {
			return fmt.Errorf("rule %s: partials[%d] needs name, expr and message", r.ID, i)
		}
	}
	return nil
}

// validatePolicy checks the organization fields: a declared floor must parse
// and the rule's own severity must not sit below it.
func validatePolicy(rules []model.Rule) error {
	for _, r := range rules {
		if r.MinSeverity == "" {
			continue
		}
		floor, err := model.ParseSeverity(r.MinSeverity)
		if err != nil {
			return fmt.Errorf("rule %s: min_severity: %w", r.ID, err)
		}
		if r.Severity < floor {
			return fmt.Errorf("rule %s: severity %s is below its own min_severity %s", r.ID, r.Severity, r.MinSeverity)
		}
	}
	return nil
}

// requireSeverity rejects rules and partials that omit severity: the zero
// value would silently mean info, which is the weakest level.
func requireSeverity(b []byte) error {
	var raw struct {
		Rules []map[string]any `yaml:"rules"`
	}
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, r := range raw.Rules {
		id, _ := r["id"].(string)
		if _, ok := r["severity"]; !ok {
			return fmt.Errorf("rule %s: severity is required (error, warning or info)", id)
		}
		partials, _ := r["partials"].([]any)
		for i, p := range partials {
			pm, _ := p.(map[string]any)
			if _, ok := pm["severity"]; !ok {
				return fmt.Errorf("rule %s: partials[%d]: severity is required", id, i)
			}
		}
	}
	return nil
}

// Digest returns the lower-case hex sha256 of a catalog body.
func Digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// VerifyDigest accepts hex or base64 (SRI-style) encodings of a sha256.
func VerifyDigest(b []byte, want string) error {
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) == strings.ToLower(want) {
		return nil
	}
	if base64.StdEncoding.EncodeToString(sum[:]) == want {
		return nil
	}
	return fmt.Errorf("digest mismatch: got sha256-%s", hex.EncodeToString(sum[:]))
}
