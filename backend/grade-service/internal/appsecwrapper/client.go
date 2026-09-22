// Package appsecwrapper is a gRPC client for security-wrapper-service. The wrapper holds no
// credentials itself: every call must carry the caller's SourceCraft PAT as an "authorization"
// gRPC metadata value, which it forwards to the AppSec API per-request.
package appsecwrapper

import (
	"context"
	"fmt"
	"time"

	"repo-therapist/grade-service/internal/appsecwrapper/gen"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

type Client struct {
	conn *grpc.ClientConn
	rpc  gen.SecurityWrapperClient
	pat  string
}

// Dial connects to security-wrapper-service at addr (e.g. "localhost:8084"). pat is the
// SourceCraft PAT attached to every RPC made through this client.
func Dial(addr, pat string) (*Client, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dialing security-wrapper-service at %s: %w", addr, err)
	}
	return &Client{conn: conn, rpc: gen.NewSecurityWrapperClient(conn), pat: pat}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) authed(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.pat)
}

// ScanSummary is the subset of the wrapper's ScanSummary message Security scoring needs.
type ScanSummary struct {
	UUID              string
	Status            string
	ScanType          string
	CommitHash        string
	TimeFinished      time.Time
	TotalDefectGroups int64
	OpenDefectGroups  int64
}

// SeverityCounts is open defect-group counts per severity tier, filter-based per
// agents_memory/topics/appsec-api.md's int32 landmine — never the raw int fields.
type SeverityCounts struct {
	Critical int64
	High     int64
	Medium   int64
	Low      int64
}

// AppsecResult is Security scoring's whole input. HasScan=false means the repo has never been
// scanned — every other field is zero-valued and must not be treated as a real 0.
type AppsecResult struct {
	HasScan bool
	Scan    ScanSummary
	Open    SeverityCounts
}

// GetAppsecResult fetches AppSec scan/defect data for one repo. gitRepo is the repo's SourceCraft
// UUID (or internal numeric-string ID) — same value as security-wrapper-service's own gitRepo param.
func (c *Client) GetAppsecResult(ctx context.Context, gitRepo string) (AppsecResult, error) {
	resp, err := c.rpc.GetAppsecResult(c.authed(ctx), &gen.GetAppsecResultRequest{GitRepo: gitRepo})
	if err != nil {
		return AppsecResult{}, err
	}
	if !resp.GetHasScan() {
		return AppsecResult{}, nil
	}

	scan := resp.GetScan()
	counts := resp.GetOpenCounts()
	return AppsecResult{
		HasScan: true,
		Scan: ScanSummary{
			UUID:              scan.GetUuid(),
			Status:            scan.GetStatus(),
			ScanType:          scan.GetScanType(),
			CommitHash:        scan.GetCommitHash(),
			TimeFinished:      scan.GetTimeFinished().AsTime(),
			TotalDefectGroups: scan.GetTotalDefectGroups(),
			OpenDefectGroups:  scan.GetOpenDefectGroups(),
		},
		Open: SeverityCounts{
			Critical: counts.GetCritical(),
			High:     counts.GetHigh(),
			Medium:   counts.GetMedium(),
			Low:      counts.GetLow(),
		},
	}, nil
}
