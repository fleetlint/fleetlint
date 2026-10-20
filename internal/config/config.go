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

	"github.com/fleetlint/fleetlint/internal/yamlsafe"

	"github.com/goccy/go-yaml"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/celenv"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/fix/compose"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/repo"
	"github.com/fleetlint/fleetlint/internal/rootfs"
	"github.com/fleetlint/fleetlint/internal/stacks"
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

// Scope is a directory evaluated as its own project. `path: .` is the
// repository root: it pins the root's stacks and task runner and adds no
// scope.
type Scope struct {
	Path string `yaml:"path"`
	// Stacks and TaskRunner pin what detection finds in this directory;
	// nothing else inherits them.
	Stacks     []string `yaml:"stacks,omitempty"`
	TaskRunner string   `yaml:"taskrunner,omitempty"`
	// Facts is the 0.2 way to write Stacks; deprecated, still read. Its
	// tier, when set, is the scope's tier.
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
// Label is who did it: repo for configuration files, org or team for a
// catalog that lowered a rule of an earlier layer.
type Disabled struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
	Where  string `json:"where"`
	Label  string `json:"label"`
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
	// Root holds the root directory's stacks and task runner: the
	// `path: .` entry of scopes, or the deprecated facts.stacks and
	// facts.taskrunner.
	Root Scope
	// Deprecations name configuration forms that still work but will go;
	// the CLI prints them as warnings.
	Deprecations []string
	// Stacks are the stack definitions of the catalog source.
	Stacks []stacks.Def
	// aliases are the catalog's renamed rule ids and their replacements.
	aliases map[string][]string
	raw     []byte
}

// renamed returns the ids that replace an old rule id no loaded rule covers,
// and records the deprecation once; nil when the key is not renamed.
func (e *Effective) renamed(key, where string, byID map[string]*model.Rule) []string {
	repl, ok := e.aliases[key]
	if !ok {
		return nil
	}
	for _, r := range byID {
		if r.Covers(key) {
			return nil // an id still in use (a catalog's own rule) keeps its meaning
		}
	}
	msg := fmt.Sprintf("%s: %s was renamed to %s; write the new id", where, key, strings.Join(repl, ", "))
	if !slices.Contains(e.Deprecations, msg) {
		e.Deprecations = append(e.Deprecations, msg)
	}
	return repl
}

// renameOverrides rewrites override keys that name a renamed rule. An
// override written for the new id wins over one carried over from the old.
func (e *Effective) renameOverrides(overrides map[string]catalog.Override, where string, byID map[string]*model.Rule) map[string]catalog.Override {
	out := make(map[string]catalog.Override, len(overrides))
	for key, ov := range overrides {
		if _, done := out[key]; !done {
			out[key] = ov
		}
		repl := e.renamed(key, where, byID)
		if repl == nil {
			continue
		}
		delete(out, key)
		for _, id := range repl {
			if _, explicit := overrides[id]; !explicit {
				out[id] = ov
			}
		}
	}
	return out
}

// RenameExceptions points exceptions written for a renamed rule at the
// rules that replace it.
func (e *Effective) RenameExceptions(exs []model.Exception, where string) []model.Exception {
	byID := make(map[string]*model.Rule, len(e.Rules))
	for i := range e.Rules {
		byID[e.Rules[i].ID] = &e.Rules[i]
	}
	return e.renameExceptions(exs, where, byID)
}

func (e *Effective) renameExceptions(exs []model.Exception, where string, byID map[string]*model.Rule) []model.Exception {
	out := make([]model.Exception, 0, len(exs))
	for _, ex := range exs {
		repl := e.renamed(ex.Rule, where, byID)
		if repl == nil {
			out = append(out, ex)
			continue
		}
		for _, id := range repl {
			ex.Rule = id
			out = append(out, ex)
		}
	}
	return out
}

