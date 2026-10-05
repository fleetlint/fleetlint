// Package cli wires the commands. It owns exit codes and flags; everything
// else is delegated so the commands stay thin and testable.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/repo"
	"github.com/fleetlint/fleetlint/internal/report"
	_ "github.com/fleetlint/fleetlint/internal/rules/builtin" // register Go rules
)

// Exit codes. Documented in the README; scripts depend on them.
const (
	ExitOK       = 0
	ExitFindings = 1
	ExitConfig   = 2
	ExitInternal = 3
)

// Version is set by the build (ldflags); "dev" otherwise.
var Version = "dev"

type globals struct {
	path    string
	format  string
	failOn  string
	color   bool
	verbose bool
	deep    bool
	require []string
}

// Run executes the CLI and returns the process exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) (int, error) {
	g := &globals{}
	root := &cobra.Command{
		Use:           "fleetlint",
		Short:         "Policy for a fleet of repositories",
		Long:          "fleetlint checks how a repository is set up (hooks, task runner, lint config, CI, release) against a catalog of rules, and tells you how to fix the gaps.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version + "\ncatalog " + catalog.ModuleVersion() + " (" + catalog.ModulePath + ")",
	}
	root.Flags().Bool("version", false, "print the version") // explicit so cobra does not claim -v
	root.PersistentFlags().StringVarP(&g.path, "path", "C", ".", "repository root")
	root.PersistentFlags().StringArrayVar(&g.require, "require", nil, "catalog that must be part of the effective config (name, ref or ref#sha256-<digest>); repeatable; exit 2 when missing")
	root.PersistentFlags().BoolVar(&g.color, "color", isTerminal(stdout), "colored output")
	root.SetOut(stdout)
	root.SetErr(stderr)

	var code int
	//nolint:contextcheck // cobra hands the context from ExecuteContext to each command via cmd.Context()
	root.AddCommand(checkCmd(g, &code), factsCmd(g), explainCmd(g), initCmd(g, &code), fixCmd(g, &code), fleetCmd(g, &code), baselineCmd(g), catalogCmd(), schemaCmd(), docsCmd())
	root.SetArgs(withDefaultCommand(root, args))
	if err := root.ExecuteContext(ctx); err != nil {
		return classify(err), err
	}
	return code, nil
}

func classify(err error) int {
	var ce *configError
	if errors.As(err, &ce) {
		return ExitConfig
	}
	return ExitInternal
}

type configError struct{ err error }

func (e *configError) Error() string { return e.err.Error() }
func (e *configError) Unwrap() error { return e.err }

func load(ctx context.Context, g *globals) (*repo.Repo, *config.Effective, error) {
	r, err := repo.Open(ctx, g.path)
	if err != nil {
		return nil, nil, &configError{err}
	}
	eff, err := config.Load(r.Root, config.Options{Loader: catalog.RemoteLoader(ctx)})
	if err != nil {
		return nil, nil, &configError{err}
	}
	if err := eff.Requires(g.require); err != nil {
		return nil, nil, &configError{err}
	}
	return r, eff, nil
}

func checkCmd(g *globals, code *int) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Evaluate the repository against its rules (default command)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, eff, err := load(cmd.Context(), g)
			if err != nil {
				return err
			}
			run, err := engine.EvaluateWith(cmd.Context(), r, eff, engine.Options{Deep: g.deep})
			if err != nil {
				return err
			}
			markFixable(r, run)
			if err = report.Write(cmd.OutOrStdout(), run, report.Format(g.format), report.Options{Color: g.color && g.format == "table", Verbose: g.verbose, Version: Version, Catalog: catalog.ModuleVersion()}); err != nil {
				return &configError{err}
			}
			*code = exitFor(run, g.failOn)
			return nil
		},
	}
	cmd.Flags().StringVarP(&g.format, "format", "f", "table", "output format: table, json, agent, sarif, markdown")
	cmd.Flags().StringVar(&g.failOn, "fail-on", "error", "lowest severity that fails the run: error, warning, info, never")
	cmd.Flags().BoolVarP(&g.verbose, "verbose", "v", false, "also list passing and not-applicable rules")
	cmd.Flags().BoolVar(&g.deep, "deep", false, "also run rules that execute repository code (make check); never used by hooks")
	return cmd
}

func exitFor(run *engine.Run, failOn string) int {
	c := model.Summarize(run.Results)
	if c.Error > 0 {
		return ExitFindings
	}
	switch failOn {
	case "never":
		return ExitOK
	case "info":
		if c.Fail > 0 {
			return ExitFindings
		}
	case "warning":
		if c.Errors+c.Warnings > 0 {
			return ExitFindings
		}
	default:
		if c.Errors > 0 {
			return ExitFindings
		}
	}
	return ExitOK
}

func factsCmd(g *globals) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "facts",
		Short: "Show what fleetlint discovered about the repository",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, eff, err := load(cmd.Context(), g)
			if err != nil {
				return err
			}
			run, err := engine.Evaluate(cmd.Context(), r, eff)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), run.Facts)
			}
			return writeFacts(cmd.OutOrStdout(), run)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	return cmd
}

func explainCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "explain <rule-id>",
		Short: "Print a rule's requirement, rationale and fix",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, eff, err := load(cmd.Context(), g)
			if err != nil {
				return err
			}
			for _, rule := range eff.Rules {
				if rule.ID == args[0] {
					return writeRule(cmd.OutOrStdout(), rule)
				}
			}
			return &configError{fmt.Errorf("rule %q is not in the loaded catalogs (%s)", args[0], strings.Join(eff.File.Extends, ", "))}
		},
	}
}

func catalogCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "catalog", Short: "Inspect catalogs"}
	cmd.AddCommand(&cobra.Command{
		Use:   "digest <file>",
		Short: "Print the sha256 to pin a catalog with in extends",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := os.ReadFile(args[0])
			if err != nil {
				return &configError{err}
			}
			if _, err := catalog.Parse(b); err != nil {
				return &configError{err}
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "sha256-%s\n", catalog.Digest(b))
			return err
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "presets",
		Short: "List the catalogs built into this binary",
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, p := range catalog.Presets() {
				cats, err := catalog.Loader{}.Load("fleetlint:" + p)
				if err != nil {
					return err
				}
				own := cats[len(cats)-1]
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "fleetlint:%-14s %-8s %2d rules  includes: %-22s %s\n", p, own.Metadata.Version, len(own.Rules), orNone(strings.Join(own.Metadata.Includes, ",")), strings.TrimSpace(own.Metadata.Description)); err != nil {
					return err
				}
			}
			return nil
		},
	})
	return cmd
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0 && os.Getenv("NO_COLOR") == ""
}

// withDefaultCommand makes `fleetlint [flags]` behave as `fleetlint check [flags]`:
// if no argument names a command, "check" is inserted before the first
// non-flag position so persistent flags given earlier still apply.
func withDefaultCommand(root *cobra.Command, args []string) []string {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "--version" {
			return args
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		for _, c := range root.Commands() {
			if c.Name() == a || c.HasAlias(a) {
				return args
			}
		}
	}
	return append([]string{"check"}, args...)
}
