// Package facts discovers what kind of repository we are looking at. Facts
// are computed once per run, carry their provenance (detected, configured or
// default) and are exposed to rules unchanged, so a surprising result can
// always be traced with `fleetlint facts`.
package facts

import (
	"net/url"
	"slices"
	"strings"

	"github.com/fleetlint/fleetlint/internal/ci"
	"github.com/fleetlint/fleetlint/internal/repo"
	"github.com/fleetlint/fleetlint/internal/stacks"
)

// Source says where a fact's value came from.
type Source string

// Fact provenance values.
const (
	SourceDetected   Source = "detected"
	SourceConfigured Source = "configured"
	SourceDefault    Source = "default"
	// SourceInferred is a value guessed from other facts (the tier from
	// visibility, release or CI); TierReason says from which.
	SourceInferred Source = "inferred"
	// SourceFleet is a value the fleet file set for the repository.
	SourceFleet Source = "fleet"
	// SourceForge is a value the forge's API reported.
	SourceForge Source = "forge"
)

// Visibility of the repository on its forge.
type Visibility string

// Visibility values.
const (
	VisibilityPublic  Visibility = "public"
	VisibilityPrivate Visibility = "private"
	VisibilityUnknown Visibility = "unknown"
)

// Facts is the repo model rules evaluate against.
type Facts struct {
	Name       string     `json:"name"`
	Stacks     []string   `json:"stacks"`
	Forge      string     `json:"forge"`
	ForgeHost  string     `json:"forge_host,omitempty"`
	Visibility Visibility `json:"visibility"`
	// Container says where the task runner's targets run: `none` is the
	// machine; `devcontainer` the repository's dev container (detected when a
	// devcontainer.json exists); `docker` and `podman` an image run with that
	// engine, set by configuration.
	Container string `json:"container"`
	Tier      int    `json:"tier"`
	// TierReason names the fact an inferred tier was derived from.
	TierReason string `json:"tier_reason,omitempty"`
	Layout     string `json:"layout"`
	// Members are workspace member directories (repo-relative), when Layout is "workspace".
	Members []string `json:"members,omitempty"`
	CI      []string `json:"ci"`
	Release Release  `json:"release"`
	// Artifacts are what the release publishes, one entry per artifact (a
	// kind, and a name where the kind names its instances), with the
	// workflows that publish each; detected from the catalog's artifacts/ definitions
	// or pinned under facts.artifacts. Root-level: a member scope carries
	// the root's.
	Artifacts  []Artifact `json:"artifacts,omitempty"`
	TaskRunner TaskRunner `json:"taskrunner"`
	Hooks      []string   `json:"hooks"`
	HasGit     bool       `json:"has_git"`
	// Owner is who owns the repository by its own files, and OwnerFrom the
	// file: catalog-info.yaml (Backstage spec.owner) or a CODEOWNERS file
	// (the default owner). Empty when neither says.
	Owner     string `json:"owner,omitempty"`
	OwnerFrom string `json:"owner_from,omitempty"`

	// Sources records provenance per top-level fact name.
	Sources map[string]Source `json:"sources"`
}

// Artifact is one published artifact: its kind (`image`, `npm`, ...), the
// name that tells it from the other artifacts of the kind (an image
// reference, a chart path; empty when the kind's definition does not name
// its instances) and the CI files that publish it. A pinned artifact names
// what the pin says.
type Artifact struct {
	Kind      string   `json:"kind"`
	Name      string   `json:"name,omitempty"`
	Workflows []string `json:"workflows,omitempty"`
}

// String is the artifact as the report names it: the kind, with the name
// when there is one.
func (a Artifact) String() string {
	if a.Name == "" {
		return a.Kind
	}
	return a.Kind + " " + a.Name
}

// ArtifactKinds lists the published artifact kinds in order, each once.
func (f Facts) ArtifactKinds() []string {
	out := make([]string, 0, len(f.Artifacts))
	for _, a := range f.Artifacts {
		if !slices.Contains(out, a.Kind) {
			out = append(out, a.Kind)
		}
	}
	return out
}

// ArtifactInstances are the published artifacts of the kind, in order.
func (f Facts) ArtifactInstances(kind string) []Artifact {
	var out []Artifact
	for _, a := range f.Artifacts {
		if a.Kind == kind {
			out = append(out, a)
		}
	}
	return out
}

// ArtifactWorkflows are the CI files that publish any artifact of the
// kind, sorted; nil when the kind is not published or the pin named none.
func (f Facts) ArtifactWorkflows(kind string) []string {
	var out []string
	for _, a := range f.Artifacts {
		if a.Kind == kind {
			for _, w := range a.Workflows {
				if !slices.Contains(out, w) {
					out = append(out, w)
				}
			}
		}
	}
	slices.Sort(out)
	return out
}

