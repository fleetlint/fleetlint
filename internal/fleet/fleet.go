// Package fleet runs the check across many repositories and renders one
// view: repo × rule, compliance per repo and per rule, every disabled rule
// and exception with its reason, and a per-repo task list for an agent. It
// is stateless: history is a directory of dated JSON files the caller keeps.
package fleet

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/repo"
)

// Spec is the fleet file: which repositories to check.
type Spec struct {
	Version int     `yaml:"version"`
	Cache   string  `yaml:"cache,omitempty"`
	Repos   []Entry `yaml:"repos"`
	// Defaults apply to every repository, Groups to the repositories of a
	// group (discovered ones too); an entry's own values win.
	Defaults Settings            `yaml:"defaults,omitempty"`
	Groups   map[string]Settings `yaml:"groups,omitempty"`
	// Unversioned is filled by discovery, never read from a fleet file.
	Unversioned []string `yaml:"-"`
}

// Settings are the values a fleet file sets for many repositories at once.
type Settings struct {
	Team       string `yaml:"team,omitempty"`
	Tier       int    `yaml:"tier,omitempty"`
	Visibility string `yaml:"visibility,omitempty"`
}

func (s Settings) validate() error {
	if s.Tier < 0 || s.Tier > 3 {
		return fmt.Errorf("tier %d must be 1, 2 or 3", s.Tier)
	}
	if s.Visibility != "" && s.Visibility != string(facts.VisibilityPublic) && s.Visibility != string(facts.VisibilityPrivate) {
		return fmt.Errorf("visibility %q must be public or private", s.Visibility)
	}
	return nil
}

// settle fills what the entry leaves open from its group, then the defaults.
func (s *Spec) settle(e Entry) Entry {
	for _, from := range []Settings{s.Groups[e.Group], s.Defaults} {
		e.Team = firstNonEmpty(e.Team, from.Team)
		e.Visibility = firstNonEmpty(e.Visibility, from.Visibility)
		if e.Tier == 0 {
			e.Tier = from.Tier
		}
	}
	return e
}

// Entry is one repository: a local path or a git URL (cloned into the cache).
type Entry struct {
	Name string `yaml:"name,omitempty"`
	Path string `yaml:"path,omitempty"`
	URL  string `yaml:"url,omitempty"`
	Team string `yaml:"team,omitempty"`
	// Group is where the repository sits: the directory it was discovered
	// under, or what the fleet file says. Reports group by it.
	Group string `yaml:"group,omitempty"`
	// Visibility (public or private) is what the forge says about the
	// repository; it applies when the repository does not say itself.
	Visibility string `yaml:"visibility,omitempty"`
	// Tier applies when the repository's configuration does not pin one.
	Tier int `yaml:"tier,omitempty"`
	// Token authenticates the clone over https the way the forge API call
	// was authenticated; set by discovery, never read from a fleet file.
	Token string `yaml:"-"`
}

// Normalize validates an entry built outside a fleet file.
func (e *Entry) Normalize() error { return e.normalize("") }

