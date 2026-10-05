package facts

import (
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/fleetlint/fleetlint/internal/repo"
)

// workspaceMembers finds the member directories of a monorepo from whatever
// workspace manifest the root carries. Globs are expanded against the tree;
// only directories that contain a stack manifest count, so a `packages/*`
// glob does not turn a README folder into a scope.
func workspaceMembers(r *repo.Repo) []string {
	set := map[string]bool{}
	add := func(dirs []string) {
		for _, d := range dirs {
			d = strings.Trim(strings.TrimPrefix(d, "./"), "/")
			if d != "" && d != "." && hasManifest(r, d) {
				set[d] = true
			}
		}
	}
	add(goWorkMembers(r))
	add(expandGlobs(r, yamlList(r, "pnpm-workspace.yaml", "packages")))
	add(expandGlobs(r, jsonWorkspaces(r)))
	add(expandGlobs(r, yamlList(r, "melos.yaml", "packages")))
	add(expandGlobs(r, cargoMembers(r)))
	add(gradleIncludes(r))
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func hasManifest(r *repo.Repo, dir string) bool {
	for _, markers := range stackMarkers {
		for _, m := range markers {
			if r.Has(dir + "/" + m) {
				return true
			}
		}
	}
	return false
}

func goWorkMembers(r *repo.Repo) []string {
	text, ok := r.Text("go.work")
	if !ok {
		return nil
	}
	var out []string
	inBlock := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "use ("):
			inBlock = true
		case inBlock && line == ")":
			inBlock = false
		case inBlock:
			if i := strings.Index(line, "//"); i >= 0 {
				line = strings.TrimSpace(line[:i])
			}
			if fields := strings.Fields(line); len(fields) > 0 {
				out = append(out, fields[0])
			}
		case strings.HasPrefix(line, "use "):
			out = append(out, strings.TrimSpace(strings.TrimPrefix(line, "use ")))
		}
	}
	return out
}

func yamlList(r *repo.Repo, file, key string) []string {
	doc, ok, err := r.Doc(repo.FormatYAML, file)
	if !ok || err != nil {
		return nil
	}
	m, _ := doc.(map[string]any)
	return anyStrings(m[key])
}

func jsonWorkspaces(r *repo.Repo) []string {
	doc, ok, err := r.Doc(repo.FormatJSON, "package.json")
	if !ok || err != nil {
		return nil
	}
	m, _ := doc.(map[string]any)
	switch ws := m["workspaces"].(type) {
	case []any:
		return anyStrings(ws)
	case map[string]any:
		return anyStrings(ws["packages"])
	}
	if doc, ok, err := r.Doc(repo.FormatJSON, "lerna.json"); ok && err == nil {
		m, _ := doc.(map[string]any)
		return anyStrings(m["packages"])
	}
	return nil
}

func cargoMembers(r *repo.Repo) []string {
	doc, ok, err := r.Doc(repo.FormatTOML, "Cargo.toml")
	if !ok || err != nil {
		return nil
	}
	m, _ := doc.(map[string]any)
	ws, _ := m["workspace"].(map[string]any)
	return anyStrings(ws["members"])
}

var gradleIncludeRe = regexp.MustCompile(`include\s*\(?\s*((?:"[^"]+"|'[^']+')(?:\s*,\s*(?:"[^"]+"|'[^']+'))*)`)

func gradleIncludes(r *repo.Repo) []string {
	text, ok := r.Text("settings.gradle.kts")
	if !ok {
		if text, ok = r.Text("settings.gradle"); !ok {
			return nil
		}
	}
	var out []string
	for _, m := range gradleIncludeRe.FindAllStringSubmatch(text, -1) {
		for _, q := range strings.Split(m[1], ",") {
			name := strings.Trim(strings.TrimSpace(q), `"'`)
			out = append(out, strings.ReplaceAll(strings.TrimPrefix(name, ":"), ":", "/"))
		}
	}
	return out
}

// expandGlobs turns patterns like packages/* into existing directories and
// removes any matched by a negated pattern (!packages/skip).
func expandGlobs(r *repo.Repo, patterns []string) []string {
	var out, negated []string
	for _, p := range patterns {
		if neg, ok := strings.CutPrefix(p, "!"); ok {
			negated = append(negated, strings.TrimSuffix(neg, "/"))
			continue
		}
		out = append(out, expandOne(r, p)...)
	}
	return slices.DeleteFunc(out, func(d string) bool {
		return slices.ContainsFunc(negated, func(n string) bool { return d == n || repo.MatchGlob(n, d) })
	})
}

// expandOne lists the directories a single positive pattern names.
func expandOne(r *repo.Repo, p string) []string {
	if !strings.ContainsAny(p, "*?[") {
		return []string{p}
	}
	var dirs []string
	seen := map[string]bool{}
	for _, f := range r.Glob(strings.TrimSuffix(p, "/") + "/*") {
		dir := f[:strings.LastIndex(f, "/")]
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

func anyStrings(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// notProjects are top-level directories whose manifests belong to examples,
// fixtures or dependencies rather than to a part of the product.
var notProjects = map[string]bool{
	"vendor": true, "third_party": true, "node_modules": true, "testdata": true, "test": true, "tests": true,
	"examples": true, "example": true, "samples": true, "sample": true, "docs": true, "doc": true,
	"build": true, "dist": true, "target": true, "out": true,
}

// nestedProjects finds projects that sit in a top-level directory without a
// workspace file declaring them, such as a `frontend/` with its own
// package.json next to a Go module. Only tracked manifests one level down
// count, so dependencies and build output are never mistaken for projects.
func nestedProjects(r *repo.Repo) []string {
	if !r.HasGit() {
		return nil
	}
	set := map[string]bool{}
	for _, f := range r.TrackedInScope() {
		dir, name, ok := strings.Cut(f, "/")
		if !ok || strings.Contains(name, "/") || strings.HasPrefix(dir, ".") || notProjects[dir] {
			continue
		}
		for _, markers := range stackMarkers {
			for _, m := range markers {
				if name == m {
					set[dir] = true
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}
