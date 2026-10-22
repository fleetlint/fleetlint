package report

import (
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/fleetlint/fleetlint/internal/baseline"
	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/model"
)

// SARIF 2.1.0 (OASIS), the parts GitHub code scanning and most viewers read.
type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool              sarifTool         `json:"tool"`
	ColumnKind        string            `json:"columnKind"`
	AutomationDetails sarifAutomation   `json:"automationDetails"`
	Invocations       []sarifInvocation `json:"invocations"`
	Results           []sarifResult     `json:"results"`
}

// sarifAutomation keys the run, so a viewer tells repositories apart.
type sarifAutomation struct {
	ID string `json:"id"`
}

type sarifInvocation struct {
	ExecutionSuccessful bool                `json:"executionSuccessful"`
	Notifications       []sarifNotification `json:"toolExecutionNotifications,omitempty"`
}

// sarifNotification reports a rule that could not run; it is not a finding.
type sarifNotification struct {
	Level          string       `json:"level"`
	Message        sarifText    `json:"message"`
	AssociatedRule sarifRuleRef `json:"associatedRule"`
}

type sarifRuleRef struct {
	ID string `json:"id"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version,omitempty"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
	// Properties carries the catalog version the binary was built against.
	Properties map[string]any `json:"properties,omitempty"`
}

type sarifRule struct {
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	ShortDescription sarifText          `json:"shortDescription"`
	FullDescription  sarifText          `json:"fullDescription"`
	Help             sarifText          `json:"help"`
	HelpURI          string             `json:"helpUri"`
	Properties       map[string]any     `json:"properties,omitempty"`
	DefaultConfig    sarifConfiguration `json:"defaultConfiguration"`
}

type sarifConfiguration struct {
	Level string `json:"level"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID              string             `json:"ruleId"`
	Level               string             `json:"level"`
	Message             sarifText          `json:"message"`
	Locations           []sarifLocation    `json:"locations"`
	PartialFingerprints map[string]string  `json:"partialFingerprints"`
	Suppressions        []sarifSuppression `json:"suppressions,omitempty"`
	Fixes               []sarifFix         `json:"fixes,omitempty"`
}

// sarifFix is what `fleetlint fix --apply` would change, one replacement
// per file.
type sarifFix struct {
	Description     sarifText           `json:"description"`
	ArtifactChanges []sarifArtifactEdit `json:"artifactChanges"`
}

type sarifArtifactEdit struct {
	ArtifactLocation sarifArtifact      `json:"artifactLocation"`
	Replacements     []sarifReplacement `json:"replacements"`
}

type sarifReplacement struct {
	DeletedRegion   sarifCharRegion `json:"deletedRegion"`
	InsertedContent sarifContent    `json:"insertedContent"`
}

// sarifCharRegion counts UTF-16 code units, the run's columnKind.
type sarifCharRegion struct {
	CharOffset int `json:"charOffset"`
	CharLength int `json:"charLength"`
}

type sarifContent struct {
	Text string `json:"text"`
}

// sarifSuppression marks a finding an exception or the baseline accepted;
// an exception's expiry is in the properties.
type sarifSuppression struct {
	Kind          string         `json:"kind"`
	Status        string         `json:"status"`
	Justification string         `json:"justification"`
	Properties    map[string]any `json:"properties,omitempty"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysical `json:"physicalLocation"`
}

type sarifPhysical struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           *sarifRegion  `json:"region,omitempty"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}

// fingerprintKey names the fingerprint scheme; a new scheme gets a new key.
const fingerprintKey = "fleetlint/v1"

// rulesSite hosts one page per rule, named after the id with "/" as "--".
const rulesSite = "https://fleetlint.org/rules/"

// securityFamilies are the rule families code scanning shows as security
// alerts, with a security-severity by rule severity.
var securityFamilies = map[string]bool{"release": true, "ci": true, "deps": true, "containers": true, "hooks": true, "repo": true}

func writeSARIF(w io.Writer, run *engine.Run, opts Options) error {
	return WriteSARIF(w, run.Results, run.Config.Hierarchy(), opts)
}

