// Package config reads .fleetlint.yaml, resolves the catalogs it extends and
// produces the effective rule set for a repository. Validation is strict on
// purpose: an unknown key, an unknown rule id or a disable without a reason
// is an error, because a typo must never silently weaken a check.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/repo"
)

// FileName is the config file fleetlint looks for at the repository root.
const FileName = ".fleetlint.yaml"

// LegacyFileName is also accepted when FileName is absent.
const LegacyFileName = "fleetlint.yaml"

// DefaultExtends is used when no config file exists.
var DefaultExtends = []string{"fleetlint:recommended"}

// File is the on-disk shape of .fleetlint.yaml. Its rules, overrides and
// exceptions have the same shape as a catalog's: the repository is the last
// catalog of the run, at the repository layer.
type File struct {
	Version int `yaml:"version"`
	// Catalog pins the version of the fleetlint catalog that `fleetlint:`
	// presets, the library and fix templates come from, instead of the
	// built-in one. One version holds for a whole run.
	Catalog *catalog.Pin `yaml:"catalog,omitempty"`
	// Sources names a sources file; its catalog names become usable in Extends
	// and its own extends come first.
	Sources string        `yaml:"sources,omitempty"`
	Extends []string      `yaml:"extends,omitempty"`
	Facts   FactOverrides `yaml:"facts,omitempty"`
	// Scopes nil = discover workspace members; empty list = no scopes.
	Scopes *[]Scope `yaml:"scopes,omitempty"`
	// Rules defines the repository's own rules (full definitions) or selects
	// library rules with `use:`; Overrides adjusts rules the catalogs brought
	// in. Policy keys (locked, min_severity, exceptions) are not the
	// repository's to set.
	Overrides  map[string]catalog.Override `yaml:"overrides,omitempty"`
	Rules      []model.Rule                `yaml:"rules,omitempty"`
	Exceptions []model.Exception           `yaml:"exceptions,omitempty"`
	Baseline   string                      `yaml:"baseline,omitempty"`
}

// FactOverrides pins discovered facts.
type FactOverrides struct {
	Tier       int      `yaml:"tier,omitempty"`
	Visibility string   `yaml:"visibility,omitempty"`
	Stacks     []string `yaml:"stacks,omitempty"`
	Forge      string   `yaml:"forge,omitempty"`
	// Container says where the task runner's targets run: none, devcontainer,
	// docker or podman; unset follows detection.
	Container string `yaml:"container,omitempty"`
	// TaskRunner names the runner to use: make, just, task, npm-scripts or
	// gradle; unset takes the first one found.
	TaskRunner string `yaml:"taskrunner,omitempty"`
}

// Scope is a sub-directory evaluated as its own repository.
type Scope struct {
	Path  string        `yaml:"path"`
	Facts FactOverrides `yaml:"facts,omitempty"`
	// Overrides adjust the effective rules for this scope: severity, params,
	// accept, enabled; never policy or applicability.
	Overrides map[string]catalog.Override `yaml:"overrides,omitempty"`
	// Exceptions scoped to this path; filled from a nested .fleetlint.yaml.
	Exceptions []model.Exception `yaml:"exceptions,omitempty"`
}

// RuleConf is the adjustment part of an override, as applyOverride hands it on.
type RuleConf struct {
	Enabled  *bool
	Reason   string
	Severity string
	Params   map[string]any
	Accept   []map[string]any
}

// Disabled records a rule switched off (or lowered) and why, for the report.
// Layer is who did it: repo for configuration files, org or team for a
// catalog that lowered a rule of an earlier layer.
type Disabled struct {
	ID, Reason, Where, Layer string
}

// Effective is the resolved configuration for one repository.
type Effective struct {
	Path   string
	Exists bool
	File   File
	// Source is the catalog version presets and templates come from: the
	// built-in one unless the file pins another.
	Source     *catalog.Source
	Catalogs   []*catalog.Catalog
	Rules      []model.Rule
	Disabled   []Disabled
	Exceptions []model.Exception
	// Weakened lists severity reductions with their reasons.
	Weakened []Disabled
	// Pin is the catalog version this run uses; nil is the built-in one.
	Pin *catalog.Pin
	raw []byte
}

// FixTemplates returns the templates for `fix`: files shipped with the
// catalogs the repository extends, later layers first, over the preset
// source's templates.
func (e *Effective) FixTemplates() catalog.Templates {
	layers := make([]fs.FS, 0, len(e.Catalogs)+1)
	for i := len(e.Catalogs) - 1; i >= 0; i-- {
		if e.Catalogs[i].Templates != nil {
			layers = append(layers, e.Catalogs[i].Templates)
		}
	}
	src := e.Source
	if src == nil {
		src = catalog.Builtin()
	}
	return src.FixTemplatesOver(layers...)
}

