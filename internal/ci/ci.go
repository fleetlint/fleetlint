// Package ci reads CI configuration into one shape whatever the system:
// GitHub, Gitea and Forgejo Actions workflows and GitLab CI pipelines. Rules
// see jobs, steps, triggers and tag patterns without knowing which wrote them.
package ci

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/fleetlint/fleetlint/internal/repo"
)

// Kinds of CI file.
const (
	GitHub  = "github"
	Gitea   = "gitea"
	Forgejo = "forgejo"
	GitLab  = "gitlab"
)

// Files returns the CI files of a scope: Actions workflows of every forge,
// the GitLab pipeline and the local files it includes, sorted.
func Files(r *repo.Repo) []string {
	var out []string
	for _, dir := range []string{".github/workflows", ".gitea/workflows", ".forgejo/workflows"} {
		out = append(out, r.Glob(dir+"/*.y*ml")...)
	}
	for _, f := range []string{".gitlab-ci.yml", ".gitlab-ci.yaml"} {
		if !r.Has(f) {
			continue
		}
		out = append(out, f)
		for _, inc := range Includes(r, f) {
			if inc["type"] == "local" {
				out = append(out, r.Glob(strings.TrimPrefix(str(inc["target"]), "/"))...)
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Kind names the CI system a file belongs to; a file outside the Actions
// directories is GitLab's (the pipeline or a file it includes).
func Kind(path string) string {
	switch {
	case strings.HasPrefix(path, ".github/"):
		return GitHub
	case strings.HasPrefix(path, ".gitea/"):
		return Gitea
	case strings.HasPrefix(path, ".forgejo/"):
		return Forgejo
	}
	return GitLab
}

func doc(r *repo.Repo, path string) map[string]any {
	d, ok, err := r.Doc(repo.FormatYAML, path)
	if !ok || err != nil {
		return nil
	}
	m, _ := d.(map[string]any)
	return m
}

// gitlabReserved are the top-level keys of a GitLab pipeline that are not jobs.
var gitlabReserved = map[string]bool{
	"default": true, "include": true, "stages": true, "variables": true, "workflow": true, "image": true,
	"services": true, "before_script": true, "after_script": true, "cache": true, "spec": true,
}

// gitlabJobs returns the visible jobs of a GitLab file with `extends:`
// resolved from the jobs and templates of the same file.
func gitlabJobs(m map[string]any) map[string]map[string]any {
	jobs := map[string]map[string]any{}
	for name, v := range m {
		job, ok := v.(map[string]any)
		if !ok || gitlabReserved[name] || strings.HasPrefix(name, ".") {
			continue
		}
		jobs[name] = resolveExtends(m, job, 0)
	}
	return jobs
}

func resolveExtends(m, job map[string]any, depth int) map[string]any {
	parents := strList(job["extends"])
	if len(parents) == 0 || depth > 10 {
		return job
	}
	out := map[string]any{}
	for _, p := range parents {
		if base, ok := m[p].(map[string]any); ok {
			for k, v := range resolveExtends(m, base, depth+1) {
				out[k] = v
			}
		}
	}
	for k, v := range job {
		out[k] = v
	}
	return out
}

// Jobs lists a file's jobs as `{name, timeout_minutes, permissions, runs_on,
// uses, image, interruptible}` with zero values for absent fields.
func Jobs(r *repo.Repo, path string) []any {
	m := doc(r, path)
	if Kind(path) == GitLab {
		jobs := gitlabJobs(m)
		out := make([]any, 0, len(jobs))
		for _, name := range sortedKeys(jobs) {
			job := jobs[name]
			out = append(out, map[string]any{
				"name": name, "timeout_minutes": gitlabMinutes(str(job["timeout"])), "permissions": map[string]any{},
				"runs_on": strings.Join(strList(job["tags"]), ","), "uses": "", "image": image(job["image"]),
				"interruptible": job["interruptible"] == true,
			})
		}
		return out
	}
	jobs, _ := m["jobs"].(map[string]any)
	out := make([]any, 0, len(jobs))
	for _, name := range sortedKeys(jobs) {
		job, _ := jobs[name].(map[string]any)
		perms := job["permissions"]
		if perms == nil {
			perms = map[string]any{}
		}
		out = append(out, map[string]any{
			"name": name, "timeout_minutes": intOf(job["timeout-minutes"]), "permissions": perms,
			"runs_on": str(job["runs-on"]), "uses": str(job["uses"]), "image": image(job["container"]),
			"interruptible": false,
		})
	}
	return out
}

// Steps flattens a file's jobs into step maps `{job, name, uses, run, with}`.
// A GitLab job is one step whose run is its before_script, script and
// after_script.
func Steps(r *repo.Repo, path string) []any {
	m := doc(r, path)
	if Kind(path) == GitLab {
		return gitlabSteps(m)
	}
	out := []any{}
	jobs, _ := m["jobs"].(map[string]any)
	for _, jobName := range sortedKeys(jobs) {
		job, _ := jobs[jobName].(map[string]any)
		list, _ := job["steps"].([]any)
		for _, s := range list {
			step, _ := s.(map[string]any)
			with, _ := step["with"].(map[string]any)
			if with == nil {
				with = map[string]any{}
			}
			out = append(out, map[string]any{"job": jobName, "name": str(step["name"]), "uses": str(step["uses"]), "run": str(step["run"]), "with": with})
		}
	}
	return out
}

func gitlabSteps(m map[string]any) []any {
	def, _ := m["default"].(map[string]any)
	jobs := gitlabJobs(m)
	out := make([]any, 0, len(jobs))
	for _, name := range sortedKeys(jobs) {
		job := jobs[name]
		lines := make([]string, 0, 8)
		for _, key := range []string{"before_script", "script", "after_script"} {
			// The job's own value, else the default section's, else the top level's.
			v, ok := job[key]
			if !ok {
				v, ok = def[key]
			}
			if !ok {
				v = m[key]
			}
			lines = append(lines, strList(v)...)
		}
		out = append(out, map[string]any{"job": name, "name": name, "uses": "", "run": strings.Join(lines, "\n"), "with": map[string]any{}})
	}
	return out
}

var (
	mrRe       = regexp.MustCompile(`merge_request_event|\bmerge_requests\b`)
	scheduleRe = regexp.MustCompile(`["']?schedule["']?|\bschedules\b`)
	tagRuleRe  = regexp.MustCompile(`\$CI_COMMIT_TAG(?:\s*=~\s*/([^/]+)/)?`)
	onlyTagsRe = regexp.MustCompile(`(?m)^\s*(?:only:\s*\[?[^\]\n]*\btags\b|-\s*tags\s*$)`)
)

// Triggers lists the events a file reacts to in GitHub's vocabulary. A
// GitLab pipeline runs on push; merge request pipelines read as
// pull_request, scheduled ones as schedule.
func Triggers(r *repo.Repo, path string) []string {
	if Kind(path) == GitLab {
		text, ok := r.Text(path)
		if !ok {
			return nil
		}
		out := []string{"push"}
		if mrRe.MatchString(text) {
			out = append(out, "pull_request")
		}
		if scheduleRe.MatchString(text) {
			out = append(out, "schedule")
		}
		return out
	}
	switch on := doc(r, path)["on"].(type) {
	case string:
		return []string{on}
	case []any:
		return strList(on)
	case map[string]any:
		return sortedKeys(on)
	}
	return nil
}

// TagPatterns returns the tag patterns that start the file's pipeline: the
// `on.push.tags` of a workflow; for GitLab the regex a rule matches
// `$CI_COMMIT_TAG` against, or `*` when any tag runs it.
func TagPatterns(r *repo.Repo, path string) []string {
	if Kind(path) == GitLab {
		text, _ := r.Text(path)
		var out []string
		for _, m := range tagRuleRe.FindAllStringSubmatch(text, -1) {
			if m[1] != "" {
				out = append(out, m[1])
			} else {
				out = append(out, "*")
			}
		}
		if len(out) == 0 && onlyTagsRe.MatchString(text) {
			out = append(out, "*")
		}
		slices.Sort(out)
		return slices.Compact(out)
	}
	on, _ := doc(r, path)["on"].(map[string]any)
	push, _ := on["push"].(map[string]any)
	tags, _ := push["tags"].([]any)
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, strings.TrimSpace(strings.Trim(fmt.Sprint(t), `"'`)))
	}
	return out
}

// Includes lists what a GitLab file includes as `{type, target, ref}`:
// type is local, project, remote, template or component; ref is the
// project ref or the component version. Other files have none.
func Includes(r *repo.Repo, path string) []map[string]any {
	if Kind(path) != GitLab {
		return nil
	}
	var entries []any
	switch inc := doc(r, path)["include"].(type) {
	case string, map[string]any:
		entries = []any{inc}
	case []any:
		entries = inc
	}
	out := []map[string]any{}
	for _, e := range entries {
		out = append(out, includeEntries(e)...)
	}
	return out
}

// includeEntries reads one `include:` item: a plain path or URL, or a map
// keyed by the include type.
func includeEntries(e any) []map[string]any {
	switch v := e.(type) {
	case string:
		kind := "local"
		if strings.HasPrefix(v, "https://") || strings.HasPrefix(v, "http://") {
			kind = "remote"
		}
		return []map[string]any{{"type": kind, "target": v, "ref": ""}}
	case map[string]any:
		var out []map[string]any
		for _, kind := range []string{"local", "project", "remote", "template", "component"} {
			target, ok := v[kind]
			if !ok {
				continue
			}
			t, ref := str(target), str(v["ref"])
			if i := strings.LastIndex(t, "@"); kind == "component" && i >= 0 {
				t, ref = t[:i], t[i+1:]
			}
			out = append(out, map[string]any{"type": kind, "target": t, "ref": ref})
		}
		return out
	}
	return nil
}

var durationRe = regexp.MustCompile(`(?i)(\d+)\s*(h|hours?|hrs?|m|min|mins|minutes?|s|sec|secs|seconds?)\b`)

// gitlabMinutes reads a GitLab duration ("1h 30m", "90 minutes", "3600s").
func gitlabMinutes(s string) int64 {
	var seconds int64
	for _, m := range durationRe.FindAllStringSubmatch(s, -1) {
		n, _ := strconv.ParseInt(m[1], 10, 64)
		switch strings.ToLower(m[2])[0] {
		case 'h':
			seconds += n * 3600
		case 'm':
			seconds += n * 60
		default:
			seconds += n
		}
	}
	if seconds > 0 && seconds < 60 {
		return 1
	}
	return seconds / 60
}

func image(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		if n := str(t["name"]); n != "" {
			return n
		}
		return str(t["image"])
	}
	return ""
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func strList(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			switch y := x.(type) {
			case []any: // GitLab lets a script line be a nested list (YAML anchors)
				out = append(out, strList(y)...)
			default:
				out = append(out, fmt.Sprint(y))
			}
		}
		return out
	}
	return nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func intOf(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case uint64:
		return int64(t) //nolint:gosec // workflow timeouts never approach the int64 range
	case string:
		i, _ := strconv.ParseInt(t, 10, 64)
		return i
	}
	return 0
}
