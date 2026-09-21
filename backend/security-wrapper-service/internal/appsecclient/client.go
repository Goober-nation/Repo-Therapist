// Package appsecclient wraps the SourceCraft AppSec API (https://appsec.sourcecraft.tech), a
// separate REST subdomain from the main SourceCraft API — see
// agents_memory/topics/appsec-api.md for the full research behind every non-obvious call here.
package appsecclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"repo-therapist/security-wrapper-service/internal/domain"
	"time"
)

type Client struct {
	httpClient http.Client
	baseURL    string
}

func New(httpClient http.Client, baseURL string) *Client {
	return &Client{httpClient: httpClient, baseURL: baseURL}
}

// --- /v1/scans/latest -------------------------------------------------------------------------

type scanSummaryDTO struct {
	UUID              string `json:"uuid"`
	CommitHash        string `json:"commitHash"`
	ScanType          string `json:"scanType"`
	TimeStarted       *int64 `json:"timeStarted"`
	TimeFinished      *int64 `json:"timeFinished"`
	TotalDefectGroups *int64 `json:"totalDefectGroups"`
	FoundDefectGroups *int64 `json:"foundDefectGroups"`
	OpenDefectGroups  *int64 `json:"openDefectGroups"`
	// Status is deliberately omitted — it's the AppSec API's raw int32 trap. Resolved
	// separately via resolveScanStatus's status-filtered /v1/scans lookups instead.
}

// GetLatestScan fetches a repo's most recent scan. A nil result with a nil error means the repo
// has never been scanned (404 from the AppSec API) — a valid "no data" state, not a failure.
func (c *Client) GetLatestScan(ctx context.Context, token, gitRepo string) (*domain.ScanSummary, error) {
	req, err := c.newRequest(ctx, token, "/v1/scans/latest", url.Values{"gitRepo": {gitRepo}})
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrExternalAPI, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, nil
	case http.StatusForbidden:
		return nil, domain.ErrNoAccess
	default:
		return nil, unexpectedStatus(resp)
	}

	var dto scanSummaryDTO
	if err := json.NewDecoder(resp.Body).Decode(&dto); err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}

	status, err := c.resolveScanStatus(ctx, token, gitRepo, dto.UUID)
	if err != nil {
		return nil, err
	}

	summary := &domain.ScanSummary{
		UUID:         dto.UUID,
		Status:       status,
		ScanType:     dto.ScanType,
		CommitHash:   dto.CommitHash,
		TimeStarted:  epochMillisPtr(dto.TimeStarted),
		TimeFinished: epochMillisPtr(dto.TimeFinished),
	}
	if dto.TotalDefectGroups != nil {
		summary.TotalDefectGroups = *dto.TotalDefectGroups
	}
	if dto.FoundDefectGroups != nil {
		summary.FoundDefectGroups = *dto.FoundDefectGroups
	}
	if dto.OpenDefectGroups != nil {
		summary.OpenDefectGroups = *dto.OpenDefectGroups
	}
	return summary, nil
}

// --- /v1/scans (status resolution) ------------------------------------------------------------

type scansListResponse struct {
	Data []struct {
		UUID string `json:"uuid"`
	} `json:"data"`
}

// resolveScanStatus works around ScanSummaryDto.status being a raw int32: /v1/scans accepts
// `status` as a proper string-enum *filter*, so we ask for the most recent scan (pageSize=1) per
// candidate status and check whether it's the same scan we're resolving. Same trick as the
// defect-group severity counts, just applied to scans. See agents_memory/topics/appsec-api.md.
func (c *Client) resolveScanStatus(ctx context.Context, token, gitRepo, scanUUID string) (domain.ScanStatus, error) {
	candidates := []domain.ScanStatus{domain.ScanStatusFinished, domain.ScanStatusFailed, domain.ScanStatusRunning}
	for _, status := range candidates {
		params := url.Values{
			"gitRepo":  {gitRepo},
			"pageSize": {"1"},
			"status":   {string(status)},
		}
		req, err := c.newRequest(ctx, token, "/v1/scans", params)
		if err != nil {
			return "", err
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("%w: %w", domain.ErrExternalAPI, err)
		}

		var list scansListResponse
		decodeErr := json.NewDecoder(resp.Body).Decode(&list)
		statusCode := resp.StatusCode
		resp.Body.Close()

		if statusCode != http.StatusOK {
			return "", unexpectedStatusCode(statusCode)
		}
		if decodeErr != nil {
			return "", fmt.Errorf("%w: %w", domain.ErrInternalError, decodeErr)
		}

		if len(list.Data) > 0 && list.Data[0].UUID == scanUUID {
			return status, nil
		}
	}
	return domain.ScanStatusUnknown, nil
}

// --- /v1/defect-groups (severity counts) ------------------------------------------------------

type paginatedDefectGroups struct {
	TotalSize int64 `json:"totalSize"`
}

