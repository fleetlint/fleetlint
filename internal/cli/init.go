package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/facts"
	"github.com/fleetlint/fleetlint/internal/forge"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/repo"
)

func initCmd(g *globals, code *int) *cobra.Command {
	var preset string
	var tier int
	var force, pr bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a minimal .fleetlint.yaml with the discovered facts as comments",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd, g, code, initOptions{preset: preset, tier: tier, force: force, pr: pr})
		},
	}
	cmd.Flags().StringVar(&preset, "preset", "recommended", "preset to extend: minimal, recommended")
	cmd.Flags().IntVar(&tier, "tier", 0, "pin the tier (1 production/public, 2 personal tool, 3 experiment); default: keep auto-detection")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing .fleetlint.yaml")
	cmd.Flags().BoolVar(&pr, "pr", false, "commit the config on a branch and open a pull request with the report (needs gh or tea)")
	return cmd
}

// forgeVisibility asks the forge whether the repository is public when
// nothing local says so. It returns "" when there is nothing to ask or the
// forge does not answer; the reason goes to stderr.
func forgeVisibility(cmd *cobra.Command, r *repo.Repo, f facts.Facts) facts.Visibility {
	if f.Visibility != facts.VisibilityUnknown || (f.Forge != "github" && f.Forge != "gitea") {
		return ""
	}
	host, owner, name, ok := forge.ParseRemote(r.RemoteURL())
	if !ok {
		return ""
	}
	public, err := forge.Client{}.Public(cmd.Context(), f.Forge, host, owner, name)
	switch {
	case errors.Is(err, forge.ErrNotFound):
		fmt.Fprintf(cmd.ErrOrStderr(), "visibility: %s does not show %s/%s without a token; left undetected (set GITHUB_TOKEN or GITEA_TOKEN, or pin facts.visibility)\n", host, owner, name) //nolint:errcheck // a note on stderr
		return ""
	case err != nil:
		fmt.Fprintf(cmd.ErrOrStderr(), "visibility: lookup on %s failed (%v); left undetected\n", host, err) //nolint:errcheck // a note on stderr
		return ""
	case public:
		return facts.VisibilityPublic
	}
	return facts.VisibilityPrivate
}

func renderInit(run *engine.Run, preset string, tier int, looked facts.Visibility, runners []string) string {
	f := run.Facts
	var b strings.Builder
	b.WriteString("# .fleetlint.yaml: https://github.com/fleetlint/fleetlint\n")
	b.WriteString("version: 1\n")
	b.WriteString("extends:\n")
	fmt.Fprintf(&b, "  - fleetlint:%s\n", preset)
	if tier != 0 || looked != "" {
		b.WriteString("facts:\n")
	}
	if tier != 0 {
		fmt.Fprintf(&b, "  tier: %d\n", tier)
	}
	if looked != "" {
		fmt.Fprintf(&b, "  visibility: %s   # as the forge reported it at init; update it if the repository changes hands\n", looked)
	}
	fmt.Fprintf(&b, "# discovered: stacks=[%s] forge=%s visibility=%s tier=%d (%s) layout=%s release=%s taskrunner=%s container=%s\n",
		strings.Join(f.Stacks, ","), f.Forge, f.Visibility, f.Tier, f.Sources["tier"], f.Layout, yesNo(f.Release.Exists), f.TaskRunner.Kind, f.Container)
	if len(runners) > 1 {
		fmt.Fprintf(&b, "# task runners found: %s. %s is used; set `facts.taskrunner` to choose another.\n", strings.Join(runners, ", "), f.TaskRunner.Kind)
	}
	if len(f.Members) > 0 {
		fmt.Fprintf(&b, "# scopes (each checked as its own project): %s. List them under `scopes:` to change that.\n", strings.Join(f.Members, ", "))
	}
	b.WriteString("# Facts are re-detected on every run; pin one under `facts:` only when detection is wrong.\n")
	return b.String()
}

func writeFacts(w io.Writer, run *engine.Run) error {
	f := run.Facts
	rows := make([][3]string, 0, 12+len(run.Scopes))
	rows = append(rows, [][3]string{
		{"fact", "value", "source"},
		{"name", f.Name, ""},
		{"stacks", strings.Join(f.Stacks, ", "), string(f.Sources["stacks"])},
		{"forge", f.Forge + hostSuffix(f.ForgeHost), string(f.Sources["forge"])},
		{"visibility", string(f.Visibility), string(f.Sources["visibility"])},
		{"container", f.Container, string(f.Sources["container"])},
		{"tier", fmt.Sprint(f.Tier), string(f.Sources["tier"])},
		{"layout", f.Layout, string(f.Sources["layout"])},
		{"ci", orNone(strings.Join(f.CI, ", ")), "detected"},
		{"release", releaseText(f), "detected"},
		{"taskrunner", f.TaskRunner.Kind + targetsText(f.TaskRunner.Targets), string(f.Sources["taskrunner"])},
		{"hooks", orNone(strings.Join(f.Hooks, ", ")), "detected"},
	}...)
	scopeSource := "detected"
	if run.Config != nil && run.Config.File.Scopes != nil {
		scopeSource = "configured"
	}
	for _, s := range run.Scopes[1:] {
		rows = append(rows, [3]string{"scope", s.Path + " stacks=" + strings.Join(s.Facts.Stacks, ","), scopeSource})
	}
	for _, r := range rows {
		if _, err := fmt.Fprintf(w, "%-12s %-40s %s\n", r[0], r[1], r[2]); err != nil {
			return err
		}
	}
	return nil
}

