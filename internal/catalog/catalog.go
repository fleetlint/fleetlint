// Package catalog loads rule catalogs: the presets embedded in the binary
// (from the module github.com/fleetlint/catalog),
// local files, and remote files over https or from a git repository, pinned
// by digest or commit. A catalog is data; it can
// add and tighten rules but, unless it is the repo's own file, it cannot
// declare anything that executes or weaken anything.
package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	data "github.com/fleetlint/catalog"
	"github.com/goccy/go-yaml"

	"github.com/fleetlint/fleetlint/internal/fix"
	"github.com/fleetlint/fleetlint/internal/model"
)

// APIVersion is the catalog format this binary reads.
const APIVersion = "fleetlint.org/v1"

// EngineLevel counts the changes to what rules can rely on (facts,
// functions, fix actions) that an older catalog or an older binary would
// not survive. A catalog states the level it was written for; this binary
// runs catalogs from MinEngineLevel to EngineLevel.
const (
	EngineLevel    = 2
	MinEngineLevel = 1
)

// checkEngine refuses a catalog this binary cannot run correctly, with the
// way out, instead of letting its rules fail one by one.
func checkEngine(c *Catalog) error {
	switch e := c.Metadata.Engine; {
	case e == 0:
		return nil
	case e > EngineLevel:
		return fmt.Errorf("catalog %s %s needs engine level %d and this fleetlint has %d: upgrade fleetlint, or use an older catalog version", c.Metadata.Name, c.Metadata.Version, e, EngineLevel)
	case e < MinEngineLevel:
		return fmt.Errorf("catalog %s %s was written for engine level %d and this fleetlint runs %d to %d: use a newer catalog version, or an older fleetlint", c.Metadata.Name, c.Metadata.Version, e, MinEngineLevel, EngineLevel)
	}
	return nil
}

// Source is one version of the fleetlint catalog: the presets that
// `fleetlint:<name>` refers to and the templates fixes write. The binary
// carries one (the module version go.mod names); a repository can pin
// another, which is fetched once and kept in the user's cache.
type Source struct {
	// Presets holds presets/<name>.yaml, Templates holds templates/..., Rules
	// holds rules/<family>/<name>.yaml, the library presets select from.
	Presets, Templates, Rules fs.FS
	// Version is how reports name this source.
	Version string
	// Pinned is true when the repository chose the version.
	Pinned bool
}

// Builtin returns the catalog the binary was built with.
func Builtin() *Source {
	return &Source{Presets: data.Presets, Templates: data.Templates, Rules: data.Rules, Version: ModuleVersion()}
}

// Describe names the source for a report.
func (s *Source) Describe() string {
	if s.Pinned {
		return s.Version + ", pinned in " + ConfigName
	}
	return s.Version
}

// ConfigName is the configuration file a pin lives in.
const ConfigName = ".fleetlint.yaml"

// FixTemplates serves the source's templates to the fix planner.
func (s *Source) FixTemplates() Templates { return Templates{fsys: s.Templates} }

// FixTemplatesOver serves the source's templates with the given file
// systems in front: the first that has a file wins.
func (s *Source) FixTemplatesOver(layers ...fs.FS) Templates {
	if len(layers) == 0 {
		return s.FixTemplates()
	}
	return Templates{fsys: overlayFS(append(layers, s.Templates))}
}

// overlayFS reads from the first layer that has the file.
type overlayFS []fs.FS

func (o overlayFS) Open(name string) (fs.File, error) {
	for _, layer := range o {
		f, err := layer.Open(name)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// PresetNames lists the presets of the source.
func (s *Source) PresetNames() []string {
	entries, _ := fs.ReadDir(s.Presets, "presets")
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".yaml"))
	}
	sort.Strings(names)
	return names
}

// Templates resolves template names against one catalog source.
type Templates struct {
	fsys fs.FS
}

// Template returns the content of a template by name, e.g. "editorconfig"
// or "go/pre-commit-config.yaml".
func (t Templates) Template(name string) ([]byte, error) {
	if body, ok, err := t.Compose(name, fix.Project{}); ok {
		return body, err
	}
	b, err := fs.ReadFile(t.fsys, "templates/"+name)
	if err != nil {
		return nil, fmt.Errorf("no template %q in this catalog", name)
	}
	return b, nil
}

// EmbeddedTemplates serves the templates of the built-in catalog.
type EmbeddedTemplates struct{}

// Template returns a built-in template by name.
func (EmbeddedTemplates) Template(name string) ([]byte, error) {
	return Builtin().FixTemplates().Template(name)
}

