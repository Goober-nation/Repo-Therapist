package service

import (
	"context"
	"errors"
	"repo-therapist/security-wrapper-service/internal/domain"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
)

type fakeClient struct {
	scan          *domain.ScanSummary
	getScanErr    error
	counts        map[domain.Severity]int64
	openCountErr  error
	operationUUID string
	generateErr   error
	pollResponses []pollResponse
	pollErr       error
	report        []byte
	fetchErr      error
}

type pollResponse struct {
	done   bool
	failed bool
}

func (f *fakeClient) GetLatestScan(ctx context.Context, token, gitRepo string) (*domain.ScanSummary, error) {
	return f.scan, f.getScanErr
}

func (f *fakeClient) OpenCount(ctx context.Context, token, gitRepo string, severity domain.Severity) (int64, error) {
	if f.openCountErr != nil {
		return 0, f.openCountErr
	}
	return f.counts[severity], nil
}

func (f *fakeClient) GenerateReport(ctx context.Context, token, gitRepo string) (string, error) {
	return f.operationUUID, f.generateErr
}

func (f *fakeClient) PollOperation(ctx context.Context, token, gitRepo, operationUUID string) (bool, bool, error) {
	if f.pollErr != nil {
		return false, false, f.pollErr
	}
	if len(f.pollResponses) == 0 {
		// Exhausted the scripted responses: keep reporting "still running" so timeout tests
		// don't accidentally fall through to a real report fetch.
		return false, false, nil
	}
	r := f.pollResponses[0]
	f.pollResponses = f.pollResponses[1:]
	return r.done, r.failed, nil
}

func (f *fakeClient) FetchReport(ctx context.Context, token, gitRepo string) ([]byte, error) {
	return f.report, f.fetchErr
}

func ctxWithToken() context.Context {
	md := metadata.New(map[string]string{"authorization": "Bearer test-token"})
	return metadata.NewIncomingContext(context.Background(), md)
}

func TestGetAppsecResult_NoScan(t *testing.T) {
	w := NewWrapper(&fakeClient{scan: nil}, time.Millisecond, time.Second)
	result, err := w.GetAppsecResult(ctxWithToken(), "repo-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.HasScan {
		t.Fatalf("expected HasScan=false, got true: %+v", result)
	}
}

func TestGetAppsecResult_MissingToken(t *testing.T) {
	w := NewWrapper(&fakeClient{}, time.Millisecond, time.Second)
	_, err := w.GetAppsecResult(context.Background(), "repo-uuid")
	if !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("expected ErrUnauthenticated, got %v", err)
	}
}

func TestGetAppsecResult_HappyPath(t *testing.T) {
	scan := &domain.ScanSummary{UUID: "scan-1", Status: domain.ScanStatusFinished}
	client := &fakeClient{
		scan: scan,
		counts: map[domain.Severity]int64{
			domain.SeverityCritical: 2,
			domain.SeverityHigh:     5,
			domain.SeverityMedium:   1,
			domain.SeverityLow:      0,
		},
		operationUUID: "op-1",
		pollResponses: []pollResponse{{done: false}, {done: true}},
		report: []byte(`{"runs":[{"tool":{"driver":{"rules":[]}},"results":[
			{"ruleId":"r1","message":{"text":"m"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"a.go"},"region":{"startLine":1,"endLine":1}}}]}
		]}]}`),
	}
	w := NewWrapper(client, time.Millisecond, time.Second)

	result, err := w.GetAppsecResult(ctxWithToken(), "repo-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasScan {
		t.Fatal("expected HasScan=true")
	}
	if result.OpenCounts.Critical != 2 || result.OpenCounts.High != 5 {
		t.Errorf("unexpected counts: %+v", result.OpenCounts)
	}
	if len(result.DefectGroups) != 1 || result.DefectGroups[0].File != "a.go" {
		t.Errorf("unexpected defect groups: %+v", result.DefectGroups)
	}
}

func TestGetAppsecResult_ReportFailed(t *testing.T) {
	scan := &domain.ScanSummary{UUID: "scan-1"}
	client := &fakeClient{
		scan:          scan,
		counts:        map[domain.Severity]int64{},
		operationUUID: "op-1",
		pollResponses: []pollResponse{{failed: true}},
	}
	w := NewWrapper(client, time.Millisecond, time.Second)

	_, err := w.GetAppsecResult(ctxWithToken(), "repo-uuid")
	if !errors.Is(err, domain.ErrReportFailed) {
		t.Fatalf("expected ErrReportFailed, got %v", err)
	}
}

func TestGetAppsecResult_ReportTimeout(t *testing.T) {
	scan := &domain.ScanSummary{UUID: "scan-1"}
	client := &fakeClient{
		scan:          scan,
		counts:        map[domain.Severity]int64{},
		operationUUID: "op-1",
		pollResponses: []pollResponse{{done: false}, {done: false}, {done: false}, {done: false}, {done: false}},
	}
	w := NewWrapper(client, time.Millisecond, 5*time.Millisecond)

	_, err := w.GetAppsecResult(ctxWithToken(), "repo-uuid")
	if !errors.Is(err, domain.ErrReportTimeout) {
		t.Fatalf("expected ErrReportTimeout, got %v", err)
	}
}

func TestGetAppsecResult_NoAccess(t *testing.T) {
	w := NewWrapper(&fakeClient{getScanErr: domain.ErrNoAccess}, time.Millisecond, time.Second)
	_, err := w.GetAppsecResult(ctxWithToken(), "repo-uuid")
	if !errors.Is(err, domain.ErrNoAccess) {
		t.Fatalf("expected ErrNoAccess, got %v", err)
	}
}
