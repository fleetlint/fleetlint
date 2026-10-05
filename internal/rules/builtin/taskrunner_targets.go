package builtin

import (
	"fmt"
	"strings"

	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/rules"
)

var defaultTargets = []string{"fmt", "lint", "test", "cover", "audit", "check", "check-fast"}

type taskrunnerTargets struct{}

func (taskrunnerTargets) ID() string { return "taskrunner/targets" }

// Check verifies the task-runner contract against whatever runner the scope
// uses: Makefile targets, package.json scripts, or Gradle (where the
// `check` task is built in and the rest are conventions we cannot read
// without running Gradle, so only Makefile-less Gradle roots are accepted
// as satisfied by convention).
func (taskrunnerTargets) Check(ctx rules.Context) (rules.Outcome, error) {
	required := rules.StringList(ctx.Params, "required", defaultTargets)
	switch ctx.Facts.TaskRunner.Kind {
	case "make":
		return checkMakefile(ctx, required), nil
	case "npm-scripts":
		return checkTargets(ctx.Facts.TaskRunner.Targets, required, "package.json", "script"), nil
	case "gradle":
		return rules.Outcome{Evidence: "gradle: `check` is the built-in aggregate task"}, nil
	}
	return rules.Outcome{Findings: []model.Finding{{
		Path:    "Makefile",
		Message: "no task runner found; the contract targets are " + strings.Join(required, ", "),
	}}}, nil
}

func checkMakefile(ctx rules.Context, required []string) rules.Outcome {
	mf, ok := ctx.Repo.Makefile("Makefile")
	if !ok {
		return rules.Outcome{Findings: []model.Finding{{Path: "Makefile", Message: "Makefile is missing"}}}
	}
	out := checkTargets(mf.Targets, required, "Makefile", "target")
	for _, t := range required {
		if isPlaceholder(mf.Recipes[t]) {
			out.Findings = append(out.Findings, model.Finding{
				Path: "Makefile", Message: fmt.Sprintf("target %q is a TODO placeholder", t), Evidence: "placeholder",
			})
		}
	}
	if len(out.Findings) > 0 {
		out.Evidence = ""
	}
	return out
}

func checkTargets(have []string, required []string, file, noun string) rules.Outcome {
	set := map[string]bool{}
	for _, t := range have {
		set[t] = true
	}
	var out rules.Outcome
	for _, t := range required {
		if !set[t] {
			out.Findings = append(out.Findings, model.Finding{
				Path: file, Message: fmt.Sprintf("%s %q is missing", noun, t), Evidence: "missing",
			})
		}
	}
	if len(out.Findings) == 0 {
		out.Evidence = fmt.Sprintf("all %d contract %ss present in %s", len(required), noun, file)
	}
	return out
}

// isPlaceholder is true when a recipe only echoes a to-do note (the template's
// initial state).
func isPlaceholder(recipe []string) bool {
	if len(recipe) == 0 {
		return false
	}
	for _, line := range recipe {
		l := strings.ToLower(line)
		if !strings.HasPrefix(l, "@echo") || !strings.Contains(l, "todo") {
			return false
		}
	}
	return true
}
