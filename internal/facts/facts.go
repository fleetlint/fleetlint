// Package facts discovers what kind of repository we are looking at. Facts
// are computed once per run, carry their provenance (detected, configured or
// default) and are exposed to rules unchanged, so a surprising result can
// always be traced with `fleetlint facts`.
package facts

import (
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/fleetlint/fleetlint/internal/repo"
)

// Source says where a fact's value came from.
type Source string

// Fact provenance values.
const (
	SourceDetected   Source = "detected"
	SourceConfigured Source = "configured"
	SourceDefault    Source = "default"
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
	Layout    string `json:"layout"`
	// Members are workspace member directories (repo-relative), when Layout is "workspace".
	Members    []string   `json:"members,omitempty"`
	CI         []string   `json:"ci"`
	Release    Release    `json:"release"`
	TaskRunner TaskRunner `json:"taskrunner"`
	Hooks      []string   `json:"hooks"`
	HasGit     bool       `json:"has_git"`

	// Sources records provenance per top-level fact name.
	Sources map[string]Source `json:"sources"`
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
}

// Discover computes facts for a repository, applying overrides last.
func Discover(r *repo.Repo, ov Overrides) Facts {
	f := Facts{Name: r.Name(), Sources: map[string]Source{}, HasGit: r.HasGit()}
	f.Stacks = detectStacks(r)
	f.Sources["stacks"] = SourceDetected
	f.Forge, f.ForgeHost = detectForge(r.RemoteURL())
	f.Sources["forge"] = SourceDetected
	if f.Visibility = detectVisibility(r); f.Visibility == VisibilityUnknown && ov.DetectedVisibility != "" {
		f.Visibility = ov.DetectedVisibility
	}
	f.Sources["visibility"] = SourceDetected
	f.Container = ContainerNone
	if r.Has(".devcontainer/devcontainer.json") || r.Has(".devcontainer.json") || len(r.Glob(".devcontainer/*/devcontainer.json")) > 0 {
		f.Container = ContainerDevcontainer
	}
	f.Sources["container"] = SourceDetected
	f.Layout, f.Members = detectLayout(r)
	f.Sources["layout"] = SourceDetected
	f.CI = detectCI(r)
	f.Release = detectRelease(r)
	f.TaskRunner = detectTaskRunner(r, ov.TaskRunner)
	f.Sources["taskrunner"] = SourceDetected
	if ov.TaskRunner != "" {
		f.Sources["taskrunner"] = SourceConfigured
	}
	f.Hooks = detectHooks(r)

	applyOverrides(&f, ov)

	if f.Tier == 0 {
		f.Tier = defaultTier(f)
		f.Sources["tier"] = SourceDefault
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

// Public is a convenience for rules.
func (f Facts) Public() bool { return f.Visibility == VisibilityPublic }

var stackMarkers = map[string][]string{
	"go":      {"go.mod"},
	"python":  {"pyproject.toml", "setup.py", "requirements.txt"},
	"rust":    {"Cargo.toml"},
	"node":    {"package.json"},
	"kotlin":  {"build.gradle.kts", "settings.gradle.kts", "build.gradle"},
	"flutter": {"pubspec.yaml"},
	"hugo":    {"hugo.toml", "hugo.yaml", "hugo.json", "config/_default/hugo.toml", "config/_default/hugo.yaml", "config/_default/config.toml"},
	"java":    {"pom.xml"},
	"dotnet":  {"global.json", "Directory.Build.props"},
	"cpp":     {"CMakeLists.txt", "meson.build", "conanfile.txt", "conanfile.py", "vcpkg.json"},
	"ruby":    {"Gemfile"},
	"php":     {"composer.json"},
}

// stackGlobs are markers whose name varies: solution and project files, gemspecs.
var stackGlobs = map[string][]string{
	"dotnet": {"*.sln", "*.slnx", "*.csproj", "*.fsproj"},
	"ruby":   {"*.gemspec"},
}

func detectStacks(r *repo.Repo) []string {
	var out []string
	for stack, markers := range stackMarkers {
		if anyMarker(markers, r.Has) {
			out = append(out, stack)
		}
	}
	for stack, globs := range stackGlobs {
		if !slices.Contains(out, stack) && anyMarker(globs, func(g string) bool { return len(r.Glob(g)) > 0 }) {
			out = append(out, stack)
		}
	}
	if !slices.Contains(out, "hugo") && legacyHugo(r) {
		out = append(out, "hugo")
	}
	// A Flutter project's Android shell lives under android/, so a root
	// pubspec plus a root Groovy build file is still Flutter, not a Kotlin
	// project. A root build.gradle.kts means a real Gradle project.
	if slices.Contains(out, "flutter") && slices.Contains(out, "kotlin") && !r.Has("build.gradle.kts") {
		out = slices.DeleteFunc(out, func(s string) bool { return s == "kotlin" })
	}
	sort.Strings(out)
	return out
}

func anyMarker(markers []string, has func(string) bool) bool {
	for _, m := range markers {
		if has(m) {
			return true
		}
	}
	return false
}

// legacyHugo recognises an older Hugo site: a config.toml that names the
// baseURL next to a content directory.
func legacyHugo(r *repo.Repo) bool {
	if !r.Has("config.toml") || !r.Has("content") {
		return false
	}
	text, ok := r.Text("config.toml")
	return ok && strings.Contains(text, "baseURL")
}

func detectForge(remote string) (forge, host string) {
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

func detectLayout(r *repo.Repo) (string, []string) {
	if members := workspaceMembers(r); len(members) > 0 {
		return "workspace", members
	}
	if members := nestedProjects(r); len(members) > 0 {
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
	if r.Has(".gitlab-ci.yml") {
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
	for _, wf := range append(r.Glob(".github/workflows/*.y*ml"), r.Glob(".gitea/workflows/*.y*ml")...) {
		if pats := tagTriggers(r, wf); len(pats) > 0 {
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

// tagTriggers returns the push.tags patterns of a workflow, if any.
func tagTriggers(r *repo.Repo, wf string) []string {
	doc, ok, err := r.Doc(repo.FormatYAML, wf)
	if !ok || err != nil {
		return nil
	}
	m, _ := doc.(map[string]any)
	on, _ := m["on"].(map[string]any)
	push, _ := on["push"].(map[string]any)
	tags, _ := push["tags"].([]any)
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, strings.TrimSpace(strings.Trim(fmtAny(t), `"'`)))
	}
	return out
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
	if r.Has("lefthook.yml") || r.Has(".lefthook.yml") {
		out = append(out, "lefthook")
	}
	if r.IsDir(".husky") {
		out = append(out, "husky")
	}
	return out
}

// defaultTier guesses how seriously a repo should be treated when the config
// does not say. Public repos and anything with a release pipeline are tier 1;
// anything with CI or tags is tier 2; the rest are experiments.
func defaultTier(f Facts) int {
	switch {
	case f.Visibility == VisibilityPublic, f.Release.Exists:
		return 1
	case len(f.CI) > 0, f.Release.LatestTag != "":
		return 2
	}
	return 3
}

func fmtAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