// Options controls loading.
type Options struct {
	// Now is injected for deterministic exception expiry in tests.
	Now time.Time
	// Loader resolves catalog references.
	Loader catalog.Loader
}

// Load reads <root>/.fleetlint.yaml (or defaults) and resolves it.
func Load(root string, opts Options) (*Effective, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	eff := &Effective{Path: filepath.Join(root, FileName)}
	b, err := os.ReadFile(eff.Path)
	if errors.Is(err, os.ErrNotExist) {
		// Accept the undotted name too, so a repo that chose it keeps working.
		if lb, lerr := os.ReadFile(filepath.Join(root, LegacyFileName)); lerr == nil { //nolint:gosec // fixed name under the repository root
			b, err, eff.Path = lb, nil, filepath.Join(root, LegacyFileName)
		}
	}
	switch {
	case errors.Is(err, os.ErrNotExist):
		eff.File = File{Version: 1, Extends: DefaultExtends}
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", FileName, err)
	default:
		eff.Exists = true
		if err := yaml.UnmarshalWithOptions(b, &eff.File, yaml.Strict()); err != nil {
			return nil, fmt.Errorf("%s: %w", FileName, err)
		}
	}
	if err := validateFile(&eff.File); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	eff.raw = b
	opts.Loader.BaseDir = root
	if err := eff.resolve(opts); err != nil {
		return nil, err
	}
	if err := eff.validateScopeRules(); err != nil {
		return nil, err
	}
	return eff, nil
}

// validateScopeRules surfaces scope rule mistakes at load time rather than
// in the middle of a run. It leaves the recorded Disabled/Weakened lists as
// they were: the engine records them when it applies the scope for real.
func (e *Effective) validateScopeRules() error {
	if e.File.Scopes == nil {
		return nil
	}
	disabled, weakened := e.Disabled, e.Weakened
	defer func() { e.Disabled, e.Weakened = disabled, weakened }()
	for _, s := range *e.File.Scopes {
		if len(s.Overrides) == 0 {
			continue
		}
		if _, err := e.ScopeRules(s.Path, s.Overrides); err != nil {
			return err
		}
	}
	return nil
}

func validateFile(f *File) error {
	if f.Version != 1 {
		return fmt.Errorf("version must be 1 (got %d)", f.Version)
	}
	if f.Facts.Tier < 0 || f.Facts.Tier > 3 {
		return fmt.Errorf("facts.tier must be 1, 2 or 3")
	}
	if err := validateChoices(f); err != nil {
		return err
	}
	switch f.Facts.Visibility {
	case "", "public", "private":
	default:
		return fmt.Errorf("facts.visibility must be public or private (got %q)", f.Facts.Visibility)
	}
	if err := validateExceptions(f.Exceptions); err != nil {
		return err
	}
	if f.Scopes == nil {
		return nil
	}
	for _, s := range *f.Scopes {
		if s.Path == "" || strings.HasPrefix(s.Path, "..") || filepath.IsAbs(s.Path) {
			return fmt.Errorf("scopes: path %q must be a relative path inside the repository", s.Path)
		}
	}
	return nil
}

// validateChoices checks the settings that name a tool or a version.
func validateChoices(f *File) error {
	if pin := f.Catalog; pin != nil {
		if pin.Version == "" {
			return errors.New("catalog.version is required: a tag or a full commit SHA")
		}
		if pin.Repo != "" && !strings.HasPrefix(pin.Repo, "https://") && !strings.HasPrefix(pin.Repo, "ssh://") && !strings.HasPrefix(pin.Repo, "git@") {
			return fmt.Errorf("catalog.repo %q must be an https or ssh git URL", pin.Repo)
		}
	}
	switch f.Facts.Container {
	case "", facts.ContainerNone, facts.ContainerDevcontainer, facts.ContainerDocker, facts.ContainerPodman:
	default:
		return fmt.Errorf("facts.container must be none, devcontainer, docker or podman (got %q)", f.Facts.Container)
	}
	if f.Facts.TaskRunner != "" && !slices.Contains(repo.RunnerKinds(), f.Facts.TaskRunner) {
		return fmt.Errorf("facts.taskrunner must be one of %s (got %q)", strings.Join(repo.RunnerKinds(), ", "), f.Facts.TaskRunner)
	}
	return nil
}

