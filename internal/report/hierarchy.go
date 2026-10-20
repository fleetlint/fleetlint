package report

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/fleetlint/fleetlint/internal/config"
)

// HierarchyIntro is the one line every format puts above the catalog list.
const HierarchyIntro = "catalogs in the order they applied (a later one adjusts an earlier one within its policy):"

// WriteHierarchy prints the catalogs of a run as a text table: order,
// how each was referenced, its label, what it calls itself, how it is
// pinned, what it builds on and what it contributed.
func WriteHierarchy(w io.Writer, h []config.CatalogInfo) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	ew := &errWriter{w: tw, plain: true}
	ew.printf("#\tcatalog\tlabel\tname\tpinned by\tbuilds on\tcontributes\n")
	for _, c := range h {
		ew.printf("%d\t%s\t%s\t%s\t%s\t%s\t%s\n", c.Order, refText(c), c.Label, c.Name+"@"+c.Version, pinText(c), buildsOn(h, c), contributes(c))
	}
	if ew.err != nil {
		return ew.err
	}
	return tw.Flush()
}

// HierarchyMarkdown renders the same list as a Markdown table.
func HierarchyMarkdown(h []config.CatalogInfo) string {
	var b strings.Builder
	b.WriteString("| # | Catalog | Label | Name | Pinned by | Builds on | Contributes |\n|---|---|---|---|---|---|---|\n")
	for _, c := range h {
		fmt.Fprintf(&b, "| %d | `%s` | %s | %s | %s | %s | %s |\n", c.Order, codeText(refText(c)), cellText(c.Label), cellText(c.Name+"@"+c.Version), cellText(pinText(c)), cellText(buildsOn(h, c)), cellText(contributes(c)))
	}
	return b.String()
}

// refText is how the catalog was referenced: its sources-file name with
// the reference it resolved to, or the reference alone.
func refText(c config.CatalogInfo) string {
	if c.Alias != "" {
		return c.Alias + " = " + c.Ref
	}
	return c.Ref
}

// pinText names what fixes the catalog's content: the digest of an OCI
// artifact, the sha256 of the file, or nothing for a file of the repository.
func pinText(c config.CatalogInfo) string {
	switch {
	case c.Artifact != "":
		return "artifact sha256:" + c.Artifact
	case c.Digest != "" && c.Origin != config.OriginRepository:
		return "sha256-" + c.Digest
	case c.Origin == config.OriginRepository:
		return "file in the repository"
	}
	return ""
}

// buildsOn lists the orders of the catalogs an entry includes, as it names
// them; an include that is not in the run (an error would have said so) is
// named instead.
func buildsOn(h []config.CatalogInfo, c config.CatalogInfo) string {
	if len(c.Includes) == 0 {
		return "-"
	}
	byRef := map[string]int{}
	for _, o := range h {
		byRef[o.Ref] = o.Order
	}
	parts := make([]string, 0, len(c.Includes))
	for _, inc := range c.Includes {
		if n, ok := byRef[inc]; ok {
			parts = append(parts, "#"+strconv.Itoa(n))
		} else {
			parts = append(parts, inc)
		}
	}
	return strings.Join(parts, ", ")
}

func contributes(c config.CatalogInfo) string {
	var parts []string
	for _, p := range []struct {
		n    int
		what string
	}{{c.Rules, "rule"}, {c.Overrides, "override"}, {c.Exceptions, "exception"}} {
		if p.n == 1 {
			parts = append(parts, "1 "+p.what)
		} else if p.n > 1 {
			parts = append(parts, strconv.Itoa(p.n)+" "+p.what+"s")
		}
	}
	if len(parts) == 0 {
		return "nothing"
	}
	return strings.Join(parts, ", ")
}
