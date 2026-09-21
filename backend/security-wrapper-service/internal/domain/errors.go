package domain

import "errors"

var (
	// ErrNoAccess is returned when the AppSec API responds 403 for the given repo — the repo
	// exists but the caller's token has no AppSec access to it (role-gated, not our decision).
	ErrNoAccess = errors.New("no access rights for the specified repository")
	// ErrUnauthenticated means the incoming gRPC call had no usable bearer token in its metadata.
	ErrUnauthenticated = errors.New("missing or invalid authorization token")
	// ErrExternalAPI wraps any unexpected non-2xx response from the AppSec API.
	ErrExternalAPI = errors.New("external AppSec API error")
	// ErrReportFailed means report generation finished with OperationStatus FAILED.
	ErrReportFailed = errors.New("report generation failed")
	// ErrReportTimeout means the report-generation operation never reached done=true in time.
	ErrReportTimeout = errors.New("timed out waiting for report generation")
	// ErrInternalError covers request-building/decoding failures on our own side.
	ErrInternalError = errors.New("internal service error")
)
