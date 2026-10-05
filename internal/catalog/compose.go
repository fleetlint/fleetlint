package catalog

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/fleetlint/fleetlint/internal/fix"
)

// The hook configuration and the check workflow are assembled from a shared
// head, one fragment per stack and (for the workflow) a shared tail, so a
// repository with a nested project of another stack gets both stacks in one
// file.
const (
	hooksTemplate = "pre-commit-config.yaml"
	checkTemplate = "check.yml"
)

const makefileTemplate = "Makefile"

// composed splits "<stack>/<file>" for the assembled templates.
func composed(name string) (stack, file string, ok bool) {
	stack, file, found := strings.Cut(name, "/")
	return stack, file, found && (file == hooksTemplate || file == checkTemplate || file == devcontainerTemplate)
}

// Compose returns a template assembled for the project: the root stack
// named in name, its nested projects and its dev container flag. ok is false
// for templates that are plain files.
func (EmbeddedTemplates) Compose(name string, p fix.Project) (body []byte, ok bool, err error) {
	if name == makefileTemplate {
		if !p.Devcontainer {
			return nil, false, nil
		}
		body, err = composeMakefile()
		return body, true, err
	}
	stack, file, ok := composed(name)
	if !ok {
		return nil, false, nil
	}
	switch {
	case file == hooksTemplate:
		body, err = composeHooks(stack, p.Nested)
	case file == checkTemplate:
		body, err = composeCheck(stack, p)
	case p.Devcontainer:
		body, err = composeDevcontainer(stack, p.Nested)
	default:
		// No flag, no container: the repository runs on the machine.
		return nil, true, nil
	}
	return body, true, err
}

var phonyRe = regexp.MustCompile(`(?m)^\.PHONY: (.*)$`)

// devcontainerSwitch is the head of a Makefile whose targets run inside the
// dev container. Outside the container every target is forwarded into it;
// inside (the container sets IN_DEVCONTAINER) and with DEVCONTAINER=0 the
// recipes below run directly.
const devcontainerSwitch = `# DEVCONTAINER=1 runs every target inside the dev container (needs Docker and the devcontainer CLI);
# DEVCONTAINER=0 runs it on this machine. Inside the container targets always run directly.
DEVCONTAINER ?= 1
TARGETS := %s

ifeq ($(DEVCONTAINER)$(IN_DEVCONTAINER),1)
.PHONY: $(TARGETS)
$(TARGETS):
	@devcontainer up --workspace-folder . >/dev/null
	devcontainer exec --workspace-folder . make $@ $(filter-out DEVCONTAINER=%%,$(MAKEOVERRIDES)) DEVCONTAINER=0
else
`

// composeMakefile wraps the Makefile template's targets in the dev
// container switch.
func composeMakefile() ([]byte, error) {
	plain, err := fragment(makefileTemplate)
	if err != nil {
		return nil, err
	}
	m := phonyRe.FindStringSubmatchIndex(plain)
	if m == nil {
		return nil, fmt.Errorf("the Makefile template has no .PHONY line to take the targets from")
	}
	head, rest := plain[:m[0]], plain[m[0]:]
	return []byte(head + fmt.Sprintf(devcontainerSwitch, plain[m[2]:m[3]]) + strings.TrimRight(rest, "\n") + "\nendif\n"), nil
}

func fragment(name string) (string, error) {
	b, err := templates.ReadFile("templates/" + name)
	if err != nil {
		return "", fmt.Errorf("no template %q in this binary", name)
	}
	return string(b), nil
}