func validateExceptions(exs []model.Exception) error {
	for i, ex := range exs {
		if ex.Rule == "" || ex.Reason == "" {
			return fmt.Errorf("exceptions[%d]: rule and reason are required", i)
		}
		if ex.Match != "" && !strings.HasPrefix(ex.Match, "re:") && !repo.ValidGlob(ex.Match) {
			return fmt.Errorf("exceptions[%d]: match %q is not a valid glob", i, ex.Match)
		}
		if ex.Until == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", ex.Until); err != nil {
			return fmt.Errorf("exceptions[%d]: until must be YYYY-MM-DD", i)
		}
	}
	return nil
}

// ScopeRules applies a scope's (or nested file's) overrides to copies of the
// effective rules. Ids must exist, disabling and lowering need a reason, and
// every weakening is recorded with the scope path so reports show it. A
// scope adjusts: no policy, no applicability. It never mutates the root rules.
func (e *Effective) ScopeRules(scopePath string, overrides map[string]catalog.Override) ([]model.Rule, error) {
	where := scopePath + "/" + FileName
	byID := make(map[string]*model.Rule, len(e.Rules))
	order := make([]string, 0, len(e.Rules))
	for i := range e.Rules {
		r := e.Rules[i]
		r.Params = copyParams(r.Params)
		byID[r.ID] = &r
		order = append(order, r.ID)
	}
	for _, key := range overrideKeys(overrides) {
		ov := overrides[key]
		if err := adjustmentOnly(ov, where, key); err != nil {
			return nil, err
		}
		ids, err := matchRuleIDs(key, byID, where)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if err := e.applyOverride(byID[id], ov, where, catalog.LayerRepo, where, byID); err != nil {
				return nil, err
			}
		}
	}
	out := make([]model.Rule, 0, len(order))
	for _, id := range order {
		if r, ok := byID[id]; ok {
			out = append(out, *r)
		}
	}
	return out, nil
}

// adjustmentOnly refuses the override keys the repository and its scopes
// may not use: policy binds lower layers, and the repository is the lowest;
// applicability changes would switch a rule off without a reason.
func adjustmentOnly(ov catalog.Override, where, key string) error {
	switch {
	case ov.Locked != nil || ov.MinSeverity != "" || ov.Exceptions != nil:
		return fmt.Errorf("%s: overrides.%s: locked, min_severity and exceptions are policy a catalog sets; the repository adjusts", where, key)
	case ov.When != nil || len(ov.Tiers) > 0:
		return fmt.Errorf("%s: overrides.%s: when and tiers are set by catalogs; to switch a rule off here use enabled: false with a reason", where, key)
	}
	return nil
}

func copyParams(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// Nested is a scope-local .fleetlint.yaml. It may override facts, adjust
// rules and add exceptions for its scope; it cannot extend catalogs or define
// new rules, which keeps policy resolution in one place.
type Nested struct {
	Version    int                         `yaml:"version"`
	Facts      FactOverrides               `yaml:"facts,omitempty"`
	Overrides  map[string]catalog.Override `yaml:"overrides,omitempty"`
	Exceptions []model.Exception           `yaml:"exceptions,omitempty"`
}

// LoadNested reads <dir>/.fleetlint.yaml for a scope. A missing file yields nil.
func LoadNested(dir string, now time.Time) (*Nested, error) {
	b, err := os.ReadFile(filepath.Join(dir, FileName)) //nolint:gosec // fixed name under a scope directory
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil //nolint:nilnil // absence is the normal case, not an error
	}
	if err != nil {
		return nil, err
	}
	var n Nested
	if err := yaml.UnmarshalWithOptions(b, &n, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("%s/%s: %w", dir, FileName, err)
	}
	if err := validateNested(dir, &n); err != nil {
		return nil, err
	}
	for i := range n.Exceptions {
		n.Exceptions[i].Expired = expired(n.Exceptions[i].Until, now)
	}
	return &n, nil
}

func validateNested(dir string, n *Nested) error {
	if n.Version != 1 {
		return fmt.Errorf("%s/%s: version must be 1", dir, FileName)
	}
	if err := validateExceptions(n.Exceptions); err != nil {
		return fmt.Errorf("%s/%s: %w", dir, FileName, err)
	}
	return nil
}

// ToFacts converts the config section into the facts package type.
func (f FactOverrides) ToFacts() facts.Overrides {
	return facts.Overrides{
		Stacks:     f.Stacks,
		Visibility: facts.Visibility(f.Visibility),
		Tier:       f.Tier,
		Forge:      f.Forge,

		Container:  f.Container,
		TaskRunner: f.TaskRunner,
	}
}