// Release describes the release mechanism in use.
type Release struct {
	Exists      bool     `json:"exists"`
	Mechanisms  []string `json:"mechanisms,omitempty"`
	TagPatterns []string `json:"tag_patterns,omitempty"`
	LatestTag   string   `json:"latest_tag,omitempty"`
}

// TaskRunner describes how the repo exposes its commands.
type TaskRunner struct {
	Kind    string   `json:"kind"`
	Targets []string `json:"targets,omitempty"`
	// File is the runner's definition file, Cmd how a target is invoked.
	File string `json:"file,omitempty"`
	Cmd  string `json:"cmd,omitempty"`
	// Deps and Recipes describe each target; rules read them, reports do not.
	Deps    map[string][]string `json:"-"`
	Recipes map[string][]string `json:"-"`
}

// Container values: where the task runner's targets run.
const (
	ContainerNone         = "none"
	ContainerDevcontainer = "devcontainer"
	ContainerDocker       = "docker"
	ContainerPodman       = "podman"
)

// Overrides are values a config file pins instead of detecting.
type Overrides struct {
	Stacks     []string
	Visibility Visibility
	Tier       int
	Forge      string
	// Container pins where targets run; "" keeps detection.
	Container string
	// TaskRunner names the runner to use when several could be; "" detects.
	TaskRunner string
	// DetectedVisibility is what a forge said; it applies only when neither
	// git config nor the configuration file settles the visibility.
	DetectedVisibility Visibility
	// FleetTier is the tier a fleet file sets; it applies only when the
	// configuration file does not pin one.
	FleetTier int
	// StackDefs are the catalog's stack definitions; nil means the legacy set.
	StackDefs []stacks.Def
	// Artifacts pins the published artifacts; nil detects, an empty list
	// says nothing is published. The engine applies it, since detection
	// needs the rule environment.
	Artifacts *[]Artifact
}

// Discover computes facts for a repository, applying overrides last.
func Discover(r *repo.Repo, ov Overrides) Facts {
	f := Facts{Name: r.Name(), Sources: map[string]Source{}, HasGit: r.HasGit()}
	defs := ov.StackDefs
	if defs == nil {
		defs = stacks.Legacy()
	}
	f.Stacks = stacks.Detect(r, defs)
	f.Sources["stacks"] = SourceDetected
	f.Forge, f.ForgeHost = ForgeOf(r.RemoteURL())
	f.Sources["forge"] = SourceDetected
	f.Visibility = detectVisibility(r)
	f.Sources["visibility"] = SourceDetected
	if f.Visibility == VisibilityUnknown && ov.DetectedVisibility != "" {
		f.Visibility = ov.DetectedVisibility
		f.Sources["visibility"] = SourceForge
	}
	f.Container = ContainerNone
	if r.Has(".devcontainer/devcontainer.json") || r.Has(".devcontainer.json") || len(r.Glob(".devcontainer/*/devcontainer.json")) > 0 {
		f.Container = ContainerDevcontainer
	}
	f.Sources["container"] = SourceDetected
	f.Layout, f.Members = detectLayout(r, stacks.Manifests(defs))
	f.Sources["layout"] = SourceDetected
	f.CI = detectCI(r)
	f.Release = detectRelease(r)
	f.TaskRunner = detectTaskRunner(r, ov.TaskRunner)
	f.Sources["taskrunner"] = SourceDetected
	if ov.TaskRunner != "" {
		f.Sources["taskrunner"] = SourceConfigured
	}
	f.Hooks = detectHooks(r)
	f.Owner, f.OwnerFrom = detectOwner(r)

	applyOverrides(&f, ov)

	if f.Tier == 0 && ov.FleetTier != 0 {
		f.Tier = ov.FleetTier
		f.Sources["tier"] = SourceFleet
	}
	if f.Tier == 0 {
		f.Tier, f.TierReason = defaultTier(f)
		f.Sources["tier"] = SourceDefault
		if f.TierReason != "" {
			f.Sources["tier"] = SourceInferred
		}
	}
	return f
}

func applyOverrides(f *Facts, ov Overrides) {
	if len(ov.Stacks) > 0 {
		f.Stacks = ov.Stacks
		f.Sources["stacks"] = SourceConfigured
	}
	if ov.Visibility != "" {
		f.Visibility = ov.Visibility
		f.Sources["visibility"] = SourceConfigured
	}
	if ov.Container != "" {
		f.Container = ov.Container
		f.Sources["container"] = SourceConfigured
	}
	if ov.Forge != "" {
		f.Forge = ov.Forge
		f.Sources["forge"] = SourceConfigured
	}
	if ov.Tier != 0 {
		f.Tier = ov.Tier
		f.Sources["tier"] = SourceConfigured
	}
}

