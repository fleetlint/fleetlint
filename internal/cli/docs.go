package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/docs"
)

func schemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Print the JSON Schema for .fleetlint.yaml",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := cmd.OutOrStdout().Write(config.Schema)
			return err
		},
	}
}

func docsCmd() *cobra.Command {
	var out, indexFile string
	var check bool
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Generate the rule and CEL reference pages (or verify they are current with --check)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pages, err := docs.Generate(docs.Options{IndexFile: indexFile})
			if err != nil {
				return err
			}
			if check {
				return verifyDocs(out, pages)
			}
			if err := writeDocs(out, pages); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "wrote %d pages under %s\n", len(pages), out)
			return err
		},
	}
	cmd.Flags().StringVar(&out, "out", "docs", "directory to write into")
	cmd.Flags().StringVar(&indexFile, "index-file", "README.md", "name of the rule index inside rules/ (_index.md for a Hugo section)")
	cmd.Flags().BoolVar(&check, "check", false, "exit 2 if the committed pages differ from what would be generated")
	return cmd
}

func writeDocs(out string, pages []docs.Page) error {
	for _, p := range pages {
		target := filepath.Join(out, p.Path)
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(target, p.Content, 0o644); err != nil { //nolint:gosec // documentation files
			return err
		}
	}
	return nil
}

func verifyDocs(out string, pages []docs.Page) error {
	var stale []string
	for _, p := range pages {
		current, err := os.ReadFile(filepath.Join(out, p.Path)) //nolint:gosec // paths come from the generator, under --out
		if err != nil || !bytes.Equal(current, p.Content) {
			stale = append(stale, p.Path)
		}
	}
	if len(stale) > 0 {
		return &configError{fmt.Errorf("generated docs are stale: %s (run `fleetlint docs`)", strings.Join(stale, ", "))}
	}
	return nil
}
