package catalog

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/fix"
	"github.com/fleetlint/fleetlint/internal/repo"
)

// The hook configuration and the check workflow are assembled from a shared
// head, one fragment per stack and (for the workflow) a shared tail, so a
// repository with a nested project of another stack gets both stacks in one
// file.
const (
	hooksTemplate = "pre-commit-config.yaml"
	checkTemplate = "check.yml"
)

// composed splits "<stack>/<file>" for the assembled templates.
func composed(name string) (stack, file string, ok bool) {
	stack, file, found := strings.Cut(name, "/")
	return stack, file, found && (file == hooksTemplate || file == checkTemplate || file == devcontainerTemplate)
}

// Compose returns a template assembled for the project: its stacks, its
// task runner and where its targets run. ok is false for templates that are
// plain files; a nil body with ok means the project does not get the file.
func (t Templates) Compose(name string, p fix.Project) (body []byte, ok bool, err error) {
	if name == repo.RunnerFile(repo.RunnerMake) || name == repo.RunnerFile(repo.RunnerJust) || name == repo.RunnerFile(repo.RunnerTask) {
		body, err = t.composeRunner(name, p)
		return body, true, err
	}
	stack, file, ok := composed(name)
	if !ok {
		return nil, false, nil
	}
	switch {
	case file == hooksTemplate:
		body, err = t.composeHooks(stack, p)
	case file == checkTemplate:
		body, err = t.composeCheck(stack, p)
	case p.Container == facts.ContainerDevcontainer:
		body, err = composeDevcontainer(stack, p)
	default:
		// Only the dev container mode has a devcontainer.json.
		return nil, true, nil
	}
	return body, true, err
}

// runnerCmd is how the project invokes a target.
func runnerCmd(p fix.Project) string {
	if p.Runner == "" {
		return repo.RunnerCmd(repo.RunnerMake)
	}
	return repo.RunnerCmd(p.Runner)
}

// withRunner rewrites the `make <target>` invocations of a shared fragment
// for the project's task runner.
func withRunner(text string, p fix.Project) string {
	cmd := runnerCmd(p)
	if cmd == "make" {
		return text
	}
	for _, target := range []string{"check-fast", "check", "tools"} {
		text = strings.ReplaceAll(text, "make "+target, cmd+" "+target)
	}
	return text
}

// devImages are the images targets run in with `container: docker|podman`.
var devImages = map[string]string{
	"go":      "mcr.microsoft.com/devcontainers/go:1",
	"python":  "mcr.microsoft.com/devcontainers/python:3",
	"rust":    "mcr.microsoft.com/devcontainers/rust:1",
	"node":    "mcr.microsoft.com/devcontainers/javascript-node:22",
	"kotlin":  "mcr.microsoft.com/devcontainers/java:21",
	"flutter": devcontainerFlutter,
}

func devImage(stack string) string {
	if img, ok := devImages[stack]; ok {
		return img
	}
	return devcontainerBase
}

// containerPrefix is the shell text that runs the command following it
// inside the project's container; pwd is how the runner file spells the
// working directory.
func containerPrefix(p fix.Project, pwd, image string) string {
	switch p.Container {
	case facts.ContainerDevcontainer:
		return "devcontainer up --workspace-folder . >/dev/null && devcontainer exec --workspace-folder . "
	case facts.ContainerDocker, facts.ContainerPodman:
		return fmt.Sprintf(`%s run --rm -v "%s":/work -w /work -e IN_CONTAINER=1 %s `, p.Container, pwd, image)
	}
	return ""
}

const switchComment = `# CONTAINER=1 runs every target in the container (%s); CONTAINER=0 runs it on this machine.
# Inside the container targets always run directly.`

func containerNeeds(p fix.Project) string {
	if p.Container == facts.ContainerDevcontainer {
		return "the dev container; needs Docker and the devcontainer CLI"
	}
	return "an image run with " + p.Container
}

