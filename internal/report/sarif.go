package report

import (
	"encoding/json"
	"io"

	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/model"
)

// SARIF 2.1.0, the subset GitHub code scanning and most viewers read.
type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version,omitempty"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	ShortDescription sarifText          `json:"shortDescription"`
	FullDescription  sarifText          `json:"fullDescription"`
	Help             sarifText          `json:"help"`
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
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifText       `json:"message"`
	Locations []sarifLocation `json:"locations,omitempty"`
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

func writeSARIF(w io.Writer, run *engine.Run, version string) error {
	log := sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
	}
	driver := sarifDriver{Name: "fleetlint", Version: version, InformationURI: "https://github.com/fleetlint/fleetlint"}
	seen := map[string]bool{}
	var results []sarifResult
	for _, r := range run.Results {
		if r.Status != model.StatusFail && r.Status != model.StatusError {
			continue
		}
		if !seen[r.Rule.ID] {
			seen[r.Rule.ID] = true
			driver.Rules = append(driver.Rules, toSarifRule(r.Rule))
		}
		results = append(results, toSarifResults(r)...)
	}
	if results == nil {
		results = []sarifResult{}
	}
	if driver.Rules == nil {
		driver.Rules = []sarifRule{}
	}
	log.Runs = []sarifRun{{Tool: sarifTool{Driver: driver}, Results: results}}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}

func toSarifRule(r model.Rule) sarifRule {
	help := r.Fix.Human
	if r.Rationale != "" {
		help = r.Rationale + "\n\nFix: " + r.Fix.Human
	}
	return sarifRule{
		ID:               r.ID,
		Name:             r.Title,
		ShortDescription: sarifText{r.Title},
		FullDescription:  sarifText{r.Requirement},
		Help:             sarifText{help},
		Properties:       map[string]any{"tags": []string{"repository-setup"}, "source": r.Source, "layer": r.Layer},
		DefaultConfig:    sarifConfiguration{Level: sarifLevel(r.Severity)},
	}
}

func toSarifResults(r model.Result) []sarifResult {
	if r.Status == model.StatusError {
		return []sarifResult{{RuleID: r.Rule.ID, Level: "warning", Message: sarifText{"rule could not run: " + r.Err}}}
	}
	out := make([]sarifResult, 0, len(r.Findings))
	for _, f := range r.Findings {
		if f.Exception != nil {
			continue
		}
		res := sarifResult{RuleID: r.Rule.ID, Level: sarifLevel(r.Severity), Message: sarifText{f.Message}}
		path := f.Path
		if r.Scope != "" && path != "" {
			path = r.Scope + "/" + path
		}
		if path != "" {
			loc := sarifLocation{PhysicalLocation: sarifPhysical{ArtifactLocation: sarifArtifact{URI: path}}}
			if f.Line > 0 {
				loc.PhysicalLocation.Region = &sarifRegion{StartLine: f.Line}
			}
			res.Locations = []sarifLocation{loc}
		}
		out = append(out, res)
	}
	return out
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