// Compose assembles a built-in template for a project.
func (EmbeddedTemplates) Compose(name string, p fix.Project) ([]byte, bool, error) {
	return Builtin().FixTemplates().Compose(name, p)
}

// Catalog is a versioned set of rules.
type Catalog struct {
	APIVersion string       `yaml:"apiVersion"`
	Kind       string       `yaml:"kind"`
	Metadata   Metadata     `yaml:"metadata"`
	Rules      []model.Rule `yaml:"rules"`
	// Overrides adjust rules this catalog includes without restating them.
	// A key is a rule id or a glob over ids ("*", "release/*"); globs apply
	// first, so an exact key refines them.
	Overrides map[string]Override `yaml:"overrides,omitempty"`
	// Exceptions are per-finding exceptions the catalog grants to every
	// repository that extends it, bound by `exceptions: false` set earlier.
	Exceptions []model.Exception `yaml:"exceptions,omitempty"`
	// Ref is how the catalog was referenced; Digest is its sha256 (hex).
	Ref    string `yaml:"-"`
	Digest string `yaml:"-"`
	// Trusted is true only for the repo's own config and local rule files it
	// points to; presets and remote catalogs are untrusted.
	Trusted bool `yaml:"-"`
	// Templates holds fix templates shipped with the catalog (templates/...),
	// which overlay the preset ones; nil when the catalog brings none.
	Templates fs.FS `yaml:"-"`
	// Layer is who owns the catalog (preset, org, team or repo); Alias is the
	// sources-file name it was referenced by, if any. Both are set by config.
	Layer string `yaml:"-"`
	Alias string `yaml:"-"`
	// Library holds rules/<family>/<name>.yaml shipped with the catalog (a
	// git tree or an OCI artifact); its `use:` entries search it before the
	// source's library. nil when the catalog brings none.
	Library fs.FS `yaml:"-"`
	// uses are the `use:` entries awaiting expansion; used are the ids they
	// brought in, which the same rule loaded earlier supersedes.
	uses []useEntry
	used map[string]bool
}

// Selected reports whether the rule came into the catalog through `use:`
// rather than an inline definition.
func (c *Catalog) Selected(id string) bool { return c.used[id] }

// UsesLibrary reports whether the catalog selected anything with `use:`.
func (c *Catalog) UsesLibrary() bool { return len(c.used) > 0 }

const presetPrefix = "fleetlint:"

// isPinnedRef reports whether ref names an https or git source.
func isPinnedRef(ref string) bool {
	return strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, gitPrefix) || strings.HasPrefix(ref, ociPrefix)
}

// Override changes a rule that came from an included catalog: its severity,
// parameters and accepted producers, and the policy lower layers are held
// to. It can tighten policy but not loosen what an earlier catalog locked.
type Override struct {
	Enabled  *bool  `yaml:"enabled,omitempty"`
	Reason   string `yaml:"reason,omitempty"`
	Severity string `yaml:"severity,omitempty"`
	// Raise lifts the severity by that many steps (info, warning, error),
	// capped at error; for pattern overrides that tighten whatever they match.
	Raise  int              `yaml:"raise,omitempty"`
	Params map[string]any   `yaml:"params,omitempty"`
	Accept []map[string]any `yaml:"accept,omitempty"`
	// When and Tiers replace the rule's applicability; "true" for When
	// makes a conditional rule unconditional. Neither may change a locked rule.
	When  *string `yaml:"when,omitempty"`
	Tiers []int   `yaml:"tiers,omitempty"`
	// Locked, MinSeverity and Exceptions have the meaning they have on a rule.
	Locked      *bool  `yaml:"locked,omitempty"`
	MinSeverity string `yaml:"min_severity,omitempty"`
	Exceptions  *bool  `yaml:"exceptions,omitempty"`
}

// Metadata identifies a catalog.
type Metadata struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Description string `yaml:"description,omitempty"`
	// Includes names catalogs this one builds on; they are loaded first.
	Includes []string `yaml:"includes,omitempty"`
	// Engine is the engine level the catalog was written for; 0 means it
	// does not say. See EngineLevel.
	Engine int `yaml:"engine,omitempty"`
	// Layer is who owns the catalog: org or team. Presets are the preset
	// layer, a catalog that says nothing is the repository's.
	Layer string `yaml:"layer,omitempty"`
	// Catalog is the version of the fleetlint catalog this one builds on:
	// the presets it includes and the library it selects from come from
	// it. Required for a remote catalog that does either; one version holds
	// for a whole run.
	Catalog *Pin `yaml:"catalog,omitempty"`
}

