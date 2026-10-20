package fleet

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fleetlint/fleetlint/internal/repo"
)

// DiscoverDepth is how many directory levels below the root discovery looks.
const DiscoverDepth = 4

// projectMarkers are files that make a directory a project worth checking.
var projectMarkers = map[string]bool{
	"go.mod": true, "package.json": true, "pyproject.toml": true, "setup.py": true, "requirements.txt": true,
	"Cargo.toml": true, "pom.xml": true, "build.gradle": true, "build.gradle.kts": true, "composer.json": true,
	"Gemfile": true, "pubspec.yaml": true, "Makefile": true, "justfile": true, "Dockerfile": true,
	"docker-compose.yml": true, "compose.yaml": true, "index.html": true,
}

// sourceExts are file types that make a directory a project even without a manifest.
var sourceExts = map[string]bool{".go": true, ".py": true, ".sh": true, ".js": true, ".ts": true, ".rs": true, ".kt": true, ".java": true, ".html": true}

// Discovered is what a directory walk found.
type Discovered struct {
	Repos []Entry
	// Unversioned are project directories (relative to the root) that are
	// in no repository of their own.
	Unversioned []string
}

// Discover walks root for git repositories, up to depth levels down. A
// repository is not descended into. Each entry is named after its directory
// (with its parents when two would collide) and grouped by the first path
// component when it sits deeper than one level. The root is a repository
// of its own only when nothing below it is. Paths matching an exclude glob
// (relative to the root, e.g. `*/third_party/*`) are skipped.
func Discover(root string, depth int, exclude []string) (*Discovered, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	for _, g := range exclude {
		if !repo.ValidGlob(g) {
			return nil, fmt.Errorf("--exclude %q is not a valid glob", g)
		}
	}
	w := walker{root: root, depth: depth, exclude: exclude}
	if err := filepath.WalkDir(root, w.visit); err != nil {
		return nil, err
	}
	repos, unversioned := w.repos, w.unversioned
	// A project directory that holds repositories is a container, not a project.
	kept := unversioned[:0]
	for _, u := range unversioned {
		if !containsAny(u, repos) {
			kept = append(kept, u)
		}
	}
	if len(repos) == 0 && isRepo(root) {
		repos = []string{"."}
	}
	sort.Strings(repos)
	return &Discovered{Repos: entriesFor(root, repos), Unversioned: kept}, nil
}

// walker collects repositories and unversioned project directories.
type walker struct {
	root        string
	depth       int
	exclude     []string
	repos       []string
	unversioned []string
}

func (w *walker) visit(path string, d fs.DirEntry, err error) error {
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fs.SkipDir
		}
		return err
	}
	if !d.IsDir() || path == w.root {
		return nil
	}
	rel := filepath.ToSlash(mustRel(w.root, path))
	switch {
	case skipWalk(d.Name()) || excluded(rel, w.exclude) || strings.Count(rel, "/") >= w.depth:
		return fs.SkipDir
	case isRepo(path):
		w.repos = append(w.repos, rel)
		return fs.SkipDir
	case !insideAny(rel, w.unversioned) && isProject(path):
		w.unversioned = append(w.unversioned, rel)
	}
	return nil
}

func entriesFor(root string, rels []string) []Entry {
	count := map[string]int{}
	for _, rel := range rels {
		count[filepath.Base(rel)]++
	}
	out := make([]Entry, 0, len(rels))
	for _, rel := range rels {
		e := Entry{Path: filepath.Join(root, filepath.FromSlash(rel)), Name: filepath.Base(rel)}
		if rel == "." {
			e.Name = filepath.Base(root)
		}
		if count[e.Name] > 1 {
			e.Name = strings.ReplaceAll(rel, "/", "-")
		}
		if i := strings.LastIndex(rel, "/"); i > 0 {
			e.Group = rel[:i] // the directories above it, nested groups joined by `/`
		}
		out = append(out, e)
	}
	return out
}

func mustRel(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

func skipWalk(name string) bool {
	switch name {
	case "node_modules", "vendor", "venv", "__pycache__", "target":
		return true
	}
	return strings.HasPrefix(name, ".")
}

func excluded(rel string, globs []string) bool {
	for _, g := range globs {
		if repo.MatchGlob(g, rel) {
			return true
		}
	}
	return false
}

func isRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func isProject(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && (projectMarkers[e.Name()] || sourceExts[filepath.Ext(e.Name())]) {
			return true
		}
	}
	return false
}

func insideAny(rel string, dirs []string) bool {
	for _, d := range dirs {
		if strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	return false
}

func containsAny(dir string, rels []string) bool {
	for _, r := range rels {
		if strings.HasPrefix(r, dir+"/") {
			return true
		}
	}
	return false
}