func (e *Effective) resolve(opts Options) error {
	entries, src, err := e.extendsEntries(&opts.Loader)
	if err != nil {
		return err
	}
	if err := e.choosePin(src, &opts.Loader); err != nil {
		return err
	}
	loaded, err := e.loadEntries(opts.Loader, entries)
	if err != nil {
		return err
	}
	// Layer order is fixed whatever extends says; within a layer, written order.
	sort.SliceStable(loaded, func(i, j int) bool {
		return catalog.LayerRank(loaded[i].Layer) < catalog.LayerRank(loaded[j].Layer)
	})
	own, err := catalog.Local(FileName, e.raw, e.File.Rules, e.File.Overrides, e.File.Exceptions)
	if err != nil {
		return fmt.Errorf("%s: %w", FileName, err)
	}
	if err := opts.Loader.Expand(own); err != nil {
		return fmt.Errorf("%s: %w", FileName, err)
	}
	loaded = append(loaded, own)
	byID := map[string]*model.Rule{}
	var order []string
	for _, c := range loaded {
		if err := e.addCatalog(c, byID, &order, opts.Now); err != nil {
			return err
		}
	}
	for _, id := range order {
		if r, ok := byID[id]; ok {
			e.Rules = append(e.Rules, *r)
		}
	}
	return nil
}

// choosePin settles the one catalog version of the run: the sources file's
// and the repository's must agree when both speak, and the loader's source
// follows it.
func (e *Effective) choosePin(src *catalog.Sources, l *catalog.Loader) error {
	e.Pin = e.File.Catalog
	if src != nil && src.Catalog != nil {
		if e.Pin != nil && !e.Pin.Same(src.Catalog) {
			return fmt.Errorf("%s pins %s, the sources file %s: one catalog version per run; drop the repository's pin or make them agree", FileName, e.Pin, src.Catalog)
		}
		e.Pin = src.Catalog
	}
	e.Source = catalog.Builtin()
	if e.Pin == nil {
		return nil
	}
	var err error
	if e.Source, err = l.Pin(e.Pin.Repo, e.Pin.Version); err != nil {
		return fmt.Errorf("%s: %w", FileName, err)
	}
	return nil
}

// loadEntries loads every extends entry with its includes, settles each
// catalog's layer and checks that it builds on the run's catalog version.
func (e *Effective) loadEntries(l catalog.Loader, entries []extendsEntry) ([]*catalog.Catalog, error) {
	var out []*catalog.Catalog
	for _, en := range entries {
		cats, err := l.Load(en.ref)
		if err != nil {
			return nil, err
		}
		for _, c := range cats {
			if err := settleLayer(c, en); err != nil {
				return nil, err
			}
			if err := e.checkBuildsOn(c); err != nil {
				return nil, err
			}
			out = append(out, c)
		}
	}
	return out, nil
}

// settleLayer gives a catalog its layer: presets are the preset layer; a
// catalog says org or team in its metadata, and a sources-file alias it is
// loaded under must agree; anything else is the repository's.
func settleLayer(c *catalog.Catalog, en extendsEntry) error {
	declared := c.Metadata.Layer
	switch {
	case catalog.RefLayer(c.Ref) == catalog.LayerPreset:
		c.Layer = catalog.LayerPreset
	case c.Ref != en.ref:
		// An include of the entry: it takes the entry's layer unless it says otherwise.
		c.Layer = firstNonEmpty(declared, en.layer)
	case en.alias != "" && declared != "" && declared != en.layer:
		return fmt.Errorf("sources file names %s as %q but its metadata.layer is %q", c.Ref, en.alias, declared)
	case en.alias != "":
		c.Layer, c.Alias = en.layer, en.alias
	default:
		c.Layer = firstNonEmpty(declared, catalog.LayerRepo)
	}
	return nil
}

// checkBuildsOn holds a catalog to the run's catalog version: what it
// declares must be the version in use, and a remote catalog that includes
// presets or selects library rules must declare one, or its content would
// depend on whichever fleetlint reads it.
func (e *Effective) checkBuildsOn(c *catalog.Catalog) error {
	if catalog.RefLayer(c.Ref) == catalog.LayerPreset {
		return nil
	}
	declared := c.Metadata.Catalog
	if declared == nil {
		if !c.Trusted && (c.UsesLibrary() || includesPreset(c)) {
			return fmt.Errorf("catalog %s builds on presets or the library but states no metadata.catalog version; without it the rules it resolves to depend on the reader's fleetlint", c.Ref)
		}
		return nil
	}
	if declared.Same(e.Pin) || (e.Pin == nil && declared.Same(&catalog.Pin{Version: catalog.ModuleVersion()})) {
		return nil
	}
	return fmt.Errorf("catalog %s builds on %s; this run uses %s (%s). Pin that version in %s (catalog:) or in the sources file so every repository runs the same rules", c.Ref, declared, e.Pin, e.Source.Version, FileName)
}