// WriteSARIF renders results as one SARIF run per scope, keyed
// `fleetlint/<repository>/` for the root and `fleetlint/<repository>/<scope>/`
// for a scope. Code scanning takes an id up to its last `/` as the run's
// category, so the trailing slash gives every scope its own alert set. Failing,
// excepted and baselined findings are results (the latter two suppressed),
// rule errors are invocation notifications, passes and n/a are left out.
// Every result has a location: a finding without a path is anchored to its
// scope's configuration file (or README.md), looked up under opts.Root.
func WriteSARIF(w io.Writer, results []model.Result, catalogs []config.CatalogInfo, opts Options) error {
	anchors := &anchorCache{root: opts.Root, found: map[string]string{}}
	var scopes []string
	byScope := map[string][]model.Result{}
	for _, r := range results {
		if _, ok := byScope[r.Scope]; !ok {
			scopes = append(scopes, r.Scope)
		}
		byScope[r.Scope] = append(byScope[r.Scope], r)
	}
	if len(scopes) == 0 {
		scopes = []string{""}
	}
	name := runName(opts)
	runs := make([]sarifRun, 0, len(scopes))
	for _, scope := range scopes {
		run := sarifRunFor(byScope[scope], catalogs, opts, anchors)
		run.AutomationDetails = sarifAutomation{ID: path.Join("fleetlint", name, scope) + "/"}
		runs = append(runs, run)
	}
	log := sarifLog{Schema: "https://json.schemastore.org/sarif-2.1.0.json", Version: "2.1.0", Runs: runs}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}

// sarifRunFor is one scope's run: its rules, results and notifications.
func sarifRunFor(results []model.Result, catalogs []config.CatalogInfo, opts Options, anchors *anchorCache) sarifRun {
	driver := sarifDriver{Name: "fleetlint", Version: opts.Version, InformationURI: "https://github.com/fleetlint/fleetlint", Properties: map[string]any{"catalog": orUnknown(opts.Catalog), "catalogs": catalogs}, Rules: []sarifRule{}}
	inv := sarifInvocation{ExecutionSuccessful: true}
	seen := map[string]bool{}
	sarifResults := []sarifResult{}
	for _, r := range results {
		switch r.Status {
		case model.StatusFail, model.StatusExcepted, model.StatusBaselined, model.StatusError:
		case model.StatusPass, model.StatusNotApplicable:
			continue
		}
		if !seen[r.Rule.ID] {
			seen[r.Rule.ID] = true
			driver.Rules = append(driver.Rules, toSarifRule(r.Rule))
		}
		if r.Status == model.StatusError {
			inv.ExecutionSuccessful = false
			inv.Notifications = append(inv.Notifications, sarifNotification{Level: "error", Message: sarifText{"rule could not run: " + r.Err}, AssociatedRule: sarifRuleRef{r.Rule.ID}})
			continue
		}
		sarifResults = append(sarifResults, toSarifResults(r, anchors)...)
	}
	return sarifRun{Tool: sarifTool{Driver: driver}, ColumnKind: "utf16CodeUnits", Invocations: []sarifInvocation{inv}, Results: sarifResults}
}

// runName is the repository name: as given, else the root directory's.
func runName(opts Options) string {
	if opts.Name != "" {
		return opts.Name
	}
	abs, err := filepath.Abs(orDot(opts.Root))
	if err != nil {
		return "repository"
	}
	return filepath.Base(abs)
}

func orDot(s string) string {
	if s == "" {
		return "."
	}
	return s
}

func toSarifRule(r model.Rule) sarifRule {
	help := r.Fix.Human
	if r.Rationale != "" {
		help = r.Rationale + "\n\nFix: " + r.Fix.Human
	}
	family, _, _ := strings.Cut(r.ID, "/")
	props := map[string]any{"tags": []string{"repository-setup", family}, "source": r.Source, "selected_by": r.SelectedBy, "label": r.Label}
	if securityFamilies[family] {
		props["tags"] = []string{"repository-setup", family, "security"}
		props["security-severity"] = securitySeverity(r.Severity)
	}
	if len(r.Refs) > 0 {
		props["refs"] = r.Refs
	}
	return sarifRule{
		ID:               r.ID,
		Name:             r.Title,
		ShortDescription: sarifText{r.Title},
		FullDescription:  sarifText{r.Requirement},
		Help:             sarifText{help},
		HelpURI:          rulesSite + strings.ReplaceAll(r.ID, "/", "--") + "/",
		Properties:       props,
		DefaultConfig:    sarifConfiguration{Level: sarifLevel(r.Severity)},
	}
}

// securitySeverity is the code scanning score (0-10) for a severity.
func securitySeverity(s model.Severity) string {
	switch s {
	case model.SeverityError:
		return "8.0"
	case model.SeverityWarning:
		return "5.0"
	case model.SeverityInfo:
		return "2.0"
	}
	return "0.0"
}