// loadStacks reads the source's stack definitions and checks that every
// stack the configuration pins is one of them.
func (e *Effective) loadStacks() error {
	src := e.Source
	if src == nil {
		src = catalog.Builtin()
	}
	defs, err := src.StackDefs()
	if err != nil {
		return fmt.Errorf("catalog %s: %w", src.Version, err)
	}
	e.Stacks = defs
	pinned := []Scope{e.Root}
	if e.File.Scopes != nil {
		pinned = append(pinned, *e.File.Scopes...)
	}
	if err := checkStacks(pinned, defs, func(id string) bool { return id != stacks.Any }); err != nil {
		return fmt.Errorf("%s: %w (catalog %s has: %s)", FileName, err, src.Version, strings.Join(stacks.IDs(defs), ", "))
	}
	for _, r := range e.Rules {
		if err := checkStacks([]Scope{{Stacks: r.Stacks}}, defs, func(string) bool { return true }); err != nil {
			return fmt.Errorf("rule %s (%s): %w (catalog %s has: %s)", r.ID, r.Source, err, src.Version, strings.Join(stacks.IDs(defs), ", "))
		}
	}
	return nil
}

// checkStacks refuses a stack the catalog does not define; anyOK says
// whether "any" may stand for every stack.
func checkStacks(scopes []Scope, defs []stacks.Def, anyOK func(string) bool) error {
	for _, sc := range scopes {
		for _, id := range sc.Stacks {
			if _, ok := stacks.Find(defs, id); ok || (id == stacks.Any && anyOK(id)) {
				continue
			}
			return fmt.Errorf("stack %q is not defined", id)
		}
	}
	return nil
}

