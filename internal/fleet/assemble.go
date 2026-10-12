package fleet

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/forge"
)

// Sources are where the repositories of a run come from: a fleet file,
// paths, directories to discover in and forge owners to list.
type Sources struct {
	SpecPath string
	Paths    []string
	// Discover are directories walked for git repositories; Exclude are
	// globs, relative to them, to skip.
	Discover, Exclude []string
	// From are forge owners: github:<owner> or gitea:<host>/<owner>.
	From []string
}

// Assemble builds the fleet from its sources. A fleet file entry for a
// discovered path adjusts the discovered repository instead of adding it
// twice.
func Assemble(ctx context.Context, s Sources) (*Spec, error) {
	spec, err := loadSpec(s.SpecPath, s.Paths)
	if err != nil {
		return nil, err
	}
	if err := addWalked(spec, s.Discover, s.Exclude); err != nil {
		return nil, err
	}
	return spec, addDiscovered(ctx, spec, s.From)
}

func loadSpec(specPath string, paths []string) (*Spec, error) {
	spec := &Spec{Version: 1}
	if specPath != "" {
		loaded, err := LoadSpec(specPath)
		if err != nil {
			return nil, err
		}
		spec = loaded
	}
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		spec.Repos = append(spec.Repos, Entry{Path: abs, Name: filepath.Base(abs)})
	}
	return spec, nil
}

// addWalked appends the repositories found under each --discover root.
func addWalked(spec *Spec, roots, exclude []string) error {
	if len(exclude) > 0 && len(roots) == 0 {
		return errors.New("--exclude needs --discover")
	}
	names := map[string]bool{}
	listed := map[string]int{} // path -> index of its fleet file entry
	for i, e := range spec.Repos {
		names[e.Name] = true
		if e.Path != "" {
			listed[absPath(e.Path)] = i
		}
	}
	for _, root := range roots {
		found, err := Discover(root, DiscoverDepth, exclude)
		if err != nil {
			return fmt.Errorf("--discover %s: %w", root, err)
		}
		for _, e := range found.Repos {
			if err := addFound(spec, names, listed, e); err != nil {
				return fmt.Errorf("--discover %s: %w", root, err)
			}
		}
		for _, u := range found.Unversioned {
			spec.Unversioned = append(spec.Unversioned, filepath.Join(root, u))
		}
	}
	return nil
}

// addFound appends a discovered repository; a fleet file entry for the
// same path adjusts it instead and only takes its group.
func addFound(spec *Spec, names map[string]bool, listed map[string]int, e Entry) error {
	if i, ok := listed[absPath(e.Path)]; ok {
		if spec.Repos[i].Group == "" {
			spec.Repos[i].Group = e.Group
		}
		return nil
	}
	if names[e.Name] {
		return fmt.Errorf("repository name %q is already in the fleet", e.Name)
	}
	names[e.Name] = true
	spec.Repos = append(spec.Repos, e)
	return nil
}

func absPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}

// addDiscovered appends the repositories of each --from source and makes
// sure the run has something to check and somewhere to clone into.
func addDiscovered(ctx context.Context, spec *Spec, from []string) error {
	names := map[string]bool{}
	for _, e := range spec.Repos {
		names[e.Name] = true
	}
	for _, s := range from {
		entries, err := discover(ctx, s)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if names[e.Name] {
				return fmt.Errorf("%s: repository name %q is already in the fleet", s, e.Name)
			}
			names[e.Name] = true
			spec.Repos = append(spec.Repos, e)
		}
	}
	if len(spec.Repos) == 0 {
		return errors.New("no repositories: pass paths, --repos fleet.yaml, --discover <dir> or --from github:<owner>")
	}
	if len(from) > 0 && spec.Cache == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			return fmt.Errorf("no cache directory for clones (set --cache): %w", err)
		}
		spec.Cache = filepath.Join(dir, "fleetlint", "repos")
	}
	return nil
}

// sameHost reports whether a clone URL the forge returned points at the
// forge itself (for GitHub Enterprise, the host of GITHUB_API_URL), so its
// token never goes to a host the forge merely named.
func sameHost(cloneURL string, src forge.Source) bool {
	u, err := url.Parse(cloneURL)
	if err != nil || u.Scheme != "https" {
		return false
	}
	if strings.EqualFold(u.Hostname(), src.Host) {
		return true
	}
	api, err := url.Parse(os.Getenv("GITHUB_API_URL"))
	return src.Kind == "github" && err == nil && api.Hostname() != "" && strings.EqualFold(u.Hostname(), api.Hostname())
}

// discover lists one forge source as fleet entries, without archived
// repositories and forks.
func discover(ctx context.Context, source string) ([]Entry, error) {
	src, err := forge.ParseSource(source)
	if err != nil {
		return nil, err
	}
	client := forge.Client{Hosts: []string{src.Host}}
	repos, err := client.List(ctx, src)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(repos))
	for _, fr := range repos {
		if fr.Archived || fr.Fork {
			continue
		}
		e := Entry{Name: fr.Name, URL: fr.CloneURL, Visibility: string(facts.VisibilityPublic)}
		if sameHost(fr.CloneURL, src) {
			e.Token = client.Token(src)
		}
		if fr.Private {
			e.Visibility = string(facts.VisibilityPrivate)
		}
		if err := e.Normalize(); err != nil {
			return nil, fmt.Errorf("%s: repository %s: %w", source, fr.Name, err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}
