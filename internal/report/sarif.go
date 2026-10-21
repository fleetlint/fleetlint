package report

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

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
}

// sarifSuppression marks a finding an exception or the baseline accepted.
type sarifSuppression struct {
	Kind          string `json:"kind"`
	Status        string `json:"status"`
	Justification string `json:"justification"`
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

// WriteSARIF renders results as one SARIF run. Failing, excepted and
// baselined findings are results (the latter two suppressed), rule errors
// are invocation notifications, passes and n/a are left out. Every result
// has a location: a finding without a path is anchored to its scope's
// configuration file (or README.md), looked up under opts.Root.
func WriteSARIF(w io.Writer, results []model.Result, catalogs []config.CatalogInfo, opts Options) error {
	driver := sarifDriver{Name: "fleetlint", Version: opts.Version, InformationURI: "https://github.com/fleetlint/fleetlint", Properties: map[string]any{"catalog": orUnknown(opts.Catalog), "catalogs": catalogs}}
	inv := sarifInvocation{ExecutionSuccessful: true}
	seen := map[string]bool{}
	anchors := &anchorCache{root: opts.Root, found: map[string]string{}}
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
	if driver.Rules == nil {
		driver.Rules = []sarifRule{}
	}
	log := sarifLog{Schema: "https://json.schemastore.org/sarif-2.1.0.json", Version: "2.1.0", Runs: []sarifRun{{
		Tool: sarifTool{Driver: driver}, AutomationDetails: sarifAutomation{ID: "fleetlint/" + runName(opts)}, Invocations: []sarifInvocation{inv}, Results: sarifResults,
	}}}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
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
			RuleID: r.Rule.ID, Level: sarifLevel(r.Severity), Message: sarifText{f.Message},
			PartialFingerprints: map[string]string{fingerprintKey: fingerprint(r.Rule.ID, r.Scope, f.Path, f.Message)},
		}
		loc := sarifLocation{PhysicalLocation: sarifPhysical{ArtifactLocation: sarifArtifact{URI: anchors.anchor(r.Scope)}}}
		if f.Path != "" {
			loc.PhysicalLocation.ArtifactLocation.URI = path.Join(r.Scope, f.Path)
			if f.Line > 0 {
				loc.PhysicalLocation.Region = &sarifRegion{StartLine: f.Line}
			}
		}
		res.Locations = []sarifLocation{loc}
		switch {
		case f.Exception != nil:
			res.Suppressions = []sarifSuppression{{Kind: "external", Status: "accepted", Justification: orText(f.Exception.Reason, "excepted")}}
		case f.Baselined:
			res.Suppressions = []sarifSuppression{{Kind: "external", Status: "accepted", Justification: "baselined: grandfathered by the baseline file"}}
		}
		out = append(out, res)
	}
	return out
}

// fingerprint identifies a finding across runs: the rule, where it was
// found and what it said, independent of line numbers and ordering.
func fingerprint(ruleID, scope, path, message string) string {
	sum := sha256.Sum256([]byte(ruleID + "\x00" + scope + "\x00" + path + "\x00" + message))
	return hex.EncodeToString(sum[:])
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