// composeRunner returns the runner file for the project's kind, with the
// container switch when targets run in a container.
func (t Templates) composeRunner(file string, p fix.Project) ([]byte, error) {
	plain, err := t.fragment(file)
	if err != nil {
		return nil, err
	}
	inContainer := p.Container != "" && p.Container != facts.ContainerNone
	switch file {
	case repo.RunnerFile(repo.RunnerJust):
		return []byte(composeJustfile(plain, p, inContainer)), nil
	case repo.RunnerFile(repo.RunnerTask):
		return []byte(composeTaskfile(plain, p, inContainer)), nil
	}
	if !inContainer {
		return []byte(plain), nil
	}
	return composeMakefile(plain, p)
}

var phonyRe = regexp.MustCompile(`(?m)^\.PHONY: (.*)$`)

// composeMakefile wraps the Makefile template's targets in the container
// switch: outside the container every target is forwarded into it.
func composeMakefile(plain string, p fix.Project) ([]byte, error) {
	m := phonyRe.FindStringSubmatchIndex(plain)
	if m == nil {
		return nil, fmt.Errorf("the Makefile template has no .PHONY line to take the targets from")
	}
	var b strings.Builder
	b.WriteString(plain[:m[0]])
	fmt.Fprintf(&b, switchComment+"\nCONTAINER ?= 1\n", containerNeeds(p))
	if p.Container != facts.ContainerDevcontainer {
		fmt.Fprintf(&b, "DEV_IMAGE ?= %s\n", devImage(p.Stack))
	}
	fmt.Fprintf(&b, "TARGETS := %s\n\nifeq ($(CONTAINER)$(IN_CONTAINER),1)\n.PHONY: $(TARGETS)\n$(TARGETS):\n", plain[m[2]:m[3]])
	forward := "make $@ $(filter-out CONTAINER=%,$(MAKEOVERRIDES)) CONTAINER=0"
	if p.Container == facts.ContainerDevcontainer {
		b.WriteString("\t@devcontainer up --workspace-folder . >/dev/null\n\tdevcontainer exec --workspace-folder . " + forward + "\n")
	} else {
		b.WriteString("\t" + containerPrefix(p, "$(CURDIR)", "$(DEV_IMAGE)") + forward + "\n")
	}
	b.WriteString("else\n" + strings.TrimRight(plain[m[0]:], "\n") + "\nendif\n")
	return []byte(b.String()), nil
}

// composeJustfile fills the justfile template: without a container the
// recipes run their commands directly, with one each command is prefixed.
func composeJustfile(plain string, p fix.Project, inContainer bool) string {
	if !inContainer {
		return strings.ReplaceAll(strings.ReplaceAll(plain, "%SWITCH%\n", ""), "%RUN%", "")
	}
	sw := fmt.Sprintf(switchComment, containerNeeds(p)) + "\n"
	// just does not interpolate inside string literals: the image is concatenated.
	prefix := "'" + containerPrefix(p, "$PWD", "' + dev_image + '") + "'"
	if p.Container != facts.ContainerDevcontainer {
		sw += fmt.Sprintf("dev_image := env_var_or_default(\"DEV_IMAGE\", \"%s\")\n", devImage(p.Stack))
	}
	sw += fmt.Sprintf("run := if env_var_or_default(\"IN_CONTAINER\", \"\") == \"1\" { \"\" } else if env_var_or_default(\"CONTAINER\", \"1\") == \"0\" { \"\" } else { %s }\n", prefix)
	return strings.ReplaceAll(strings.ReplaceAll(plain, "%SWITCH%\n", sw), "%RUN%", "{{run}}")
}