func toSarifResults(r model.Result, anchors *anchorCache) []sarifResult {
	out := make([]sarifResult, 0, len(r.Findings))
	for _, f := range r.Findings {
		res := sarifResult{
			RuleID: r.Rule.ID, Level: sarifLevel(r.Severity), Message: sarifText{f.Text()},
			PartialFingerprints: map[string]string{fingerprintKey: baseline.Key(r.Rule.ID, r.Scope, f)},
		}
		loc := sarifLocation{PhysicalLocation: sarifPhysical{ArtifactLocation: sarifArtifact{URI: anchors.anchor(r.Scope)}}}
		if where := f.Where(); where != "" {
			loc.PhysicalLocation.ArtifactLocation.URI = path.Join(r.Scope, where)
			if f.Path != "" && f.Line > 0 {
				loc.PhysicalLocation.Region = &sarifRegion{StartLine: f.Line}
			}
		}
		res.Locations = []sarifLocation{loc}
		if len(out) == 0 && len(r.Edits) > 0 {
			res.Fixes = []sarifFix{toSarifFix(r)}
		}
		switch {
		case f.Exception != nil:
			res.Suppressions = []sarifSuppression{exceptionSuppression(f.Exception)}
		case f.Baselined:
			res.Suppressions = []sarifSuppression{{Kind: "external", Status: "accepted", Justification: "baselined: grandfathered by the baseline file"}}
		}
		out = append(out, res)
	}
	return out
}

// exceptionSuppression is an exception as SARIF writes it: the reason, and
// the expiry and approver as properties.
func exceptionSuppression(ex *model.Exception) sarifSuppression {
	sup := sarifSuppression{Kind: "external", Status: "accepted", Justification: orText(ex.Reason, "excepted")}
	props := map[string]any{}
	if ex.Until != "" {
		sup.Justification += " (until " + ex.Until + ")"
		props["until"] = ex.Until
	}
	if ex.ApprovedBy != "" {
		props["approved_by"] = ex.ApprovedBy
	}
	if len(props) > 0 {
		sup.Properties = props
	}
	return sup
}

// toSarifFix is the rule's planned edits, on the first finding only: a
// viewer applying it fixes the rule, a copy on every finding would conflict.
func toSarifFix(r model.Result) sarifFix {
	fix := sarifFix{Description: sarifText{orText(r.Rule.Fix.Human, "apply the fix of "+r.Rule.ID)}}
	for _, e := range r.Edits {
		fix.ArtifactChanges = append(fix.ArtifactChanges, sarifArtifactEdit{
			ArtifactLocation: sarifArtifact{URI: e.Path},
			Replacements:     []sarifReplacement{replacement(e.Old, e.New)},
		})
	}
	return fix
}

// replacement is the one span where old and new differ, between their
// common prefix and suffix, in UTF-16 code units.
func replacement(old, updated string) sarifReplacement {
	a, b := utf16.Encode([]rune(old)), utf16.Encode([]rune(updated))
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	if pre > 0 && utf16.IsSurrogate(rune(a[pre-1])) {
		pre-- // keep a surrogate pair whole
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	if suf > 0 && utf16.IsSurrogate(rune(a[len(a)-suf])) {
		suf--
	}
	return sarifReplacement{
		DeletedRegion:   sarifCharRegion{CharOffset: pre, CharLength: len(a) - pre - suf},
		InsertedContent: sarifContent{Text: string(utf16.Decode(b[pre : len(b)-suf]))},
	}
}

// anchorCache finds, once per scope, the file a pathless finding points
// at: the scope's configuration file, else its README.md, else the
// configuration file's expected path.
type anchorCache struct {
	root  string
	found map[string]string
}

func (c *anchorCache) anchor(scope string) string {
	if a, ok := c.found[scope]; ok {
		return a
	}
	anchor := path.Join(scope, config.FileName)
	if !c.exists(anchor) {
		if readme := path.Join(scope, "README.md"); c.exists(readme) {
			anchor = readme
		}
	}
	c.found[scope] = anchor
	return anchor
}

func (c *anchorCache) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(orDot(c.root), filepath.FromSlash(rel)))
	return err == nil
}

func orText(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func sarifLevel(s model.Severity) string {
	switch s {
	case model.SeverityError:
		return "error"
	case model.SeverityWarning:
		return "warning"
	case model.SeverityInfo:
		return "note"
	}
	return "none"
}
