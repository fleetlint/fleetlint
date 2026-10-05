package repo

import (
	"regexp"
	"sort"
	"strings"
)

// Task runner kinds.
const (
	RunnerMake   = "make"
	RunnerJust   = "just"
	RunnerTask   = "task"
	RunnerNPM    = "npm-scripts"
	RunnerGradle = "gradle"
	RunnerNone   = "none"
)

// Runner describes the task runner of a scope in the terms every kind
// shares: targets, what each depends on, and the commands each runs.
type Runner struct {
	Kind string
	// File is the runner's definition file; Cmd is how a target is invoked
	// (`make`, `just`, `task`, `npm run`, `./gradlew`).
	File, Cmd string
	Targets   []string
	Deps      map[string][]string
	Recipes   map[string][]string
}

type runnerSpec struct {
	kind, cmd string
	files     []string
}

// runnerSpecs is the detection order when nothing is configured.
var runnerSpecs = []runnerSpec{
	{RunnerMake, "make", []string{"Makefile"}},
	{RunnerJust, "just", []string{"justfile", "Justfile", ".justfile"}},
	{RunnerTask, "task", []string{"Taskfile.yml", "Taskfile.yaml", "taskfile.yml", "taskfile.yaml", "Taskfile.dist.yml"}},
	{RunnerGradle, "./gradlew", []string{"gradlew", "build.gradle.kts", "build.gradle"}},
	{RunnerNPM, "npm run", []string{"package.json"}},
}

// RunnerKinds lists the kinds a configuration may name.
func RunnerKinds() []string {
	kinds := make([]string, 0, len(runnerSpecs))
	for _, s := range runnerSpecs {
		kinds = append(kinds, s.kind)
	}
	return kinds
}

// RunnerFile is the file a new runner of the kind is written to, and
// RunnerCmd how its targets are invoked; make is the default for kinds
// without a file of their own.
func RunnerFile(kind string) string {
	for _, s := range runnerSpecs {
		if s.kind == kind && (kind == RunnerMake || kind == RunnerJust || kind == RunnerTask) {
			return s.files[0]
		}
	}
	return "Makefile"
}

// RunnerCmd returns the command that invokes a target of the kind.
func RunnerCmd(kind string) string {
	for _, s := range runnerSpecs {
		if s.kind == kind {
			return s.cmd
		}
	}
	return "make"
}

// RunnersFound lists the kinds whose file is present, in detection order.
func (r *Repo) RunnersFound() []string {
	var found []string
	for _, s := range runnerSpecs {
		for _, f := range s.files {
			if r.Has(f) {
				found = append(found, s.kind)
				break
			}
		}
	}
	return found
}

// Runner reads the scope's task runner. want names a kind to use even when
// another runner's file is present; "" takes the first one found.
func (r *Repo) Runner(want string) Runner {
	for _, s := range runnerSpecs {
		if want != "" && s.kind != want {
			continue
		}
		for _, f := range s.files {
			if r.Has(f) {
				return r.readRunner(s, f)
			}
		}
		if want != "" {
			// Configured but not there yet: rules report what is missing.
			return Runner{Kind: s.kind, File: s.files[0], Cmd: s.cmd, Deps: map[string][]string{}, Recipes: map[string][]string{}}
		}
	}
	return Runner{Kind: RunnerNone, File: "Makefile", Cmd: "make", Deps: map[string][]string{}, Recipes: map[string][]string{}}
}

func (r *Repo) readRunner(s runnerSpec, file string) Runner {
	out := Runner{Kind: s.kind, File: file, Cmd: s.cmd, Deps: map[string][]string{}, Recipes: map[string][]string{}}
	switch s.kind {
	case RunnerMake:
		if mf, ok := r.Makefile(file); ok {
			out.Targets, out.Deps, out.Recipes = mf.Targets, mf.Deps, mf.Recipes
		}
	case RunnerJust:
		text, _ := r.Text(file)
		parseJustfile(text, &out)
	case RunnerTask:
		if doc, ok, err := r.Doc(FormatYAML, file); ok && err == nil {
			parseTaskfile(doc, &out)
		}
	case RunnerNPM:
		if doc, ok, err := r.Doc(FormatJSON, file); ok && err == nil {
			parseScripts(doc, &out)
		}
	}
	sort.Strings(out.Targets)
	return out
}

