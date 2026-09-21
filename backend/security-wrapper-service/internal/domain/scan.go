package domain

import "time"

// ScanStatus is a resolved string, never the AppSec API's raw int32 ScanSummaryDto.status —
// see agents_memory/topics/appsec-api.md for why that field can't be decoded directly.
type ScanStatus string

const (
	ScanStatusRunning  ScanStatus = "RUNNING"
	ScanStatusFinished ScanStatus = "FINISHED"
	ScanStatusFailed   ScanStatus = "FAILED"
	// ScanStatusUnknown means the latest scan's uuid didn't turn up in any of the status-filtered
	// /v1/scans lookups we tried — shouldn't normally happen, reported rather than guessed at.
	ScanStatusUnknown ScanStatus = "UNKNOWN"
)

// ScanSummary is the resolved view of the AppSec API's ScanSummaryDto for a repo's latest scan.
type ScanSummary struct {
	UUID         string
	Status       ScanStatus
	ScanType     string
	CommitHash   string
	TimeStarted  *time.Time
	TimeFinished *time.Time

	TotalDefectGroups int64
	FoundDefectGroups int64
	OpenDefectGroups  int64
}