// Pin names a version of the fleetlint catalog: a tag or a full commit SHA,
// and the repository when it is not the project's.
type Pin struct {
	Version string `yaml:"version"`
	Repo    string `yaml:"repo,omitempty"`
}

// Same reports whether two pins name the same version; a nil pin is the
// built-in catalog.
func (p *Pin) Same(o *Pin) bool {
	if p == nil || o == nil {
		return p == nil && o == nil
	}
	return p.Version == o.Version && repoOrDefault(p.Repo) == repoOrDefault(o.Repo)
}

func repoOrDefault(repo string) string {
	if repo == "" {
		return DefaultRepo
	}
	return repo
}

// String names the pin in messages.
func (p *Pin) String() string {
	if p == nil {
		return "the built-in catalog"
	}
	if p.Repo == "" || p.Repo == DefaultRepo {
		return "catalog " + p.Version
	}
	return "catalog " + p.Version + " from " + p.Repo
}

// ErrDigestRequired is returned for a remote reference without a digest.
var ErrDigestRequired = errors.New("remote catalogs must be pinned with #sha256-<digest>")

// Presets returns the names of the built-in presets.
func Presets() []string { return Builtin().PresetNames() }

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
	// FetchGitTree reads a directory (its files and its templates/ tree) at
	// a tag or commit, so a git catalog can ship templates; nil means the
	// catalog file alone is read through FetchGit.
	FetchGitTree func(repoURL, rev, dir string) (map[string][]byte, error)
	// FetchCatalog provides a whole catalog repository at a tag or commit as
	// a directory, with the commit it resolved to; nil disables pinning.
	FetchCatalog func(repoURL, rev string) (dir, commit string, err error)
	// FetchOCI reads a catalog artifact from a registry; nil disables oci catalogs.
	FetchOCI func(OCIRef) (*OCIArtifact, error)
	// TrustedRoot provides sigstore trust material for signature checks;
	// nil disables them, which makes signed sources unusable.
	TrustedRoot TrustedRootFunc
	// Signers are the identities the sources file trusts for oci catalogs;
	// config sets them after reading the sources file.
	Signers []Signer
	// Source is the catalog `fleetlint:<preset>` resolves in; nil is the
	// built-in one.
	Source *Source
}

// DefaultRepo is where a pinned catalog version comes from unless the
// repository names another.
const DefaultRepo = "https://github.com/fleetlint/catalog.git"

// Pin switches the loader to another version of the catalog and returns it.
func (l *Loader) Pin(repoURL, version string) (*Source, error) {
	if repoURL == "" {
		repoURL = DefaultRepo
	}
	if l.FetchCatalog == nil {
		return nil, fmt.Errorf("catalog %s: pinned catalogs are disabled in this mode", version)
	}
	dir, commit, err := l.FetchCatalog(repoURL, version)
	if err != nil {
		return nil, fmt.Errorf("catalog %s from %s: %w", version, repoURL, err)
	}
	name := version
	if !commitRe.MatchString(version) && len(commit) >= 12 {
		name += " (" + commit[:12] + ")"
	}
	root := os.DirFS(dir)
	l.Source = &Source{Presets: root, Templates: root, Rules: root, Version: name, Pinned: true}
	if len(l.Source.PresetNames()) == 0 {
		return nil, fmt.Errorf("catalog %s from %s: no presets/ directory", version, repoURL)
	}
	return l.Source, nil
}

func (l Loader) source() *Source {
	if l.Source != nil {
		return l.Source
	}
	return Builtin()
}

// RemoteLoader is the Loader for commands that may use the network.
func RemoteLoader(ctx context.Context) Loader {
	return Loader{Fetch: HTTPFetch(ctx), FetchGit: GitFetch(ctx), FetchGitTree: GitTreeFetch(ctx), FetchCatalog: PinFetch(ctx, ""), FetchOCI: OCIFetch(ctx), TrustedRoot: PublicTrustedRoot(ctx)}
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
		dropIncluded(c, out)
		out = append(out, c)
	}
	return out, nil
}