// composeTaskfile does the same for a Taskfile.
func composeTaskfile(plain string, p fix.Project, inContainer bool) string {
	if !inContainer {
		return strings.ReplaceAll(strings.ReplaceAll(plain, "%SWITCH%\n", ""), "%RUN%", "")
	}
	sw := fmt.Sprintf(switchComment, containerNeeds(p)) + "\nvars:\n"
	image := "{{.DEV_IMAGE}}"
	if p.Container != facts.ContainerDevcontainer {
		sw += fmt.Sprintf("  DEV_IMAGE: '{{.DEV_IMAGE | default \"%s\"}}'\n", devImage(p.Stack))
	}
	sw += fmt.Sprintf("  RUN: '{{if or (eq (.IN_CONTAINER | default \"\") \"1\") (eq (.CONTAINER | default \"1\") \"0\")}}{{else}}%s{{end}}'\n",
		strings.ReplaceAll(containerPrefix(p, "$PWD", image), "'", "''"))
	return strings.ReplaceAll(strings.ReplaceAll(plain, "%SWITCH%\n", sw), "%RUN%", "{{.RUN}}")
}

func (t Templates) fragment(name string) (string, error) {
	b, err := fs.ReadFile(t.fsys, "templates/"+name)
	if err != nil {
		return "", fmt.Errorf("no template %q in this catalog", name)
	}
	return string(b), nil
}

func (t Templates) composeHooks(stack string, p fix.Project) ([]byte, error) {
	nested := p.Nested
	head, err := t.fragment("pre-commit/head.yaml")
	if err != nil {
		return nil, err
	}
	head = withRunner(head, p)
	own, err := t.fragment("pre-commit/" + stack + ".yaml")
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString(head)
	fmt.Fprintf(&b, "  # %s hooks\n%s", stack, own)
	for _, n := range nested {
		block, err := t.fragment("pre-commit/" + n.Stack + ".yaml")
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

// composeCheck assembles the check workflow. On the machine it sets up
// every toolchain; with a dev container it builds the container and runs
// the check in it; with docker or podman the job runs in the image.
func (t Templates) composeCheck(stack string, p fix.Project) ([]byte, error) {
	head, err := t.fragment("check/head.yml")
	if err != nil {
		return nil, err
	}
	switch p.Container {
	case facts.ContainerDevcontainer:
		inside, err := t.fragment("check/devcontainer.yml")
		return []byte(head + withRunner(inside, p)), err
	case facts.ContainerDocker, facts.ContainerPodman:
		head = strings.Replace(head, "    runs-on: ubuntu-latest\n",
			"    runs-on: ubuntu-latest\n    container: "+devImage(stack)+"   # the image the task runner uses locally\n    env:\n      IN_CONTAINER: \"1\"\n", 1)
	}
	var b strings.Builder
	b.WriteString(head)
	if p.Container == "" || p.Container == facts.ContainerNone {
		if err := t.writeToolchains(&b, stack, p.Nested); err != nil {
			return nil, err
		}
	}
	if p.Runner == repo.RunnerJust || p.Runner == repo.RunnerTask {
		setup, err := t.fragment("check/runner-" + p.Runner + ".yml")
		if err != nil {
			return nil, err
		}
		b.WriteString(setup)
	}
	tail, err := t.fragment("check/tail.yml")
	if err != nil {
		return nil, err
	}
	b.WriteString(withRunner(tail, p))
	return []byte(b.String()), nil
}

// writeToolchains adds the setup steps of the root stack and of every
// nested stack that differs from it.
func (t Templates) writeToolchains(b *strings.Builder, stack string, nested []fix.Nested) error {
	own, err := t.fragment("check/" + stack + ".yml")
	if err != nil {
		return err
	}
	b.WriteString(own)
	seen := map[string]bool{stack: true}
	for _, n := range nested {
		if seen[n.Stack] {
			continue // one toolchain per stack; the root's wins
		}
		seen[n.Stack] = true
		part, err := t.fragment("check/" + n.Stack + ".yml")
		if err != nil {
			return err
		}
		if n.Path == "" {
			b.WriteString(part)
			continue
		}
		fmt.Fprintf(b, "      # toolchain for %s/\n%s", n.Path, versionFileRe.ReplaceAllString(part, "${1}"+n.Path+"/${2}"))
	}
	return nil
}
