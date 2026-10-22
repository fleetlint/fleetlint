package facts

import (
	"strings"

	"github.com/fleetlint/fleetlint/internal/repo"
	"github.com/fleetlint/fleetlint/internal/yamlsafe"
)

// codeownersFiles are where GitHub, Gitea and GitLab look for CODEOWNERS,
// in the order GitHub reads them.
var codeownersFiles = []string{".github/CODEOWNERS", "CODEOWNERS", "docs/CODEOWNERS", ".gitea/CODEOWNERS", ".gitlab/CODEOWNERS"}

// detectOwner names who owns the repository from its own files: the
// Backstage descriptor's spec.owner, else the CODEOWNERS default owner
// (the first owner of the last `*` line). from is the file it came from.
func detectOwner(r *repo.Repo) (owner, from string) {
	if text, ok := r.Text("catalog-info.yaml"); ok {
		if o := backstageOwner(text); o != "" {
			return o, "catalog-info.yaml"
		}
	}
	for _, f := range codeownersFiles {
		if text, ok := r.Text(f); ok {
			if o := defaultCodeowner(text); o != "" {
				return o, f
			}
		}
	}
	return "", ""
}

// backstageOwner reads spec.owner of the first entity that has one, as a
// plain name: `group:default/payments` and `payments` are both payments,
// another namespace stays (`group:ops/payments` is ops/payments).
func backstageOwner(text string) string {
	for _, doc := range strings.Split("\n"+text, "\n---") {
		var entity struct {
			Spec struct {
				Owner string `yaml:"owner"`
			} `yaml:"spec"`
		}
		if yamlsafe.Unmarshal([]byte(doc), &entity) != nil || entity.Spec.Owner == "" {
			continue
		}
		ref := entity.Spec.Owner
		if kind, rest, ok := strings.Cut(ref, ":"); ok && (kind == "group" || kind == "user") {
			ref = rest
		}
		return strings.TrimPrefix(ref, "default/")
	}
	return ""
}

// defaultCodeowner is the first owner of the last rule for every file
// (`*`, `/*` or `**`), without its `@`. GitLab section headers are skipped:
// their default owners name a section, not the repository.
func defaultCodeowner(text string) string {
	owner := ""
	for _, line := range strings.Split(text, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.HasPrefix(fields[0], "[") || strings.HasPrefix(fields[0], "^[") {
			continue
		}
		if fields[0] == "*" || fields[0] == "/*" || fields[0] == "**" || fields[0] == "/**" {
			owner = strings.TrimPrefix(fields[1], "@")
		}
	}
	return owner
}
