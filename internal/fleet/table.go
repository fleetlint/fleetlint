package fleet

import (
	"fmt"
	"html"
	"strings"
)

// tcell is one table cell. Markdown renders Text (as code when Code is
// set); HTML also uses Class, Href, Title and the compliance bar.
type tcell struct {
	Text  string
	Code  bool
	Class string
	Href  string
	Title string
	// Bar is a percentage drawn as a bar before the text; Sort is the
	// value the HTML sorts by when the text is not a plain number.
	Bar  *float64
	Sort string
}

func txt(s string) tcell  { return tcell{Text: s} }
func code(s string) tcell { return tcell{Text: s, Code: true} }
func num(n int) tcell     { return tcell{Text: fmt.Sprint(n)} }

// numIf is a count colored with class when it is not zero.
func numIf(n int, class string) tcell {
	c := num(n)
	if n > 0 {
		c.Class = class
	}
	return c
}

// table is a section rendered the same way into Markdown and HTML, so both
// formats always carry the same content.
type table struct {
	Title string
	Intro string
	Head  []string
	Rows  [][]tcell
	// Sortable tables get clickable headers in HTML.
	Sortable bool
	ID       string
}

func (t table) markdown(b *strings.Builder) {
	if len(t.Rows) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n\n", t.Title)
	if t.Intro != "" {
		b.WriteString(t.Intro + "\n\n")
	}
	b.WriteString("| " + strings.Join(t.Head, " | ") + " |\n|" + strings.Repeat("---|", len(t.Head)) + "\n")
	for _, row := range t.Rows {
		b.WriteString("|")
		for _, c := range row {
			s := escapeCell(c.Text)
			if c.Code && s != "" {
				s = "`" + s + "`"
			}
			b.WriteString(" " + s + " |")
		}
		b.WriteString("\n")
	}
}

func (t table) html(b *strings.Builder) {
	if len(t.Rows) == 0 {
		return
	}
	fmt.Fprintf(b, "<h2>%s</h2>", html.EscapeString(t.Title))
	if t.Intro != "" {
		b.WriteString("<p class=muted>" + inlineHTML(t.Intro) + "</p>")
	}
	attrs := ""
	if t.ID != "" {
		attrs += ` id="` + html.EscapeString(t.ID) + `"`
	}
	if t.Sortable {
		attrs += ` class=sort`
	}
	b.WriteString("<div class=wrap><table" + attrs + "><thead><tr>")
	for _, h := range t.Head {
		b.WriteString("<th>" + inlineHTML(h) + "</th>")
	}
	b.WriteString("</tr></thead><tbody>")
	for _, row := range t.Rows {
		b.WriteString("<tr>")
		for _, c := range row {
			b.WriteString(c.html())
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</tbody></table></div>")
}

func (c tcell) html() string {
	var attrs string
	if c.Class != "" {
		attrs += ` class="` + c.Class + `"`
	}
	if c.Sort != "" {
		attrs += ` data-v="` + html.EscapeString(c.Sort) + `"`
	}
	if c.Title != "" {
		attrs += ` title="` + html.EscapeString(c.Title) + `"`
	}
	body := html.EscapeString(c.Text)
	if c.Code && body != "" {
		body = "<code>" + body + "</code>"
	}
	if c.Href != "" {
		body = `<a href="` + html.EscapeString(c.Href) + `">` + body + "</a>"
	}
	if c.Bar != nil {
		body = fmt.Sprintf(`<span class="bar %s" style="width:%.0fpx"></span>`, barClass(*c.Bar), *c.Bar) + body
	}
	return "<td" + attrs + ">" + body + "</td>"
}

// barClass colors compliance like a traffic light.
func barClass(pct float64) string {
	switch {
	case pct < 50:
		return "b-err"
	case pct < 80:
		return "b-warn"
	}
	return "b-ok"
}

// inlineHTML escapes s and renders `code` spans, the only inline markup
// the reports use.
func inlineHTML(s string) string {
	parts := strings.Split(html.EscapeString(s), "`")
	if len(parts)%2 == 0 { // unbalanced: leave the backticks alone
		return html.EscapeString(s)
	}
	var b strings.Builder
	for i, p := range parts {
		if i%2 == 1 {
			b.WriteString("<code>" + p + "</code>")
		} else {
			b.WriteString(p)
		}
	}
	return b.String()
}