// OpenCount returns the number of OPEN-status defect groups at the given severity tier, read
// from totalSize on a pageSize=1 call — never from the list response's own int32 severity/status
// fields (see the int32 landmine section of appsec-api.md).
func (c *Client) OpenCount(ctx context.Context, token, gitRepo string, severity domain.Severity) (int64, error) {
	params := url.Values{
		"gitRepo":  {gitRepo},
		"pageSize": {"1"},
		"severity": {string(severity)},
		"status":   {"OPEN"},
	}
	req, err := c.newRequest(ctx, token, "/v1/defect-groups", params)
	if err != nil {
		return 0, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", domain.ErrExternalAPI, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return 0, domain.ErrNoAccess
	}
	if resp.StatusCode != http.StatusOK {
		return 0, unexpectedStatus(resp)
	}

	var page paginatedDefectGroups
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return 0, fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	return page.TotalSize, nil
}

// --- SARIF report generate / poll / download ----------------------------------------------------

type reportOperationDTO struct {
	OperationUUID string  `json:"operationUuid"`
	Status        string  `json:"status"`
	Done          bool    `json:"done"`
	ReportUUID    *string `json:"reportUuid"`
}

// GenerateReport starts (or reuses an in-flight/cached) SARIF report generation for a repo's
// latest scan and returns the operation to poll.
func (c *Client) GenerateReport(ctx context.Context, token, gitRepo string) (operationUUID string, err error) {
	params := url.Values{
		"gitRepo":    {gitRepo},
		"format":     {"SARIF"},
		"engineType": {"UNSPECIFIED"},
	}
	req, err := c.newRequestMethod(ctx, http.MethodPost, token, "/v1/reports/generate", params)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %w", domain.ErrExternalAPI, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return "", domain.ErrNoAccess
	}
	if resp.StatusCode != http.StatusOK {
		return "", unexpectedStatus(resp)
	}

	var op reportOperationDTO
	if err := json.NewDecoder(resp.Body).Decode(&op); err != nil {
		return "", fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	return op.OperationUUID, nil
}

// PollOperation checks a report-generation operation once, without sleeping — the caller drives
// the retry loop so it can respect the caller's own context deadline.
func (c *Client) PollOperation(ctx context.Context, token, gitRepo, operationUUID string) (done bool, failed bool, err error) {
	params := url.Values{"gitRepo": {gitRepo}}
	req, err := c.newRequest(ctx, token, "/v1/reports/operations/"+operationUUID, params)
	if err != nil {
		return false, false, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, false, fmt.Errorf("%w: %w", domain.ErrExternalAPI, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, false, unexpectedStatus(resp)
	}

	var op reportOperationDTO
	if err := json.NewDecoder(resp.Body).Decode(&op); err != nil {
		return false, false, fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	return op.Done, op.Status == "FAILED", nil
}

type reportDTO struct {
	PresignedURL string `json:"presignedUrl"`
}

// FetchReport resolves the presigned URL for a completed report and downloads the raw SARIF
// bytes. The caller is expected to parse and discard them — this client never persists them.
func (c *Client) FetchReport(ctx context.Context, token, gitRepo string) ([]byte, error) {
	params := url.Values{
		"gitRepo":    {gitRepo},
		"format":     {"SARIF"},
		"engineType": {"UNSPECIFIED"},
	}
	req, err := c.newRequest(ctx, token, "/v1/reports/presigned-url", params)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrExternalAPI, err)
	}
	var report reportDTO
	decodeErr := json.NewDecoder(resp.Body).Decode(&report)
	statusCode := resp.StatusCode
	resp.Body.Close()

	if statusCode != http.StatusOK {
		return nil, unexpectedStatusCode(statusCode)
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrInternalError, decodeErr)
	}

	// Deliberately NOT sending our AppSec bearer token to the presigned URL — it's already
	// authenticated via its own signature, per appsec-api.md's explicit warning.
	dlReq, err := http.NewRequestWithContext(ctx, http.MethodGet, report.PresignedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	dlResp, err := c.httpClient.Do(dlReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrExternalAPI, err)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != http.StatusOK {
		return nil, unexpectedStatus(dlResp)
	}
	body, err := io.ReadAll(dlResp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	return body, nil
}

// --- helpers -------------------------------------------------------------------------------

func (c *Client) newRequest(ctx context.Context, token, path string, params url.Values) (*http.Request, error) {
	return c.newRequestMethod(ctx, http.MethodGet, token, path, params)
}

func (c *Client) newRequestMethod(ctx context.Context, method, token, path string, params url.Values) (*http.Request, error) {
	u := c.baseURL + path + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return req, nil
}

func unexpectedStatus(resp *http.Response) error {
	return fmt.Errorf("%w: AppSec API returned status %s", domain.ErrExternalAPI, resp.Status)
}

func unexpectedStatusCode(code int) error {
	return fmt.Errorf("%w: AppSec API returned status %d", domain.ErrExternalAPI, code)
}

func epochMillisPtr(ms *int64) *time.Time {
	if ms == nil {
		return nil
	}
	t := time.UnixMilli(*ms)
	return &t
}
