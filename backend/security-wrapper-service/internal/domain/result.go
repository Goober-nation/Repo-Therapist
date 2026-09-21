package domain

// AppsecResult is everything the Grade service needs from one "get AppSec data for this repo"
// call. Security wrapper never scores it and never writes it anywhere — it's handed back over
// gRPC and this process forgets it immediately after.
type AppsecResult struct {
	// HasScan is false when the repo has never been scanned (AppSec API's NO_RUNS_YET /
	// equivalent 404 on /v1/scans/latest) — a real "no data" case, not an error. Every other
	// field is zero-valued when this is false.
	HasScan bool

	Scan         ScanSummary
	OpenCounts   SeverityCounts
	DefectGroups []DefectGroupSummary
}