// LoadSpec reads and validates a fleet file.
func LoadSpec(path string) (*Spec, error) {
	b, err := os.ReadFile(path) //nolint:gosec // the user names the fleet file
	if err != nil {
		return nil, err
	}
	var s Spec
	if err := yaml.UnmarshalWithOptions(b, &s, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.Version != 1 {
		return nil, fmt.Errorf("%s: version must be 1", path)
	}
	if err := s.Defaults.validate(); err != nil {
		return nil, fmt.Errorf("%s: defaults: %w", path, err)
	}
	for name, g := range s.Groups {
		if err := g.validate(); err != nil {
			return nil, fmt.Errorf("%s: groups.%s: %w", path, name, err)
		}
	}
	base := filepath.Dir(path)
	names := map[string]bool{}
	for i := range s.Repos {
		e := &s.Repos[i]
		if err := e.normalize(base); err != nil {
			return nil, fmt.Errorf("%s: repos[%d]: %w", path, i, err)
		}
		if names[e.Name] {
			return nil, fmt.Errorf("%s: duplicate repository name %q", path, e.Name)
		}
		names[e.Name] = true
	}
	return &s, nil
}

// normalize validates one entry, resolves its path against the fleet file
// and derives a name safe for use as a file name.
func (e *Entry) normalize(base string) error {
	if (e.Path == "") == (e.URL == "") {
		return errors.New("exactly one of path or url")
	}
	if e.Path != "" && !filepath.IsAbs(e.Path) {
		e.Path = filepath.Join(base, e.Path)
	}
	if e.URL != "" && !validCloneURL(e.URL) {
		return fmt.Errorf("url %q must start with https://, ssh:// or git@ (or be an absolute local path)", e.URL)
	}
	if err := (Settings{Tier: e.Tier, Visibility: e.Visibility}).validate(); err != nil {
		return err
	}
	if e.Name == "" {
		e.Name = deriveName(*e)
	}
	e.Name = unsafeName.ReplaceAllString(e.Name, "-")
	if strings.Trim(e.Name, ".-") == "" {
		return errors.New("name is empty after sanitizing")
	}
	return nil
}

// Options control a fleet run.
type Options struct {
	Deep bool
	// Require lists catalogs every repository must extend; a repository that
	// does not is reported as an error, not silently checked without them.
	Require []string
	Update  bool // git fetch/pull cached clones
	Now     time.Time
	Cache   string
	// Version names the binary in the report.
	Version string
	// MarkFixable flags the results `fleetlint fix --apply` can repair; the
	// planner lives with the fix command, so the caller passes it in.
	MarkFixable func(*repo.Repo, *engine.Run)
	// Visibility asks the forge when neither the entry nor the repository
	// says whether it is public; nil asks no one.
	Visibility func(context.Context, *repo.Repo, facts.Facts) facts.Visibility
}

// RepoReport is one repository's outcome.
type RepoReport struct {
	Name       string            `json:"name"`
	Team       string            `json:"team,omitempty"`
	Group      string            `json:"group,omitempty"`
	Source     string            `json:"source"`
	Git        *GitState         `json:"git,omitempty"`
	Facts      facts.Facts       `json:"facts"`
	Summary    model.Counts      `json:"summary"`
	Results    []model.Result    `json:"results"`
	Disabled   []config.Disabled `json:"disabled,omitempty"`
	Weakened   []config.Disabled `json:"weakened,omitempty"`
	Exceptions []model.Exception `json:"exceptions,omitempty"`
	Err        string            `json:"error,omitempty"`
}

// Compliance is the share of applicable rules that pass or are excepted.
// Rules that could not be evaluated say nothing about the repository and
// are left out; reports list them separately.
func (r RepoReport) Compliance() float64 {
	applicable := r.Summary.Pass + r.Summary.Fail + r.Summary.Excepted + r.Summary.Baselined
	if applicable == 0 {
		return 0 // nothing applied: no evidence of compliance
	}
	// Baselined findings are acknowledged debt, not compliance.
	return float64(r.Summary.Pass+r.Summary.Excepted) / float64(applicable)
}

// Fixable counts the failing results `fleetlint fix --apply` can repair.
func (r RepoReport) Fixable() int {
	n := 0
	for _, res := range r.Results {
		if res.Status == model.StatusFail && res.Fixable {
			n++
		}
	}
	return n
}

// urgent orders repositories for triage: unchecked ones first, then by
// errors, rule errors and warnings, then by the lowest compliance.
func urgent(a, b RepoReport) bool {
	if (a.Err != "") != (b.Err != "") {
		return a.Err != ""
	}
	ka := [3]int{a.Summary.Errors, a.Summary.Error, a.Summary.Warnings}
	kb := [3]int{b.Summary.Errors, b.Summary.Error, b.Summary.Warnings}
	if ka != kb {
		for i := range ka {
			if ka[i] != kb[i] {
				return ka[i] > kb[i]
			}
		}
	}
	if ca, cb := a.Compliance(), b.Compliance(); ca != cb {
		return ca < cb
	}
	return strings.ToLower(a.Name) < strings.ToLower(b.Name)
}

// Report is the whole fleet.
type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	// Tool and Catalog are the fleetlint version and the catalog version
	// built into it: which rules produced this report.
	Tool    string       `json:"tool,omitempty"`
	Catalog string       `json:"catalog,omitempty"`
	Repos   []RepoReport `json:"repos"`
	// Rules is the union of rule ids seen, sorted, for the matrix columns.
	Rules []string `json:"rules"`
	// Teams summarizes the repositories per team; empty when no entry names one.
	Teams []TeamSummary `json:"teams,omitempty"`
	// Groups does the same per group.
	Groups []TeamSummary `json:"groups,omitempty"`
	// Unversioned lists project directories discovery found outside any
	// repository of their own; nothing can check them.
	Unversioned []string `json:"unversioned,omitempty"`
}

// Run checks every repository in the spec.
func Run(ctx context.Context, spec *Spec, opts Options) (*Report, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	rep := &Report{GeneratedAt: opts.Now, Tool: opts.Version, Catalog: catalog.ModuleVersion()}
	seen := map[string]bool{}
	for _, e := range spec.Repos {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		rr := checkOne(ctx, spec.settle(e), spec, opts)
		rep.Repos = append(rep.Repos, rr)
		for _, res := range rr.Results {
			seen[res.Rule.ID] = true
		}
	}
	for id := range seen {
		rep.Rules = append(rep.Rules, id)
	}
	sort.Strings(rep.Rules)
	sort.SliceStable(rep.Repos, func(i, j int) bool { return urgent(rep.Repos[i], rep.Repos[j]) })
	rep.Teams = summarize(rep.Repos, opts.Now, func(r RepoReport) string { return r.Team })
	rep.Groups = summarize(rep.Repos, opts.Now, func(r RepoReport) string { return r.Group })
	rep.Unversioned = spec.Unversioned
	return rep, nil
}