// dropIncluded removes the rules a catalog selected with `use:` that one of
// its includes already defines: the same library rule, from the catalog
// that introduced it. An inline redefinition is kept and judged as such.
func dropIncluded(c *Catalog, included []*Catalog) {
	if len(c.used) == 0 {
		return
	}
	have := map[string]bool{}
	for _, inc := range included {
		for _, r := range inc.Rules {
			have[r.ID] = true
		}
	}
	kept := c.Rules[:0]
	for _, r := range c.Rules {
		if c.used[r.ID] && have[r.ID] {
			continue
		}
		kept = append(kept, r)
	}
	c.Rules = kept
}

func (l Loader) loadOne(ref string) ([]*Catalog, error) {
	switch {
	case strings.HasPrefix(ref, presetPrefix):
		c, err := l.loadPreset(strings.TrimPrefix(ref, presetPrefix))
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

func (l Loader) loadPreset(name string) (*Catalog, error) {
	src := l.source()
	b, err := fs.ReadFile(src.Presets, "presets/"+name+".yaml")
	if err != nil {
		return nil, fmt.Errorf("unknown preset %q (catalog %s has: %s)", name, src.Version, strings.Join(src.PresetNames(), ", "))
	}
	c, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("preset %s: %w", name, err)
	}
	if err := l.expand(c); err != nil {
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
		if err := l.expand(c); err != nil {
			return nil, fmt.Errorf("catalog %s: %w", f, err)
		}
		c.Ref, c.Trusted = ref, true
		out = append(out, c)
	}
	return out, nil
}

// loadPinned loads an https or git catalog.
func (l Loader) loadPinned(ref string) ([]*Catalog, error) {
	if strings.HasPrefix(ref, ociPrefix) {
		return l.loadOCI(ref)
	}
	var (
		b                  []byte
		templates, library fs.FS
		err                error
	)
	if strings.HasPrefix(ref, gitPrefix) && l.FetchGitTree != nil {
		b, templates, library, err = l.readGitTree(ref)
	} else {
		b, err = l.readPinned(ref)
	}
	if err != nil {
		return nil, err
	}
	c, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("catalog %s: %w", ref, err)
	}
	c.Library = library
	if err := l.expand(c); err != nil {
		return nil, fmt.Errorf("catalog %s: %w", ref, err)
	}
	c.Ref, c.Templates = ref, templates
	return []*Catalog{c}, nil
}

// readGitTree reads a git catalog with the templates/ directory next to it.
// Templates are taken only from a commit-pinned reference: the digest of a
// tag reference covers the catalog file, not the files around it.
func (l Loader) readGitTree(ref string) ([]byte, fs.FS, fs.FS, error) {
	g, err := parseGitRef(ref)
	if err != nil {
		return nil, nil, nil, err
	}
	dir, file := path.Split(g.Path)
	files, err := l.FetchGitTree(g.URL, g.Rev, strings.TrimSuffix(dir, "/"))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("fetch %s: %w", ref, err)
	}
	b, ok := files[file]
	if !ok {
		return nil, nil, nil, fmt.Errorf("fetch %s: %s has no file %s", ref, g.URL, g.Path)
	}
	if g.Digest != "" {
		if err := VerifyDigest(b, g.Digest); err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %w", ref, err)
		}
	}
	if !g.isCommit() {
		return b, nil, nil, nil
	}
	templates, library := shippedFiles(files)
	return b, templates, library, nil
}

// shippedFiles sorts a catalog's companion files into its templates/ and
// rules/ trees; either is nil when empty.
func shippedFiles(files map[string][]byte) (templates, library fs.FS) {
	t, r := memFS{}, memFS{}
	for name, body := range files {
		switch {
		case strings.HasPrefix(name, templatesDir):
			t[name] = body
		case strings.HasPrefix(name, "rules/"):
			r[name] = body
		}
	}
	if len(t) > 0 {
		templates = t
	}
	if len(r) > 0 {
		library = r
	}
	return templates, library
}

