package docs_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/fleetlint/fleetlint/internal/docs"
)

func testTree() *cobra.Command {
	run := func(*cobra.Command, []string) error { return nil }
	root := &cobra.Command{Use: "tool", Short: "A tool", Long: "tool does\nthings."}
	root.PersistentFlags().StringP("path", "C", ".", "repository root")
	root.PersistentFlags().Bool("color", true, "colored output")
	_ = root.PersistentFlags().SetAnnotation("color", docs.DefaultAnnotation, []string{"on in a terminal"})
	fix := &cobra.Command{
		Use: "fix [rule-id...]", Short: "Fix (or with --apply, write)", RunE: run,
		Long: "fix plans fixes for actions/<repo>.md.\nIt reads $TOOL_TOKEN and\nGITHUB_TOKEN; a per-repo *glob*.\n\n--apply --pr opens a pull request.",
	}
	fix.Flags().Bool("apply", false, "write the changes")
	fix.Flags().String("repos", "", "fleet file (repos: [{path|url}])")
	fix.Flags().Int("jobs", 4, "parallel checks")
	fix.Flags().Bool("secret", false, "hidden")
	_ = fix.Flags().MarkHidden("secret")
	group := &cobra.Command{Use: "group", Short: "Grouped commands"}
	group.AddCommand(&cobra.Command{Use: "leaf <file>", Short: "A leaf", RunE: run})
	root.AddCommand(fix, group, &cobra.Command{Use: "hidden", Hidden: true, RunE: run})
	root.InitDefaultCompletionCmd()
	return root
}

func TestCLIReference(t *testing.T) {
	t.Parallel()
	pages, err := docs.Generate(docs.Options{CLI: &docs.CLI{
		Root:        testTree(),
		ExitCodes:   []docs.ExitCode{{Code: 0, Meaning: "fine"}, {Code: 2, Meaning: "a | b"}},
		Environment: []docs.EnvVar{{Names: []string{"TOOL_TOKEN", "GH_TOKEN"}, Doc: "token"}, {Names: []string{"TOOL_VERIFY"}, Doc: "`cosign`", InstallOnly: true}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var page string
	for _, p := range pages {
		if p.Path == "cli-reference.md" {
			page = string(p.Content)
		}
	}
	for _, want := range []string{
		"| `--color` | on in a terminal | colored output |",
		"| `-C, --path string` | `.` | repository root |",
		"| `-h, --help` |  | help for the command |",
		"### tool fix\n\nFix (or with `--apply`, write).\n\n```text\ntool fix [rule-id...] [flags]\n```",
		"fix plans fixes for actions/&lt;repo&gt;.md. It reads `$TOOL_TOKEN` and `GITHUB_TOKEN`; a per-repo \\*glob\\*.\n\n`--apply --pr` opens a pull request.",
		"| `--repos string` |  | fleet file (repos: [{path\\|url}]) |",
		"| `--jobs int` | `4` | parallel checks |",
		"### tool group\n\nGrouped commands.\n\n```text\ntool group [command]\n```",
		"### tool group leaf\n",
		"### tool completion\n",
		"tool completion bash|fish|powershell|zsh",
		"| `--no-descriptions` |",
		"| `0` | fine |\n| `2` | a \\| b |",
		"| `TOOL_TOKEN`, `GH_TOKEN` | token |",
		"| `TOOL_VERIFY` | Read by the install script, not by `tool`: `cosign` |",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %q in:\n%s", want, page)
		}
	}
	for _, unwanted := range []string{"secret", "hidden", "### tool help", "### tool completion bash", "#### Linux"} {
		if strings.Contains(page, unwanted) {
			t.Errorf("has %q", unwanted)
		}
	}
	plain, err := docs.Generate(docs.Options{})
	if err != nil || slices.ContainsFunc(plain, func(p docs.Page) bool { return p.Path == "cli-reference.md" }) {
		t.Errorf("without a CLI there is no CLI reference (%v)", err)
	}
}
