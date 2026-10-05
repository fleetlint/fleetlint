// Package fleet runs the check across many repositories and renders one
// view: repo × rule, compliance per repo and per rule, every disabled rule
// and exception with its reason, and a per-repo task list for an agent. It
// is stateless: history is a directory of dated JSON files the caller keeps.
package fleet

import (
	"context"
	"errors"
	"fmt"
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
}

// Entry is one repository: a local path or a git URL (cloned into the cache).
type Entry struct {
	Name string `yaml:"name,omitempty"`
	Path string `yaml:"path,omitempty"`
	URL  string `yaml:"url,omitempty"`
	Team string `yaml:"team,omitempty"`
	// Visibility (public or private) is what the forge says about the
	// repository; it applies when the repository does not say itself.
	Visibility string `yaml:"visibility,omitempty"`
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
	if e.Visibility != "" && e.Visibility != string(facts.VisibilityPublic) && e.Visibility != string(facts.VisibilityPrivate) {
		return fmt.Errorf("visibility %q must be public or private", e.Visibility)
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
}

// RepoReport is one repository's outcome.
type RepoReport struct {
	Name       string            `json:"name"`
	Team       string            `json:"team,omitempty"`
	Source     string            `json:"source"`
	Facts      facts.Facts       `json:"facts"`
	Summary    model.Counts      `json:"summary"`
	Results    []model.Result    `json:"results"`
	Disabled   []config.Disabled `json:"disabled,omitempty"`
	Weakened   []config.Disabled `json:"weakened,omitempty"`
	Exceptions []model.Exception `json:"exceptions,omitempty"`
	Err        string            `json:"error,omitempty"`
}

// Compliance is the share of applicable rules that pass or are excepted.
func (r RepoReport) Compliance() float64 {
	applicable := r.Summary.Pass + r.Summary.Fail + r.Summary.Excepted + r.Summary.Baselined + r.Summary.Error
	if applicable == 0 {
		return 0 // nothing applied: no evidence of compliance
	}
	// Baselined findings are acknowledged debt, not compliance.
	return float64(r.Summary.Pass+r.Summary.Excepted) / float64(applicable)
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
		rr := checkOne(ctx, e, spec, opts)
		rep.Repos = append(rep.Repos, rr)
		for _, res := range rr.Results {
			seen[res.Rule.ID] = true
		}
	}
	for id := range seen {
		rep.Rules = append(rep.Rules, id)
	}
	sort.Strings(rep.Rules)
	sort.Slice(rep.Repos, func(i, j int) bool { return rep.Repos[i].Name < rep.Repos[j].Name })
	rep.Teams = summarizeTeams(rep.Repos, opts.Now)
	return rep, nil
}

func checkOne(ctx context.Context, e Entry, spec *Spec, opts Options) RepoReport {
	rr := RepoReport{Name: e.Name, Team: e.Team, Source: firstNonEmpty(e.URL, e.Path)}
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
	run, err := engine.EvaluateWith(ctx, r, eff, engine.Options{Deep: opts.Deep, Visibility: facts.Visibility(e.Visibility)})
	if err != nil {
		rr.Err = err.Error()
		return rr
	}
	rr.Facts, rr.Results, rr.Summary = run.Facts, run.Results, model.Summarize(run.Results)
	rr.Disabled, rr.Weakened = eff.Disabled, eff.Weakened
	rr.Exceptions = append(append([]model.Exception{}, eff.Exceptions...), run.ScopeExceptions...)
	return rr
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
		if err := gitRun(ctx, "", "clone", "--quiet", "--depth=50", "--", e.URL, dest); err != nil {
			return "", err
		}
		return dest, nil
	}
	if update {
		if err := gitRun(ctx, dest, "pull", "--quiet", "--ff-only"); err != nil {
			return "", err
		}
	}
	return dest, nil
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