func includesPreset(c *catalog.Catalog) bool {
	for _, inc := range c.Metadata.Includes {
		if catalog.RefLayer(inc) == catalog.LayerPreset {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// extendsEntry is one `extends` item with its alias resolved.
type extendsEntry struct {
	ref, alias, layer string
}

// extendsEntries lists what the run extends: the sources file's extends
// first, then the repository's, each alias resolved; a repository without
// extends gets the default. The sources file's signers become the loader's.
func (e *Effective) extendsEntries(l *catalog.Loader) ([]extendsEntry, *catalog.Sources, error) {
	var src *catalog.Sources
	refs := e.File.Extends
	if e.File.Sources != "" {
		var err error
		if src, err = l.LoadSources(e.File.Sources); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", FileName, err)
		}
		l.Signers = src.Signers
		refs = append(append([]string{}, src.Extends...), refs...)
	}
	if len(refs) == 0 {
		refs = DefaultExtends
	}
	entries := make([]extendsEntry, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		if seen[ref] {
			continue
		}
		seen[ref] = true
		layer, isAlias := catalog.AliasLayer(ref)
		target, known := "", false
		if src != nil {
			target, known = src.Catalogs[ref]
		}
		switch {
		case known:
			entries = append(entries, extendsEntry{ref: target, alias: ref, layer: layer})
		case isAlias && src == nil:
			return nil, nil, fmt.Errorf("%s: extends: %q is a sources-file name but no sources file is set", FileName, ref)
		case isAlias:
			return nil, nil, fmt.Errorf("%s: extends: %q is not in the sources file (it has: %s)", FileName, ref, strings.Join(src.Aliases(), ", "))
		default:
			entries = append(entries, extendsEntry{ref: ref, layer: catalog.RefLayer(ref)})
		}
	}
	return entries, src, nil
}

func (e *Effective) addCatalog(c *catalog.Catalog, byID map[string]*model.Rule, order *[]string, now time.Time) error {
	if e.hasCatalog(c) {
		return nil
	}
	e.Catalogs = append(e.Catalogs, c)
	for i := range c.Rules {
		r := c.Rules[i]
		if !c.Trusted && r.Kind == model.KindCommand {
			return fmt.Errorf("catalog %s: rule %s: command rules are only allowed in the repository's own config", c.Ref, r.ID)
		}
		r.Layer = c.Layer
		prev, seen := byID[r.ID]
		switch {
		case !seen:
			*order = append(*order, r.ID)
		case c.Selected(r.ID):
			continue // the same library rule, already loaded: the first catalog keeps it
		case c.Ref == FileName:
			return fmt.Errorf("%s: rules: %s is a catalog rule; the repository adjusts it under overrides, it does not redefine it", FileName, r.ID)
		default:
			if err := e.redefine(c, prev, &r); err != nil {
				return err
			}
		}
		byID[r.ID] = &r
	}
	if err := e.applyCatalogOverrides(c, byID); err != nil {
		return err
	}
	return e.addExceptions(c, byID, now)
}

// addExceptions takes a catalog's exceptions into the run, attributed to the
// catalog and bound by the policy set before it.
func (e *Effective) addExceptions(c *catalog.Catalog, byID map[string]*model.Rule, now time.Time) error {
	if err := validateExceptions(c.Exceptions); err != nil {
		return fmt.Errorf("%s: %w", c.Ref, err)
	}
	for _, ex := range c.Exceptions {
		r, known := byID[ex.Rule]
		if !known && !e.isDisabled(ex.Rule) {
			return fmt.Errorf("%s: exceptions: rule %q is not loaded", c.Ref, ex.Rule)
		}
		if known && !r.ExceptionsAllowed() {
			return fmt.Errorf("%s: exceptions: rule %s forbids exceptions (set by %s)", c.Ref, r.ID, r.PolicySource())
		}
		ex.Layer, ex.Where, ex.Expired = c.Layer, c.Ref, expired(ex.Until, now)
		e.Exceptions = append(e.Exceptions, ex)
	}
	return nil
}

// applyCatalogOverrides applies a catalog's `overrides:` to the rules its
// includes brought in. A catalog is held to the policy of the catalogs
// before it, like a repository is, and may tighten that policy further.
func (e *Effective) applyCatalogOverrides(c *catalog.Catalog, byID map[string]*model.Rule) error {
	where := "catalog " + c.Ref + " (overrides)"
	if c.Ref == FileName {
		where = FileName
	}
	by := c.Metadata.Name + "@" + c.Metadata.Version
	inline := inlineIDs(c)
	for _, key := range overrideKeys(c.Overrides) {
		ov := c.Overrides[key]
		if c.Ref == FileName {
			if err := adjustmentOnly(ov, FileName, key); err != nil {
				return err
			}
		}
		ids, err := matchRuleIDs(key, byID, where)
		if err != nil {
			return err
		}
		if err := e.applyToAll(ids, inline, ov, where, c.Layer, by, byID); err != nil {
			return err
		}
	}
	return nil
}

// applyToAll applies one override to the matched rules, skipping the
// catalog's own definitions: those are written as intended, overrides adjust
// what the catalog took in.
func (e *Effective) applyToAll(ids []string, inline map[string]bool, ov catalog.Override, where, layer, by string, byID map[string]*model.Rule) error {
	for _, id := range ids {
		if inline[id] {
			continue
		}
		if err := e.applyOverride(byID[id], ov, where, layer, by, byID); err != nil {
			return err
		}
	}
	return nil
}

// inlineIDs are the rules a catalog defines itself, as opposed to selects.
func inlineIDs(c *catalog.Catalog) map[string]bool {
	out := map[string]bool{}
	for _, r := range c.Rules {
		if !c.Selected(r.ID) {
			out[r.ID] = true
		}
	}
	return out
}

// overrideKeys orders a catalog's override keys: patterns first, then exact
// ids, each group sorted, so an exact key refines what a pattern set.
func overrideKeys(overrides map[string]catalog.Override) []string {
	var globs, exact []string
	for k := range overrides {
		if strings.ContainsAny(k, "*?[") {
			globs = append(globs, k)
		} else {
			exact = append(exact, k)
		}
	}
	sort.Strings(globs)
	sort.Strings(exact)
	return append(globs, exact...)
}

// matchRuleIDs resolves an override key to loaded rule ids. An exact key
// must exist; a pattern may match nothing.
func matchRuleIDs(key string, byID map[string]*model.Rule, where string) ([]string, error) {
	if !strings.ContainsAny(key, "*?[") {
		if _, ok := byID[key]; !ok {
			return nil, fmt.Errorf("%s: rules.%s: no such rule in the catalogs loaded before this one", where, key)
		}
		return []string{key}, nil
	}
	rx, err := regexp.Compile(catalog.GlobRegexp(key))
	if err != nil {
		return nil, fmt.Errorf("%s: rules.%s: bad pattern: %w", where, key, err)
	}
	var ids []string
	for id := range byID {
		if rx.MatchString(id) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func (e *Effective) applyOverride(r *model.Rule, ov catalog.Override, where, layer, by string, byID map[string]*model.Rule) error {
	rc := RuleConf{Enabled: ov.Enabled, Reason: ov.Reason, Severity: ov.Severity, Params: ov.Params, Accept: ov.Accept}
	if ov.Raise != 0 {
		if ov.Raise < 0 || ov.Severity != "" {
			return fmt.Errorf("%s: rules.%s: raise must be positive and not combined with severity", where, r.ID)
		}
		raised := r.Severity + model.Severity(ov.Raise)
		if raised > model.SeverityError {
			raised = model.SeverityError
		}
		rc.Severity = raised.String()
	}
	if err := e.overrideAs(r, rc, where, layer, byID); err != nil {
		return err
	}
	if _, still := byID[r.ID]; !still {
		return nil
	}
	if err := applyScope(r, ov, where); err != nil {
		return err
	}
	return tightenPolicy(r, ov, where, by)
}

// applyScope replaces a rule's `when` and `tiers` from a catalog override.
// A locked rule keeps its applicability: narrowing either would weaken it.
func applyScope(r *model.Rule, ov catalog.Override, where string) error {
	if ov.When == nil && len(ov.Tiers) == 0 {
		return nil
	}
	if r.Locked {
		return fmt.Errorf("%s: rules.%s: rule is locked by %s; when and tiers cannot change", where, r.ID, r.PolicySource())
	}
	if ov.When != nil {
		r.When = *ov.When
	}
	if len(ov.Tiers) > 0 {
		for _, t := range ov.Tiers {
			if t < 1 || t > 3 {
				return fmt.Errorf("%s: rules.%s: tiers must be 1, 2 or 3 (got %d)", where, r.ID, t)
			}
		}
		r.Tiers = ov.Tiers
	}
	return nil
}

// tightenPolicy sets lock, floor and the exceptions switch from a catalog
// override. Each can only become stricter.
func tightenPolicy(r *model.Rule, ov catalog.Override, where, by string) error {
	if err := tightenLock(r, ov, where, by); err != nil {
		return err
	}
	if err := tightenFloor(r, ov, where, by); err != nil {
		return err
	}
	if ov.Exceptions == nil {
		return nil
	}
	if *ov.Exceptions && !r.ExceptionsAllowed() {
		return fmt.Errorf("%s: rules.%s: %s forbids exceptions; they cannot be allowed again", where, r.ID, r.PolicySource())
	}
	if !*ov.Exceptions {
		no := false
		r.AllowExceptions, r.PolicyBy = &no, by
	}
	return nil
}

func tightenLock(r *model.Rule, ov catalog.Override, where, by string) error {
	if ov.Locked == nil {
		return nil
	}
	if !*ov.Locked && r.Locked {
		return fmt.Errorf("%s: rules.%s: rule is locked by %s and cannot be unlocked", where, r.ID, r.PolicySource())
	}
	if *ov.Locked && !r.Locked {
		r.Locked, r.PolicyBy = true, by
	}
	return nil
}

func tightenFloor(r *model.Rule, ov catalog.Override, where, by string) error {
	if ov.MinSeverity == "" {
		return nil
	}
	floor, err := model.ParseSeverity(ov.MinSeverity)
	if err != nil {
		return fmt.Errorf("%s: rules.%s: min_severity: %w", where, r.ID, err)
	}
	if floor < r.Floor() {
		return fmt.Errorf("%s: rules.%s: min_severity %s is below the floor %s set by %s", where, r.ID, floor, r.Floor(), r.PolicySource())
	}
	if r.Severity < floor {
		return fmt.Errorf("%s: rules.%s: severity %s is below the min_severity %s set here", where, r.ID, r.Severity, floor)
	}
	r.MinSeverity, r.PolicyBy = ov.MinSeverity, by
	return nil
}

// redefine checks a catalog rule that replaces one from an earlier catalog:
// locks and severity floors hold across catalogs, a floor is inherited, and
// a later layer lowering an earlier layer's severity is recorded.
func (e *Effective) redefine(c *catalog.Catalog, prev, r *model.Rule) error {
	if prev.Locked {
		return fmt.Errorf("catalog %s: rule %s is locked by %s and cannot be redefined", c.Ref, r.ID, prev.PolicySource())
	}
	if prev.MinSeverity != "" {
		if floor := prev.Floor(); r.Severity < floor || (r.MinSeverity != "" && r.Floor() < floor) {
			return fmt.Errorf("catalog %s: rule %s: severity is below the floor %s set by %s", c.Ref, r.ID, floor, prev.PolicySource())
		}
		if r.MinSeverity == "" {
			r.MinSeverity = prev.MinSeverity
		}
	}
	if r.Severity < prev.Severity {
		e.Weakened = append(e.Weakened, Disabled{
			ID: r.ID, Where: c.Ref, Layer: r.Layer,
			Reason: fmt.Sprintf("%s lowers %s to %s", r.Source, prev.Severity, r.Severity),
		})
	}
	return nil
}

// CheckExceptions validates exceptions declared by a scope or nested file
// against the rules' policy; where names the file for the error.
func (e *Effective) CheckExceptions(where string, exs []model.Exception) error {
	for _, ex := range exs {
		for i := range e.Rules {
			if e.Rules[i].ID == ex.Rule && !e.Rules[i].ExceptionsAllowed() {
				return fmt.Errorf("%s: exceptions: rule %s forbids exceptions (set by %s)", where, ex.Rule, e.Rules[i].PolicySource())
			}
		}
	}
	return nil
}

// Requires checks that every required catalog reference was loaded: a
// `#sha256-<hex>` reference must match a loaded digest, any other reference
// must match a loaded catalog's ref or metadata name. This is how CI and
// fleet runs enforce an organization baseline the repository cannot drop.
func (e *Effective) Requires(refs []string) error {
	for _, ref := range refs {
		if !e.hasRequired(ref) {
			return fmt.Errorf("required catalog %s is not part of the effective configuration (add it to extends)", ref)
		}
	}
	return nil
}

func (e *Effective) hasRequired(ref string) bool {
	if i := strings.Index(ref, "#sha256-"); i >= 0 {
		want := strings.ToLower(ref[i+len("#sha256-"):])
		for _, c := range e.Catalogs {
			if c.Digest == want {
				return true
			}
		}
		return false
	}
	for _, c := range e.Catalogs {
		if c.Ref == ref || c.Metadata.Name == ref || (c.Alias != "" && c.Alias == ref) {
			return true
		}
	}
	return false
}

func (e *Effective) isDisabled(id string) bool {
	for _, d := range e.Disabled {
		if d.ID == id {
			return true
		}
	}
	return false
}

// overrideAs applies an override made by the given layer.
func (e *Effective) overrideAs(r *model.Rule, rc RuleConf, where, layer string, byID map[string]*model.Rule) error {
	if rc.Enabled != nil && !*rc.Enabled {
		if r.Locked {
			return fmt.Errorf("%s: rules.%s: rule is locked by %s and cannot be disabled", where, r.ID, r.PolicySource())
		}
		if r.MinSeverity != "" {
			return fmt.Errorf("%s: rules.%s: %s set a severity floor of %s; off is below every floor", where, r.ID, r.PolicySource(), r.MinSeverity)
		}
		if rc.Reason == "" {
			return fmt.Errorf("%s: rules.%s: disabling a rule requires a reason", where, r.ID)
		}
		e.Disabled = append(e.Disabled, Disabled{ID: r.ID, Reason: rc.Reason, Where: where, Layer: layer})
		delete(byID, r.ID)
		return nil
	}
	if rc.Severity != "" {
		if err := e.applySeverity(r, rc, where, layer); err != nil {
			return err
		}
	}
	if err := checkParamsAndAccept(r, rc, where); err != nil {
		return err
	}
	mergeParams(r, rc)
	return nil
}

// checkParamsAndAccept refuses what would reshape a locked rule and
// validates accept entries; both can only widen what satisfies the rule.
func checkParamsAndAccept(r *model.Rule, rc RuleConf, where string) error {
	if len(rc.Params) == 0 && len(rc.Accept) == 0 {
		return nil
	}
	if r.Locked {
		return fmt.Errorf("%s: rules.%s: rule is locked by %s; params and accept cannot change (the locking catalog sets them)", where, r.ID, r.PolicySource())
	}
	if len(rc.Accept) == 0 {
		return nil
	}
	if r.Kind != model.KindOutcome {
		return fmt.Errorf("%s: rules.%s: accept only applies to outcome rules", where, r.ID)
	}
	return validateAccept(r.ID, rc, where)
}

func (e *Effective) applySeverity(r *model.Rule, rc RuleConf, where, layer string) error {
	sev, err := model.ParseSeverity(rc.Severity)
	if err != nil {
		return fmt.Errorf("%s: rules.%s: %w", where, r.ID, err)
	}
	if sev < r.Severity {
		if floor := r.Floor(); sev < floor {
			return fmt.Errorf("%s: rules.%s: severity %s is below the floor %s set by %s", where, r.ID, sev, floor, r.PolicySource())
		}
		if rc.Reason == "" {
			return fmt.Errorf("%s: rules.%s: lowering severity requires a reason", where, r.ID)
		}
		e.Weakened = append(e.Weakened, Disabled{ID: r.ID, Reason: rc.Reason, Where: where, Layer: layer})
	}
	r.Severity = sev
	return nil
}

func mergeParams(r *model.Rule, rc RuleConf) {
	if len(rc.Params) == 0 && len(rc.Accept) == 0 {
		return
	}
	if r.Params == nil {
		r.Params = map[string]any{}
	}
	for k, v := range rc.Params {
		r.Params[k] = v
	}
	if len(rc.Accept) > 0 {
		accept := make([]any, 0, len(rc.Accept))
		for _, a := range rc.Accept {
			accept = append(accept, a)
		}
		r.Params["accept"] = accept
	}
}

func validateAccept(id string, rc RuleConf, where string) error {
	for i, a := range rc.Accept {
		if _, ok := a["expr"].(string); !ok {
			return fmt.Errorf("%s: rules.%s: accept[%d] needs an expr", where, id, i)
		}
		for k := range a {
			if k != "expr" && k != "name" && k != "when" {
				return fmt.Errorf("%s: rules.%s: accept[%d]: unknown key %q (want name, when, expr)", where, id, i, k)
			}
		}
	}
	return nil
}

func expired(until string, now time.Time) bool {
	if until == "" {
		return false
	}
	t, err := time.Parse("2006-01-02", until)
	return err == nil && now.After(t.Add(24*time.Hour))
}

func (e *Effective) hasCatalog(c *catalog.Catalog) bool {
	for _, have := range e.Catalogs {
		if have.Digest == c.Digest {
			return true
		}
	}
	return false
}