var (
	justRecipeRe = regexp.MustCompile(`^@?([A-Za-z_][A-Za-z0-9_-]*)((?:\s+[^:=]+)?)\s*:([^=]|$)`)
	justDepRe    = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_-]*`)
	justQuotedRe = regexp.MustCompile(`"[^"]*"|'[^']*'`)
)

// parseJustfile reads recipes, their dependencies and their command lines.
// Like the Makefile reader it is structural: imports, modules and
// conditionals are not evaluated.
func parseJustfile(text string, out *Runner) {
	var current string
	for _, line := range strings.Split(text, "\n") {
		if current != "" && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			if cmd := strings.TrimSpace(line); cmd != "" {
				out.Recipes[current] = append(out.Recipes[current], cmd)
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		current = ""
		m := justRecipeRe.FindStringSubmatch(line)
		if m == nil || strings.Contains(line, ":=") {
			continue
		}
		current = m[1]
		out.Targets = append(out.Targets, current)
		deps := line[strings.Index(line, ":")+1:]
		if i := strings.Index(deps, "#"); i >= 0 {
			deps = deps[:i]
		}
		// `a (b "arg") && c`: names only, without arguments.
		deps = justQuotedRe.ReplaceAllString(deps, "")
		out.Deps[current] = justDepRe.FindAllString(deps, -1)
	}
}

// parseScripts reads the `scripts` of a package.json.
func parseScripts(doc any, out *Runner) {
	m, _ := doc.(map[string]any)
	scripts, _ := m["scripts"].(map[string]any)
	for name, cmd := range scripts {
		out.Targets = append(out.Targets, name)
		if s, ok := cmd.(string); ok {
			out.Recipes[name] = []string{s}
		}
	}
}

// parseTaskfile reads the `tasks:` map of a Taskfile.
func parseTaskfile(doc any, out *Runner) {
	root, _ := doc.(map[string]any)
	tasks, _ := root["tasks"].(map[string]any)
	for name, raw := range tasks {
		out.Targets = append(out.Targets, name)
		cmds, deps := taskParts(raw)
		for _, d := range deps {
			if dep := taskRef(d); dep != "" {
				out.Deps[name] = append(out.Deps[name], dep)
			}
		}
		for _, c := range cmds {
			line, dep := taskCommand(c)
			if line != "" {
				out.Recipes[name] = append(out.Recipes[name], line)
			}
			if dep != "" {
				out.Deps[name] = append(out.Deps[name], dep)
			}
		}
	}
}

// taskParts returns the commands and dependencies of one task, whichever
// of the three spellings (string, list, map) it uses.
func taskParts(raw any) (cmds, deps []any) {
	switch t := raw.(type) {
	case string:
		return []any{t}, nil
	case []any:
		return t, nil
	case map[string]any:
		cmds, _ = t["cmds"].([]any)
		if one, ok := t["cmd"].(string); ok {
			cmds = append(cmds, one)
		}
		deps, _ = t["deps"].([]any)
	}
	return cmds, deps
}

// taskCommand returns the shell line of a command entry and, when the entry
// calls another task, that task: in effect a dependency.
func taskCommand(c any) (line, dep string) {
	switch v := c.(type) {
	case string:
		return strings.TrimSpace(v), ""
	case map[string]any:
		s, _ := v["cmd"].(string)
		return strings.TrimSpace(s), taskRef(v)
	}
	return "", ""
}

// taskRef returns the task a `deps`/`cmds` entry refers to.
func taskRef(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		s, _ := t["task"].(string)
		return s
	}
	return ""
}