func composeHooks(stack string, nested []fix.Nested) ([]byte, error) {
	head, err := fragment("pre-commit/head.yaml")
	if err != nil {
		return nil, err
	}
	own, err := fragment("pre-commit/" + stack + ".yaml")
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString(head)
	fmt.Fprintf(&b, "  # %s hooks\n%s", stack, own)
	for _, n := range nested {
		block, err := fragment("pre-commit/" + n.Stack + ".yaml")
		if err != nil {
			return nil, err
		}
		if n.Path == "" {
			fmt.Fprintf(&b, "\n  # %s hooks\n%s", n.Stack, block)
			continue
		}
		fmt.Fprintf(&b, "\n  # %s hooks for %s/: run inside that directory, on its files only\n%s", n.Stack, n.Path, scopeHooks(block, n.Path))
	}
	return []byte(b.String()), nil
}

var (
	hookIDRe    = regexp.MustCompile(`^(\s+- id: )(\S+)(.*)$`)
	hookNameRe  = regexp.MustCompile(`^(\s+name: )(.*)$`)
	hookEntryRe = regexp.MustCompile(`^(\s+entry: )(.*)$`)
	hookFilesRe = regexp.MustCompile(`^(\s+files: )(.*)$`)
)

// scopeHooks rewrites a stack's hook block for a project in a subdirectory:
// unique ids, the directory in the name, commands run inside the directory
// with file arguments made relative to it, and only that directory's files.
func scopeHooks(block, dir string) string {
	slug := strings.ReplaceAll(dir, "/", "-")
	prefix := "^" + regexp.QuoteMeta(dir) + "/"
	var out, hook []string
	flush := func() {
		if len(hook) == 0 {
			return
		}
		hasFiles := false
		for _, l := range hook {
			hasFiles = hasFiles || hookFilesRe.MatchString(l)
		}
		if !hasFiles {
			indent := hook[0][:strings.Index(hook[0], "- id:")] + "  "
			hook = append(hook, indent+"files: "+prefix)
		}
		out = append(out, hook...)
		hook = nil
	}
	for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
		switch {
		case hookIDRe.MatchString(line):
			flush()
			hook = append(hook, hookIDRe.ReplaceAllString(line, "${1}${2}-"+slug+"${3}"))
		case len(hook) == 0:
			out = append(out, line)
		case hookNameRe.MatchString(line):
			hook = append(hook, line+" ("+dir+")")
		case hookEntryRe.MatchString(line):
			m := hookEntryRe.FindStringSubmatch(line)
			hook = append(hook, fmt.Sprintf(`%sbash -c 'cd %s && %s "${@#%s/}"' --`, m[1], dir, m[2], dir))
		case hookFilesRe.MatchString(line):
			m := hookFilesRe.FindStringSubmatch(line)
			hook = append(hook, m[1]+prefix+strings.TrimPrefix(m[2], "^"))
		default:
			hook = append(hook, line)
		}
	}
	flush()
	return strings.Join(out, "\n") + "\n"
}

var versionFileRe = regexp.MustCompile(`(-version-file:\s*)(\S+)`)

func composeCheck(stack string, p fix.Project) ([]byte, error) {
	if p.Devcontainer {
		head, err := fragment("check/head.yml")
		if err != nil {
			return nil, err
		}
		inside, err := fragment("check/devcontainer.yml")
		return []byte(head + inside), err
	}
	nested := p.Nested
	var b strings.Builder
	for _, name := range []string{"check/head.yml", "check/" + stack + ".yml"} {
		part, err := fragment(name)
		if err != nil {
			return nil, err
		}
		b.WriteString(part)
	}
	seen := map[string]bool{stack: true}
	for _, n := range nested {
		if seen[n.Stack] {
			continue // one toolchain per stack; the root's wins
		}
		seen[n.Stack] = true
		part, err := fragment("check/" + n.Stack + ".yml")
		if err != nil {
			return nil, err
		}
		if n.Path == "" {
			b.WriteString(part)
			continue
		}
		fmt.Fprintf(&b, "      # toolchain for %s/\n%s", n.Path, versionFileRe.ReplaceAllString(part, "${1}"+n.Path+"/${2}"))
	}
	tail, err := fragment("check/tail.yml")
	if err != nil {
		return nil, err
	}
	b.WriteString(tail)
	return []byte(b.String()), nil
}
