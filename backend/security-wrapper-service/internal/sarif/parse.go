// Package sarif parses SARIF 2.1.0 reports downloaded from the AppSec API into
// domain.DefectGroupSummary rows. Parsing is the only thing done with the report — the caller
// discards the raw bytes right after this returns, nothing is persisted.
package sarif

import (
	"encoding/json"
	"fmt"
	"repo-therapist/security-wrapper-service/internal/domain"
	"strconv"
)

type report struct {
	Runs []run `json:"runs"`
}

type run struct {
	Tool    tool     `json:"tool"`
	Results []result `json:"results"`
}

type tool struct {
	Driver driver `json:"driver"`
}

type driver struct {
	Rules []rule `json:"rules"`
}

type rule struct {
	ID                   string         `json:"id"`
	Name                 string         `json:"name"`
	ShortDescription     *message       `json:"shortDescription"`
	DefaultConfiguration *defaultConfig `json:"defaultConfiguration"`
	Properties           map[string]any `json:"properties"`
}

type defaultConfig struct {
	Level string `json:"level"`
}

type result struct {
	RuleID    string     `json:"ruleId"`
	RuleIndex *int       `json:"ruleIndex"`
	Level     string     `json:"level"`
	Message   message    `json:"message"`
	Locations []location `json:"locations"`
}

type message struct {
	Text string `json:"text"`
}

type location struct {
	PhysicalLocation physicalLocation `json:"physicalLocation"`
}

type physicalLocation struct {
	ArtifactLocation artifactLocation `json:"artifactLocation"`
	Region           *region          `json:"region"`
}

type artifactLocation struct {
	URI string `json:"uri"`
}

type region struct {
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine"`
}

// Parse turns raw SARIF report bytes into defect-group summaries. Every run/result in the report
// is flattened into one list — the SARIF report itself already reflects the AppSec API's chosen
// scope (combined engines, latest scan), so no further filtering happens here.
func Parse(raw []byte) ([]domain.DefectGroupSummary, error) {
	var rep report
	if err := json.Unmarshal(raw, &rep); err != nil {
		return nil, fmt.Errorf("sarif: %w", err)
	}

	var summaries []domain.DefectGroupSummary
	for _, r := range rep.Runs {
		for _, res := range r.Results {
			rl := findRule(r.Tool.Driver.Rules, res.RuleID, res.RuleIndex)
			summaries = append(summaries, toSummary(res, rl))
		}
	}
	return summaries, nil
}

func findRule(rules []rule, ruleID string, ruleIndex *int) *rule {
	if ruleIndex != nil && *ruleIndex >= 0 && *ruleIndex < len(rules) {
		return &rules[*ruleIndex]
	}
	for i := range rules {
		if rules[i].ID == ruleID {
			return &rules[i]
		}
	}
	return nil
}

func toSummary(res result, rl *rule) domain.DefectGroupSummary {
	summary := domain.DefectGroupSummary{
		RuleID:   res.RuleID,
		RuleName: res.RuleID,
		Severity: severityOf(res, rl),
		Message:  res.Message.Text,
	}
	if rl != nil {
		if rl.Name != "" {
			summary.RuleName = rl.Name
		} else if rl.ShortDescription != nil && rl.ShortDescription.Text != "" {
			summary.RuleName = rl.ShortDescription.Text
		}
	}
	if len(res.Locations) > 0 {
		phys := res.Locations[0].PhysicalLocation
		summary.File = phys.ArtifactLocation.URI
		if phys.Region != nil {
			summary.StartLine = phys.Region.StartLine
			summary.EndLine = phys.Region.EndLine
		}
	}
	return summary
}

// severityOf prefers the rule's "security-severity" property (a CVSS-like 0-10 score, the SARIF
// convention used by most SAST/SCA tools), falling back to the SARIF "level" (error/warning/note)
// on the result or, failing that, the rule's own default level.
func severityOf(res result, rl *rule) domain.Severity {
	if rl != nil {
		if score, ok := securityScore(rl.Properties); ok {
			return severityFromScore(score)
		}
	}
	level := res.Level
	if level == "" && rl != nil && rl.DefaultConfiguration != nil {
		level = rl.DefaultConfiguration.Level
	}
	return severityFromLevel(level)
}

func securityScore(props map[string]any) (float64, bool) {
	raw, ok := props["security-severity"]
	if !ok {
		return 0, false
	}
	switch v := raw.(type) {
	case float64:
		return v, true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func severityFromScore(score float64) domain.Severity {
	switch {
	case score >= 9.0:
		return domain.SeverityCritical
	case score >= 7.0:
		return domain.SeverityHigh
	case score >= 4.0:
		return domain.SeverityMedium
	case score > 0:
		return domain.SeverityLow
	default:
		return domain.SeverityNone
	}
}

func severityFromLevel(level string) domain.Severity {
	switch level {
	case "error":
		return domain.SeverityHigh
	case "warning":
		return domain.SeverityMedium
	case "note":
		return domain.SeverityLow
	default:
		return domain.SeverityNone
	}
}