// TierSource says where the tier came from, with the deciding fact when it
// was inferred: "configured", "fleet file", "default" or "inferred from CI".
func (f Facts) TierSource() string {
	if f.Sources["tier"] == SourceInferred && f.TierReason != "" {
		return "inferred from " + f.TierReason
	}
	if f.Sources["tier"] == SourceFleet {
		return "fleet file"
	}
	return string(f.Sources["tier"])
}

// Public is a convenience for rules.
func (f Facts) Public() bool { return f.Visibility == VisibilityPublic }

// ForgeOf is the forge a git remote is on and its host.
func ForgeOf(remote string) (forge, host string) {
	if remote == "" {
		return "unknown", ""
	}
	host = remoteHost(remote)
	switch {
	case host == "github.com":
		return "github", host
	case strings.Contains(host, "gitlab"):
		return "gitlab", host
	case host != "":
		// Self-hosted without a reachable API check: assume Gitea/Forgejo,
		// the common self-hosted choice; a config override settles it.
		return "gitea", host
	}
	return "unknown", ""
}

func remoteHost(remote string) string {
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return ""
		}
		return u.Hostname()
	}
	// scp-like: git@host:owner/repo.git
	if i := strings.Index(remote, "@"); i >= 0 {
		rest := remote[i+1:]
		if j := strings.Index(rest, ":"); j >= 0 {
			return rest[:j]
		}
	}
	return ""
}

func detectVisibility(r *repo.Repo) Visibility {
	switch strings.ToLower(r.GitConfig("agent.public")) {
	case "true":
		return VisibilityPublic
	case "false":
		return VisibilityPrivate
	}
	return VisibilityUnknown
}

func detectLayout(r *repo.Repo, manifests []string) (string, []string) {
	if members := workspaceMembers(r, manifests); len(members) > 0 {
		return "workspace", members
	}
	if members := nestedProjects(r, manifests); len(members) > 0 {
		return "nested", members
	}
	return "single", nil
}

func detectCI(r *repo.Repo) []string {
	var out []string
	if r.IsDir(".github/workflows") {
		out = append(out, "github-actions")
	}
	if r.IsDir(".gitea/workflows") {
		out = append(out, "gitea-actions")
	}
	if r.IsDir(".forgejo/workflows") {
		out = append(out, "forgejo-actions")
	}
	if r.Has(".gitlab-ci.yml") || r.Has(".gitlab-ci.yaml") {
		out = append(out, "gitlab-ci")
	}
	if r.Has(".woodpecker.yml") || r.IsDir(".woodpecker") {
		out = append(out, "woodpecker")
	}
	return out
}

func detectRelease(r *repo.Repo) Release {
	var rel Release
	if r.Has(".goreleaser.yaml") || r.Has(".goreleaser.yml") {
		rel.Mechanisms = append(rel.Mechanisms, "goreleaser")
	}
	if r.Has("cliff.toml") {
		rel.Mechanisms = append(rel.Mechanisms, "git-cliff")
	}
	for _, wf := range ci.Files(r) {
		if pats := ci.TagPatterns(r, wf); len(pats) > 0 {
			rel.Mechanisms = append(rel.Mechanisms, "workflow:"+wf)
			rel.TagPatterns = append(rel.TagPatterns, pats...)
		}
	}
	if tags := r.Tags(); len(tags) > 0 {
		rel.LatestTag = tags[0]
	}
	rel.Exists = len(rel.Mechanisms) > 0
	return rel
}

func detectTaskRunner(r *repo.Repo, want string) TaskRunner {
	rn := r.Runner(want)
	return TaskRunner{Kind: rn.Kind, Targets: rn.Targets, File: rn.File, Cmd: rn.Cmd, Deps: rn.Deps, Recipes: rn.Recipes}
}

func detectHooks(r *repo.Repo) []string {
	var out []string
	if r.Has(".pre-commit-config.yaml") {
		out = append(out, "pre-commit-config")
	}
	if slices.ContainsFunc([]string{"lefthook", ".lefthook"}, func(base string) bool {
		return slices.ContainsFunc([]string{".yml", ".yaml", ".toml", ".json", ".jsonc"}, func(ext string) bool { return r.Has(base + ext) })
	}) {
		out = append(out, "lefthook")
	}
	if r.IsDir(".husky") {
		out = append(out, "husky")
	}
	return out
}

// defaultTier guesses how seriously a repo should be treated when the config
// does not say. Public repos and anything with a release pipeline are tier 1;
// anything with CI or tags is tier 2; the rest are experiments. The reason
// names the deciding fact; it is empty for the plain default.
func defaultTier(f Facts) (int, string) {
	switch {
	case f.Visibility == VisibilityPublic:
		return 1, "public"
	case f.Release.Exists:
		return 1, "release pipeline"
	case len(f.CI) > 0:
		return 2, "CI"
	case f.Release.LatestTag != "":
		return 2, "tags"
	}
	return 3, ""
}
