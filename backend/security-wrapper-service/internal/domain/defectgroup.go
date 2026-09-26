package domain

// Severity mirrors the AppSec API's Severity string enum (never the DefectGroupDto's raw int32
// field of the same name).
type Severity string

const (
	SeverityCritical Severity = "CRITICAL"
	SeverityHigh     Severity = "HIGH"
	SeverityMedium   Severity = "MEDIUM"
	SeverityLow      Severity = "LOW"
	SeverityNone     Severity = "NONE"
)

// OpenSeverities is every tier that counts toward repo_appsec's open_*_count columns — strict
// status=OPEN only, not the in-progress triage states. See agents_memory/MEMORY.md (2026-09-21).
var OpenSeverities = []Severity{SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow}

// SeverityCounts holds the OPEN-status defect-group count per severity tier, each read from
// /v1/defect-groups?severity=X&status=OPEN&pageSize=1's totalSize — never decoded from the list
// endpoint's own int32 severity/status fields.
type SeverityCounts struct {
	Critical int64
	High     int64
	Medium   int64
	Low      int64
}

// DefectGroupSummary is one human-readable defect, parsed from the AppSec API's generated SARIF
// report. The report bytes are discarded immediately after parsing — this is the only thing kept.
type DefectGroupSummary struct {
	RuleID    string
	RuleName  string
	Severity  Severity
	File      string
	StartLine int
	EndLine   int
	Message   string
}
