package fleet

import (
	"strings"
	"sync"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/model"
)

// rulePages is where the rules of the built-in catalog are documented, one
// page per id with the family separator spelled "--".
const rulePages = "https://fleetlint.org/rules/"

// maxRuleHint bounds the requirement quoted in a rule's hover text.
const maxRuleHint = 400

// ruleDoc is what the report says about a rule beyond its id.
type ruleDoc struct {
	Hint string // hover text: title, severity, requirement
	Href string // its page on fleetlint.org, or its first reference
}

// builtinIDs are the rules that have a page on fleetlint.org.
var builtinIDs = sync.OnceValue(func() map[string]bool {
	ids := map[string]bool{}
	if lib, err := catalog.Builtin().Library(); err == nil {
		for _, id := range lib.IDs() {
			ids[id] = true
		}
	}
	return ids
})

// ruleDocs describes every rule from the first result that carries it.
func ruleDocs(rep *Report) map[string]ruleDoc {
	docs := map[string]ruleDoc{}
	for _, r := range rep.Repos {
		for _, res := range r.Results {
			if _, ok := docs[res.Rule.ID]; !ok {
				docs[res.Rule.ID] = docOf(res.Rule)
			}
		}
	}
	return docs
}

func docOf(r model.Rule) ruleDoc {
	hint := []string{r.Title, "severity: " + r.Severity.String()}
	if req := strings.Join(strings.Fields(r.Requirement), " "); req != "" {
		if len(req) > maxRuleHint {
			req = strings.TrimSpace(req[:strings.LastIndex(req[:maxRuleHint], " ")]) + " ..."
		}
		hint = append(hint, "", req)
	}
	d := ruleDoc{Hint: strings.TrimSpace(strings.Join(hint, "\n"))}
	switch {
	case builtinIDs()[r.ID]:
		d.Href = rulePages + strings.ReplaceAll(r.ID, "/", "--") + "/"
	case len(r.Refs) > 0:
		d.Href = r.Refs[0].URL
	}
	return d
}

// ruleCell is a rule id that shows its summary on hover and links to its docs.
func (v *view) ruleCell(id string) tcell {
	d := v.docs[id]
	return tcell{Text: id, Code: true, Title: d.Hint, Href: d.Href}
}