func releaseText(f facts.Facts) string {
	if !f.Release.Exists {
		return "none detected"
	}
	s := strings.Join(f.Release.Mechanisms, ", ")
	if f.Release.LatestTag != "" {
		s += " latest=" + f.Release.LatestTag
	}
	return s
}

func targetsText(t []string) string {
	if len(t) == 0 {
		return ""
	}
	return " (" + strings.Join(t, ", ") + ")"
}

func hostSuffix(h string) string {
	if h == "" {
		return ""
	}
	return " (" + h + ")"
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeRule(w io.Writer, r model.Rule) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n", r.ID, r.Title)
	fmt.Fprintf(&b, "severity: %s   kind: %s   tiers: %s   stacks: %s   source: %s   layer: %s\n\n",
		r.Severity, r.Kind, orAll(r.Tiers), orAny(r.Stacks), r.Source, r.Layer)
	if policy := policyLine(r); policy != "" {
		fmt.Fprintf(&b, "Policy\n  %s\n\n", policy)
	}
	fmt.Fprintf(&b, "Requirement\n  %s\n\n", strings.TrimSpace(r.Requirement))
	if r.Rationale != "" {
		fmt.Fprintf(&b, "Why\n  %s\n\n", strings.TrimSpace(r.Rationale))
	}
	if r.When != "" {
		fmt.Fprintf(&b, "Applies when\n  %s\n\n", r.When)
	}
	if r.Expr != "" {
		fmt.Fprintf(&b, "Check\n  %s\n\n", strings.TrimSpace(r.Expr))
	}
	fmt.Fprintf(&b, "Fix\n  %s\n", r.Fix.Human)
	if r.Fix.Agent != "" {
		fmt.Fprintf(&b, "\nAgent instruction\n  %s\n", r.Fix.Agent)
	}
	if len(r.Refs) > 0 {
		fmt.Fprintf(&b, "\nSee: %s\n", strings.Join(r.Refs, ", "))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// policyLine describes the organization constraints on a rule, or "".
func policyLine(r model.Rule) string {
	var parts []string
	if r.Locked {
		parts = append(parts, "locked by "+r.PolicySource()+": cannot be disabled or lowered below "+r.Floor().String())
	} else if r.MinSeverity != "" {
		parts = append(parts, "severity floor "+r.MinSeverity+" set by "+r.PolicySource())
	}
	if !r.ExceptionsAllowed() {
		parts = append(parts, "exceptions are not permitted")
	}
	return strings.Join(parts, "; ")
}

func orAll(t []int) string {
	if len(t) == 0 {
		return "all"
	}
	return strings.Trim(strings.Join(strings.Fields(fmt.Sprint(t)), ","), "[]")
}

func orAny(s []string) string {
	if len(s) == 0 {
		return "any"
	}
	return strings.Join(s, ",")
}

type initOptions struct {
	preset    string
	tier      int
	force, pr bool
}

func runInit(cmd *cobra.Command, g *globals, code *int, o initOptions) error {
	r, eff, err := load(cmd.Context(), g)
	if err != nil {
		return err
	}
	if eff.Exists && !o.force {
		return &configError{fmt.Errorf("%s already exists (use --force to overwrite)", config.FileName)}
	}
	if o.tier < 0 || o.tier > 3 {
		return &configError{errors.New("--tier must be 1, 2 or 3")}
	}
	if _, err := (catalog.Loader{}).Load("fleetlint:" + o.preset); err != nil {
		return &configError{err}
	}
	run, err := engine.Evaluate(cmd.Context(), r, eff)
	if err != nil {
		return err
	}
	// Decide from what the repository itself says, not from the file being
	// replaced: with --force the old file's pins would otherwise be lost.
	looked := forgeVisibility(cmd, r, facts.Discover(r, facts.Overrides{}))
	// The header describes the facts as they will be under the file written.
	run.Facts = facts.Discover(r, facts.Overrides{Visibility: looked, Tier: o.tier})
	content := renderInit(run, o.preset, o.tier, looked, r.RunnersFound())
	target := filepath.Join(r.Root, config.FileName)
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil { //nolint:gosec // a config file must be readable by other tools
		return err
	}
	// Report the state under the configuration just written, not the defaults.
	if run, err = reevaluate(cmd.Context(), r); err != nil {
		return err
	}
	c := model.Summarize(run.Results)
	if _, err = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n\n%s\ncurrent state with this config: %d passed, %d warnings, %d errors. Run `fleetlint` to see them.\n",
		config.FileName, content, c.Pass, c.Warnings, c.Errors); err != nil {
		return err
	}
	*code = ExitOK
	if !o.pr {
		return nil
	}
	return openOnboardingPR(cmd, r, run)
}

func reevaluate(ctx context.Context, r *repo.Repo) (*engine.Run, error) {
	eff, err := config.Load(r.Root, config.Options{Loader: catalog.RemoteLoader(ctx)})
	if err != nil {
		return nil, &configError{err}
	}
	return engine.Evaluate(ctx, r, eff)
}