func checkOne(ctx context.Context, e Entry, spec *Spec, opts Options) RepoReport {
	rr := RepoReport{Name: e.Name, Team: e.Team, Group: e.Group, Source: firstNonEmpty(e.URL, e.Path)}
	path, err := materialize(ctx, e, firstNonEmpty(opts.Cache, spec.Cache), opts.Update)
	if err != nil {
		rr.Err = err.Error()
		return rr
	}
	r, err := repo.Open(ctx, path)
	if err != nil {
		rr.Err = err.Error()
		return rr
	}
	eff, err := config.Load(r.Root, config.Options{Now: opts.Now, Loader: catalog.RemoteLoader(ctx)})
	if err == nil {
		err = eff.Requires(opts.Require)
	}
	if err != nil {
		rr.Err = err.Error()
		return rr
	}
	run, err := evaluate(ctx, e, r, eff, opts)
	if err != nil {
		rr.Err = err.Error()
		return rr
	}
	rr.Git = gitState(r) //nolint:contextcheck // git lookups run under the repository's own bounded context
	rr.Facts, rr.Results, rr.Summary = run.Facts, run.Results, model.Summarize(run.Results)
	rr.Disabled, rr.Weakened = eff.Disabled, eff.Weakened
	rr.Exceptions = append(append([]model.Exception{}, eff.Exceptions...), run.ScopeExceptions...)
	return rr
}

// evaluate checks the repository with what the fleet knows about it.
func evaluate(ctx context.Context, e Entry, r *repo.Repo, eff *config.Effective, opts Options) (*engine.Run, error) {
	vis := facts.Visibility(e.Visibility)
	if vis == "" && opts.Visibility != nil {
		vis = opts.Visibility(ctx, r, facts.Discover(r, eff.File.Facts.ToFacts()))
	}
	run, err := engine.EvaluateWith(ctx, r, eff, engine.Options{Deep: opts.Deep, Visibility: vis, Tier: e.Tier})
	if err == nil && opts.MarkFixable != nil {
		opts.MarkFixable(r, run)
	}
	return run, err
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func deriveName(e Entry) string {
	src := firstNonEmpty(e.URL, e.Path)
	src = strings.TrimSuffix(strings.TrimRight(src, "/"), ".git")
	return unsafeName.ReplaceAllString(filepath.Base(src), "-")
}

// materialize returns a local path for the entry, cloning or updating a URL
// into the cache directory.
func materialize(ctx context.Context, e Entry, cache string, update bool) (string, error) {
	if e.Path != "" {
		if _, err := os.Stat(e.Path); err != nil {
			return "", fmt.Errorf("path %s: %w", e.Path, err)
		}
		return e.Path, nil
	}
	if cache == "" {
		return "", errors.New("a cache directory is required for url entries (fleet file `cache:` or --cache)")
	}
	if !validCloneURL(e.URL) {
		return "", fmt.Errorf("url %q must start with https://, ssh:// or git@ (or be an absolute local path)", e.URL)
	}
	cache = expandHome(cache)
	dest := filepath.Join(cache, unsafeName.ReplaceAllString(strings.TrimPrefix(strings.TrimPrefix(e.URL, "https://"), "git@"), "_"))
	if _, err := os.Stat(filepath.Join(dest, ".git")); err != nil {
		if err := os.MkdirAll(cache, 0o750); err != nil {
			return "", err
		}
		if err := gitRun(ctx, "", cloneAuth(e), "clone", "--quiet", "--depth=50", "--", e.URL, dest); err != nil {
			return "", err
		}
		return dest, nil
	}
	if update {
		if err := gitRun(ctx, dest, cloneAuth(e), "pull", "--quiet", "--ff-only"); err != nil {
			return "", err
		}
	}
	return dest, nil
}

// cloneAuth is the git configuration that sends the entry's token with
// every request to its host, as actions/checkout does: an Authorization
// header scoped to the URL, passed through the environment so it appears
// in no command line and no error message.
func cloneAuth(e Entry) []string {
	if e.Token == "" || !strings.HasPrefix(e.URL, "https://") {
		return nil
	}
	u, err := url.Parse(e.URL)
	if err != nil || u.Host == "" {
		return nil
	}
	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + e.Token))
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http." + u.Scheme + "://" + u.Host + "/.extraheader",
		"GIT_CONFIG_VALUE_0=AUTHORIZATION: basic " + basic,
	}
}

func validCloneURL(u string) bool {
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "ssh://") || strings.HasPrefix(u, "git@") || filepath.IsAbs(u)
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