// readPinned returns the body of an https or git reference after checking
// it against its pin.
func (l Loader) readPinned(ref string) ([]byte, error) {
	if strings.HasPrefix(ref, gitPrefix) {
		return l.readGit(ref)
	}
	if strings.HasPrefix(ref, ociPrefix) {
		return nil, fmt.Errorf("%s: a sources file cannot come from a registry; use https or git", ref)
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
	if err := probeEngine(b); err != nil {
		return nil, err
	}
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
	switch c.Metadata.Layer {
	case "", LayerOrg, LayerTeam:
	default:
		return nil, fmt.Errorf("metadata.layer must be org or team (got %q); presets and the repository are implied", c.Metadata.Layer)
	}
	if c.Metadata.Catalog != nil && c.Metadata.Catalog.Version == "" {
		return nil, errors.New("metadata.catalog.version is required: a tag or a full commit SHA")
	}
	if err := checkEngine(&c); err != nil {
		return nil, err
	}
	c.Digest = Digest(b)
	if err := requireSeverity(b); err != nil {
		return nil, err
	}
	if err := validatePolicy(c.Rules); err != nil {
		return nil, err
	}
	if err := validateOverrides(&c); err != nil {
		return nil, err
	}
	splitUses(&c)
	if err := validateRules(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// validateRules checks each inline rule and marks it with its catalog.
func validateRules(c *Catalog) error {
	seen := map[string]bool{}
	for i := range c.Rules {
		r := &c.Rules[i]
		r.Source = c.Metadata.Name + "@" + c.Metadata.Version
		if err := validateRule(r); err != nil {
			return err
		}
		if seen[r.ID] {
			return fmt.Errorf("rule %s: duplicate id", r.ID)
		}
		seen[r.ID] = true
	}
	return nil
}

// Local builds the catalog a repository's own configuration amounts to:
// its inline rules, selections, overrides and exceptions, at the repository
// layer and trusted. raw is the configuration file, for the checks that
// read the document itself.
func Local(name string, raw []byte, rules []model.Rule, overrides map[string]Override, exceptions []model.Exception) (*Catalog, error) {
	c := &Catalog{
		APIVersion: APIVersion, Kind: "Catalog",
		Metadata: Metadata{Name: name, Version: "local"},
		Rules:    rules, Overrides: overrides, Exceptions: exceptions,
		Ref: name, Trusted: true, Layer: LayerRepo,
	}
	if err := requireSeverity(raw); err != nil {
		return nil, err
	}
	if err := validatePolicy(c.Rules); err != nil {
		return nil, err
	}
	if err := validateOverrides(c); err != nil {
		return nil, err
	}
	splitUses(c)
	// A repository's own rule may stay terse: the id serves as title, the
	// message as requirement.
	for i := range c.Rules {
		r := &c.Rules[i]
		r.Title = firstNonEmpty(r.Title, r.ID)
		r.Requirement = firstNonEmpty(r.Requirement, r.Message)
		r.Fix.Human = firstNonEmpty(r.Fix.Human, "See the rule's message.")
	}
	if err := validateRules(c); err != nil {
		return nil, err
	}
	return c, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// Expand resolves a catalog's `use:` entries against the loader's library;
// Local catalogs call it since they do not pass through Load.
func (l Loader) Expand(c *Catalog) error { return l.expand(c) }

// splitUses takes the `use:` entries out of the rules list, remembering
// where they stood so expansion keeps the order; requireSeverity already
// refused entries with other keys.
func splitUses(c *Catalog) {
	c.used = map[string]bool{}
	var rules []model.Rule
	for _, r := range c.Rules {
		if r.Use == "" {
			rules = append(rules, r)
			continue
		}
		c.uses = append(c.uses, useEntry{Pattern: r.Use, Index: len(rules)})
	}
	c.Rules = rules
}

// probeEngine reads the engine level before the strict decode, so a catalog
// written for a newer fleetlint says so instead of failing on a key this
// one lacks. A document that does not even decode loosely is left to Parse.
func probeEngine(b []byte) error {
	var probe struct {
		Metadata Metadata `yaml:"metadata"`
	}
	if err := yaml.Unmarshal(b, &probe); err != nil {
		return nil //nolint:nilerr // the strict decode reports the shape error with its position
	}
	return checkEngine(&Catalog{Metadata: probe.Metadata})
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
// validateOverrides checks what can be checked without the included rules.
func validateOverrides(c *Catalog) error {
	own := map[string]bool{}
	for _, r := range c.Rules {
		own[r.ID] = true
	}
	for id, ov := range c.Overrides {
		if own[id] {
			return fmt.Errorf("overrides.%s: the rule is defined in this catalog; change the rule itself", id)
		}
		for name, sev := range map[string]string{"severity": ov.Severity, "min_severity": ov.MinSeverity} {
			if sev == "" {
				continue
			}
			if _, err := model.ParseSeverity(sev); err != nil {
				return fmt.Errorf("overrides.%s: %s: %w", id, name, err)
			}
		}
	}
	return nil
}

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
		if use, isUse := r["use"]; isUse {
			if len(r) != 1 {
				return fmt.Errorf("rules: a `use: %v` entry selects a library rule and takes no other keys (adjust it under overrides:)", use)
			}
			continue
		}
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