// FixTemplates returns the templates for `fix`: files shipped with the
// catalogs the repository extends, later layers first, over the preset
// source's templates.
func (e *Effective) FixTemplates() compose.Templates {
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
	b, err := rootfs.ReadIn(root, FileName, maxConfigLen)
	if errors.Is(err, os.ErrNotExist) {
		// Accept the undotted name too, so a repo that chose it keeps working.
		if lb, lerr := rootfs.ReadIn(root, LegacyFileName, maxConfigLen); lerr == nil {
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
		if err := yamlsafe.Unmarshal(b, &eff.File, yaml.Strict()); err != nil {
			return nil, fmt.Errorf("%s: %w", FileName, err)
		}
	}
	if err := validateFile(&eff.File); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	eff.settleScopes()
	eff.raw = b
	opts.Loader.BaseDir, opts.Loader.Root = root, root
	if err := eff.resolve(opts); err != nil {
		return nil, err
	}
	if err := eff.validateScopeRules(); err != nil {
		return nil, err
	}
	if err := eff.loadStacks(); err != nil {
		return nil, err
	}
	if err := celenv.ValidateRules(eff.Rules); err != nil {
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
	return validateScopes(f.Scopes)
}

func validateScopes(scopes *[]Scope) error {
	if scopes == nil {
		return nil
	}
	roots := 0
	for _, s := range *scopes {
		if s.Path == "" || strings.HasPrefix(s.Path, "..") || filepath.IsAbs(s.Path) {
			return fmt.Errorf("scopes: path %q must be a relative path inside the repository", s.Path)
		}
		if isRootPath(s.Path) {
			roots++
		}
		for _, r := range []string{s.TaskRunner, s.Facts.TaskRunner} {
			if r != "" && !slices.Contains(repo.RunnerKinds(), r) {
				return fmt.Errorf("scopes %s: taskrunner must be one of %s (got %q)", s.Path, strings.Join(repo.RunnerKinds(), ", "), r)
			}
		}
	}
	if roots > 1 {
		return errors.New("scopes: the root (path: .) is listed more than once")
	}
	return nil
}

// validateChoices checks the settings that name a tool or a version.
func validateChoices(f *File) error {
	if pin := f.Catalog; pin != nil {
		if err := pin.Validate(); err != nil {
			return err
		}
		if pin.Repo != "" {
			if err := catalog.ValidRepoURL(pin.Repo); err != nil {
				return err
			}
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
	overrides = e.renameOverrides(overrides, where, byID)
	for _, key := range overrideKeys(overrides) {
		ov := overrides[key]
		if err := adjustmentOnly(ov, where, key); err != nil {
			return nil, err
		}
		ids, err := e.matchRuleIDs(key, byID, where)
		if err != nil {
			return nil, err
		}
		if err := e.applyToAll(ids, key, nil, ov, where, catalog.LabelRepo, where, byID); err != nil {
			return nil, err
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

// maxConfigLen bounds a configuration file.
const maxConfigLen = 1 << 20

// LoadNested reads <scope>/.fleetlint.yaml below the repository root. A
// missing file yields nil.
func LoadNested(root, scope string, now time.Time) (*Nested, error) {
	dir := filepath.Join(root, filepath.FromSlash(scope))
	b, err := rootfs.ReadIn(root, filepath.Join(filepath.FromSlash(scope), FileName), maxConfigLen)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil //nolint:nilnil // absence is the normal case, not an error
	}
	if err != nil {
		return nil, err
	}
	var n Nested
	if err := yamlsafe.Unmarshal(b, &n, yaml.Strict()); err != nil {
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

// settleScopes moves the root's settings out of the scope list and reads
// the 0.2 forms: facts.stacks and facts.taskrunner at the top (the root's,
// no longer inherited by every scope) and facts.stacks inside a scope.
func (e *Effective) settleScopes() {
	f := &e.File
	if len(f.Facts.Stacks) > 0 || f.Facts.TaskRunner != "" {
		e.Root.Stacks, e.Root.TaskRunner = f.Facts.Stacks, f.Facts.TaskRunner
		e.Deprecations = append(e.Deprecations, "facts.stacks and facts.taskrunner are deprecated: write them under `scopes:` as `- {path: ., stacks: [...], taskrunner: ...}`; they apply to the root directory only")
		f.Facts.Stacks, f.Facts.TaskRunner = nil, ""
	}
	if f.Scopes == nil {
		return
	}
	kept := make([]Scope, 0, len(*f.Scopes))
	for _, sc := range *f.Scopes {
		if readOldScopeFacts(&sc) {
			e.Deprecations = append(e.Deprecations, fmt.Sprintf("scopes %s: facts.stacks and facts.taskrunner are deprecated: write `stacks:` and `taskrunner:` on the scope itself", sc.Path))
		}
		if isRootPath(sc.Path) {
			e.Root.Stacks, e.Root.TaskRunner = firstList(sc.Stacks, e.Root.Stacks), firstNonEmpty(sc.TaskRunner, e.Root.TaskRunner)
			continue
		}
		kept = append(kept, sc)
	}
	if len(kept) == 0 && len(*f.Scopes) > 0 {
		f.Scopes = nil // only the root was listed: members are still discovered
		return
	}
	f.Scopes = &kept
}

// readOldScopeFacts takes a scope's 0.2 `facts: {stacks, taskrunner}` as
// its stacks and task runner and reports whether there were any.
func readOldScopeFacts(sc *Scope) bool {
	if len(sc.Facts.Stacks) == 0 && sc.Facts.TaskRunner == "" {
		return false
	}
	sc.Stacks = firstList(sc.Stacks, sc.Facts.Stacks)
	sc.TaskRunner = firstNonEmpty(sc.TaskRunner, sc.Facts.TaskRunner)
	return true
}

func isRootPath(p string) bool { return p == "." || p == "./" }

func firstList(lists ...[]string) []string {
	for _, l := range lists {
		if len(l) > 0 {
			return l
		}
	}
	return nil
}

// RepoFacts are the repository-wide pinned facts: tier, visibility, forge
// and container. Stacks and task runner are per directory (Root, Scopes).
func (e *Effective) RepoFacts() facts.Overrides {
	o := e.File.Facts.ToFacts()
	o.Stacks, o.TaskRunner = nil, ""
	o.StackDefs = e.Stacks
	return o
}

// ScopeFacts are the pinned facts of one directory: the repository-wide
// ones plus its own stacks and task runner.
func (e *Effective) ScopeFacts(sc Scope) facts.Overrides {
	o := e.RepoFacts()
	if sc.Path == "" {
		sc = e.Root
	}
	o.Stacks, o.TaskRunner = sc.Stacks, sc.TaskRunner
	if sc.Facts.Tier != 0 {
		o.Tier = sc.Facts.Tier
	}
	return o
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
	if e.aliases, err = e.Source.Aliases(); err != nil {
		return fmt.Errorf("catalog %s: aliases.yaml: %w", e.Source.Version, err)
	}
	loaded, err := e.loadEntries(opts.Loader, entries)
	if err != nil {
		return err
	}
	// Catalogs apply in the order includes and extends list them: the
	// sources file's baseline first, then the repository's list as written.
	// Nothing is reordered; policy only tightens, so whoever lists a catalog
	// first binds the ones after it.
	own, err := e.ownCatalog(opts.Loader)
	if err != nil {
		return err
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

// ownCatalog is the repository's configuration as the last catalog of the run.
func (e *Effective) ownCatalog(l catalog.Loader) (*catalog.Catalog, error) {
	for _, r := range e.File.Rules {
		if r.Locked || r.MinSeverity != "" || r.AllowExceptions != nil {
			return nil, fmt.Errorf("%s: rules.%s: locked, min_severity and exceptions are policy a catalog sets; the repository's own rules have no layer below them to bind", FileName, r.ID)
		}
	}
	own, err := catalog.Local(FileName, e.raw, e.File.Rules, e.File.Overrides, e.File.Exceptions)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	if err := l.Expand(own); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	return own, nil
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
	if err := e.Pin.Validate(); err != nil {
		return fmt.Errorf("%s: %w", FileName, err)
	}
	var err error
	if e.Pin.OCI != "" {
		e.Source, err = l.PinOCI(e.Pin.OCI)
	} else {
		e.Source, err = l.Pin(e.Pin.Repo, e.Pin.Version)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", FileName, err)
	}
	return nil
}

// loadEntries loads every extends entry with its includes, labels each
// catalog and checks that it builds on the run's catalog version.
func (e *Effective) loadEntries(l catalog.Loader, entries []extendsEntry) ([]*catalog.Catalog, error) {
	var out []*catalog.Catalog
	for _, en := range entries {
		cats, err := l.Load(en.ref)
		if err != nil {
			return nil, err
		}
		for _, c := range cats {
			settleLabel(c, en)
			if c.Metadata.Layer != "" && c.Metadata.Label == c.Metadata.Layer {
				e.Deprecations = append(e.Deprecations, fmt.Sprintf("catalog %s: metadata.layer is deprecated: write it as metadata.label", c.Ref))
			}
			if err := e.checkBuildsOn(c); err != nil {
				return nil, err
			}
			out = append(out, c)
		}
	}
	return out, nil
}

// notFromRepository reports a built-in preset or a catalog fetched by
// digest, commit or signature.
func notFromRepository(c *catalog.Catalog) bool {
	return catalog.RefLabel(c.Ref) == catalog.LabelPreset || catalog.IsRemoteRef(c.Ref)
}

// settleLabel gives a catalog its label and records how it came in. Presets
// are `preset`; any other catalog carries the label it declares, else the
// sources-file name it is loaded under, else its own name. The label
// identifies the catalog in reports; it grants nothing.
func settleLabel(c *catalog.Catalog, en extendsEntry) {
	c.Via = firstNonEmpty(en.alias, en.ref)
	switch {
	case catalog.RefLabel(c.Ref) == catalog.LabelPreset:
		c.Label = catalog.LabelPreset
	case c.Ref != en.ref:
		// An include of the entry: labelled by itself.
		c.Label = firstNonEmpty(c.Metadata.Label, c.Metadata.Name)
	default:
		c.Alias = en.alias
		c.Label = firstNonEmpty(c.Metadata.Label, en.alias, c.Metadata.Name)
	}
}

// checkBuildsOn holds a catalog to the run's catalog version: what it
// declares must be the version in use, and a remote catalog that includes
// presets or selects library rules must declare one, or its content would
// depend on whichever fleetlint reads it.
func (e *Effective) checkBuildsOn(c *catalog.Catalog) error {
	if catalog.RefLabel(c.Ref) == catalog.LabelPreset {
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
		if catalog.RefLabel(inc) == catalog.LabelPreset {
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
	ref, alias string
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
		target, known := "", false
		if src != nil {
			target, known = src.Catalogs[ref]
		}
		switch {
		case known:
			entries = append(entries, extendsEntry{ref: target, alias: ref})
		case catalog.LooksLikeAlias(ref) && src == nil:
			return nil, nil, fmt.Errorf("%s: extends: %q is a sources-file name but no sources file is set", FileName, ref)
		case catalog.LooksLikeAlias(ref):
			return nil, nil, fmt.Errorf("%s: extends: %q is not in the sources file (it has: %s)", FileName, ref, strings.Join(src.Aliases(), ", "))
		default:
			entries = append(entries, extendsEntry{ref: ref})
		}
	}
	return entries, src, nil
}

func (e *Effective) addCatalog(c *catalog.Catalog, byID map[string]*model.Rule, order *[]string, now time.Time) error {
	if e.hasCatalog(c) {
		return nil
	}
	if other := e.sameNameOtherVersion(c); other != nil {
		return fmt.Errorf("catalog %s is %s@%s but %s@%s is already loaded from %s: one version of a catalog per run (align the pin in the sources file and in the catalogs that include it)", c.Ref, c.Metadata.Name, c.Metadata.Version, other.Metadata.Name, other.Metadata.Version, other.Ref)
	}
	e.Catalogs = append(e.Catalogs, c)
	for i := range c.Rules {
		r := c.Rules[i]
		if !c.Trusted && r.Kind == model.KindCommand {
			return fmt.Errorf("catalog %s: rule %s: command rules are only allowed in the repository's own config", c.Ref, r.ID)
		}
		r.Label = c.Label
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
	for _, ex := range e.renameExceptions(c.Exceptions, c.Ref, byID) {
		known := false
		for _, r := range byID {
			if !r.Covers(ex.Rule) {
				continue
			}
			known = true
			if !r.ExceptionsAllowed() {
				return fmt.Errorf("%s: exceptions: rule %s forbids exceptions (set by %s)", c.Ref, r.ID, r.PolicySource())
			}
		}
		if !known && !e.isDisabled(ex.Rule) {
			return fmt.Errorf("%s: exceptions: rule %q is not loaded", c.Ref, ex.Rule)
		}
		ex.Label, ex.Where, ex.Expired = c.Label, c.Ref, expired(ex.Until, now)
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
	overrides := e.renameOverrides(c.Overrides, where, byID)
	for _, key := range overrideKeys(overrides) {
		ov := overrides[key]
		if c.Ref == FileName {
			if err := adjustmentOnly(ov, FileName, key); err != nil {
				return err
			}
		}
		ids, err := e.matchRuleIDs(key, byID, where)
		if err != nil {
			return err
		}
		if err := e.applyToAll(ids, key, inline, ov, where, c.Label, by, byID); err != nil {
			return err
		}
	}
	return nil
}

// applyToAll applies one override to the matched rules, skipping the
// catalog's own definitions: those are written as intended, overrides adjust
// what the catalog took in.
func (e *Effective) applyToAll(ids []string, key string, inline map[string]bool, ov catalog.Override, where, layer, by string, byID map[string]*model.Rule) error {
	if len(ov.Accept) > 0 && !strings.ContainsAny(key, "*?[") && !slices.ContainsFunc(ids, func(id string) bool { return byID[id].Kind == model.KindOutcome }) {
		return fmt.Errorf("%s: rules.%s: accept only applies to outcome rules, and none of the rules it names is one", where, key)
	}
	for _, id := range ids {
		if inline[id] {
			continue
		}
		if _, still := byID[id]; !still {
			continue // disabled through its capability earlier in this loop
		}
		if err := e.applyOverride(byID[id], overrideTarget(key, id), ov, where, layer, by, byID); err != nil {
			return err
		}
	}
	return nil
}

// overrideTarget is what an override key names for one matched rule: a
// pattern names each rule itself, an exact key the rule or its capability.
func overrideTarget(key, id string) string {
	if strings.ContainsAny(key, "*?[") {
		return id
	}
	return key
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
func (e *Effective) matchRuleIDs(key string, byID map[string]*model.Rule, where string) ([]string, error) {
	if !strings.ContainsAny(key, "*?[") {
		// A capability's id reaches its implementations as well.
		var ids []string
		for id, r := range byID {
			if r.Covers(key) {
				ids = append(ids, id)
			}
		}
		if capID, _, ok := model.SplitImplementation(key); ok && len(ids) == 0 && e.isDisabled(capID) {
			return nil, nil // switched off with its capability: nothing left to adjust
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("%s: rules.%s: no such rule in the catalogs loaded before this one", where, key)
		}
		sort.Strings(ids)
		return ids, nil
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

// applyOverride applies the override written for key to r: the rule itself,
// or an implementation of the capability key names. Through a capability,
// `when` gates the implementation instead of replacing its own condition,
// and accept reaches only the outcome implementations.
func (e *Effective) applyOverride(r *model.Rule, key string, ov catalog.Override, where, layer, by string, byID map[string]*model.Rule) error {
	viaCapability := r.ID != key
	rc, err := overrideConf(r, ov, where, viaCapability)
	if err != nil {
		return err
	}
	before := len(e.Disabled)
	if err := e.overrideAs(r, rc, where, layer, byID); err != nil {
		return err
	}
	if _, still := byID[r.ID]; !still {
		// Switched off through its capability: the report names the capability once.
		if viaCapability && len(e.Disabled) == before+1 && slices.ContainsFunc(e.Disabled[:before], func(d Disabled) bool { return d.ID == key && d.Where == where }) {
			e.Disabled = e.Disabled[:before]
		}
		return nil
	}
	if err := applyScope(r, ov, where, viaCapability); err != nil {
		return err
	}
	return tightenPolicy(r, ov, where, by)
}

// overrideConf is the adjustment an override makes to one rule: accept
// reaches outcome rules only, a capability takes no params of its own, and
// raise becomes a severity.
func overrideConf(r *model.Rule, ov catalog.Override, where string, viaCapability bool) (RuleConf, error) {
	rc := RuleConf{Enabled: ov.Enabled, Reason: ov.Reason, Severity: ov.Severity, Params: ov.Params, Accept: ov.Accept}
	if r.Kind == model.KindCapability || (viaCapability && r.Kind != model.KindOutcome) {
		rc.Accept = nil
	}
	if r.Kind == model.KindCapability {
		rc.Params = nil
	}
	if ov.Raise == 0 {
		return rc, nil
	}
	if ov.Raise < 0 || ov.Severity != "" {
		return rc, fmt.Errorf("%s: rules.%s: raise must be positive and not combined with severity", where, r.ID)
	}
	rc.Severity = min(r.Severity+model.Severity(ov.Raise), model.SeverityError).String()
	return rc, nil
}

// applyScope replaces a rule's `when` and `tiers` from a catalog override.
// A locked rule keeps its applicability: narrowing either would weaken it.
func applyScope(r *model.Rule, ov catalog.Override, where string, viaCapability bool) error {
	if ov.When == nil && len(ov.Tiers) == 0 {
		return nil
	}
	if r.Locked {
		return fmt.Errorf("%s: rules.%s: rule is locked by %s; when and tiers cannot change", where, r.ID, r.PolicySource())
	}
	switch {
	case ov.When != nil && viaCapability:
		r.Gate = *ov.When
	case ov.When != nil:
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
	if err := keepExceptionPolicy(c, prev, r); err != nil {
		return err
	}
	if r.Severity < prev.Severity {
		e.Weakened = append(e.Weakened, Disabled{
			ID: r.ID, Where: c.Ref, Label: r.Label,
			Reason: fmt.Sprintf("%s lowers %s to %s", r.Source, prev.Severity, r.Severity),
		})
	}
	return nil
}

// keepExceptionPolicy binds a redefinition to the earlier policy: a
// forbidden exception stays forbidden, and the earlier catalog stays named
// as the one that set it.
func keepExceptionPolicy(c *catalog.Catalog, prev, r *model.Rule) error {
	if !prev.ExceptionsAllowed() {
		if r.AllowExceptions != nil && *r.AllowExceptions {
			return fmt.Errorf("catalog %s: rule %s: exceptions were forbidden by %s and cannot be allowed again", c.Ref, r.ID, prev.PolicySource())
		}
		r.AllowExceptions = prev.AllowExceptions
	}
	if r.PolicyBy == "" && (prev.MinSeverity != "" || !prev.ExceptionsAllowed()) {
		r.PolicyBy = prev.PolicySource()
	}
	return nil
}

// CheckExceptions validates exceptions declared by a scope or nested file
// against the rules' policy; where names the file for the error.
func (e *Effective) CheckExceptions(where string, exs []model.Exception) error {
	for _, ex := range exs {
		for i := range e.Rules {
			if e.Rules[i].Covers(ex.Rule) && !e.Rules[i].ExceptionsAllowed() {
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
		return slices.ContainsFunc(e.Catalogs, func(c *catalog.Catalog) bool {
			return c.Digest == want || (c.Artifact != "" && c.Artifact == want)
		})
	}
	return slices.ContainsFunc(e.Catalogs, func(c *catalog.Catalog) bool { return satisfies(c, ref) })
}

// satisfies matches a required name or ref. A name or alias counts only for
// a catalog the repository did not write: a file in the repository could
// call itself anything.
func satisfies(c *catalog.Catalog, ref string) bool {
	if c.Ref == ref {
		return true
	}
	named := c.Metadata.Name == ref || (c.Alias != "" && c.Alias == ref)
	return named && notFromRepository(c)
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
		e.Disabled = append(e.Disabled, Disabled{ID: r.ID, Reason: rc.Reason, Where: where, Label: layer})
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
		e.Weakened = append(e.Weakened, Disabled{ID: r.ID, Reason: rc.Reason, Where: where, Label: layer})
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
	for _, a := range rc.Accept {
		s := model.Satisfier{}
		s.Name, _ = a["name"].(string)
		s.When, _ = a["when"].(string)
		s.Expr, _ = a["expr"].(string)
		r.Accept = append(r.Accept, s)
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

// sameNameOtherVersion finds a loaded fetched catalog with the same name as
// c but other content: the sources file pins one version and a catalog
// includes another, say. Files of the repository are not compared: their
// names are the repository's business.
func (e *Effective) sameNameOtherVersion(c *catalog.Catalog) *catalog.Catalog {
	if !notFromRepository(c) || catalog.RefLabel(c.Ref) == catalog.LabelPreset {
		return nil
	}
	for _, have := range e.Catalogs {
		if notFromRepository(have) && catalog.RefLabel(have.Ref) != catalog.LabelPreset && have.Metadata.Name == c.Metadata.Name && have.Digest != c.Digest {
			return have
		}
	}
	return nil
}

func (e *Effective) hasCatalog(c *catalog.Catalog) bool {
	for _, have := range e.Catalogs {
		if have.Digest == c.Digest {
			return true
		}
	}
	return false
}
