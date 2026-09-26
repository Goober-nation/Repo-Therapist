package service

import (
	"context"
	"errors"
	"fmt"
	"repo-therapist/security-wrapper-service/internal/domain"
	"repo-therapist/security-wrapper-service/internal/sarif"
	"strings"
	"time"

	"google.golang.org/grpc/metadata"
)

// appsecClient is the subset of appsecclient.Client that the service depends on, so it can be
// faked in tests without a real HTTP round trip.
type appsecClient interface {
	GetLatestScan(ctx context.Context, token, gitRepo string) (*domain.ScanSummary, error)
	OpenCount(ctx context.Context, token, gitRepo string, severity domain.Severity) (int64, error)
	GenerateReport(ctx context.Context, token, gitRepo string) (string, error)
	PollOperation(ctx context.Context, token, gitRepo, operationUUID string) (done bool, failed bool, err error)
	FetchReport(ctx context.Context, token, gitRepo string) ([]byte, error)
}

type SecurityService interface {
	GetAppsecResult(ctx context.Context, gitRepo string) (domain.AppsecResult, error)
}

type Wrapper struct {
	client       appsecClient
	pollInterval time.Duration
	pollTimeout  time.Duration
}

func NewWrapper(client appsecClient, pollInterval, pollTimeout time.Duration) *Wrapper {
	return &Wrapper{client: client, pollInterval: pollInterval, pollTimeout: pollTimeout}
}

// GetAppsecResult is the whole v1 flow: latest scan -> per-severity OPEN counts -> generate/poll/
// fetch/parse the SARIF report for human-readable summaries. The report bytes never leave this
// function's stack — sarif.Parse consumes them and nothing keeps a reference afterward.
func (w *Wrapper) GetAppsecResult(ctx context.Context, gitRepo string) (domain.AppsecResult, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return domain.AppsecResult{}, fmt.Errorf("%w: %w", domain.ErrUnauthenticated, err)
	}

	scan, err := w.client.GetLatestScan(ctx, token, gitRepo)
	if err != nil {
		return domain.AppsecResult{}, err
	}
	if scan == nil {
		return domain.AppsecResult{HasScan: false}, nil
	}

	counts, err := w.severityCounts(ctx, token, gitRepo)
	if err != nil {
		return domain.AppsecResult{}, err
	}

	summaries, err := w.fetchAndParseReport(ctx, token, gitRepo)
	if err != nil {
		return domain.AppsecResult{}, err
	}

	return domain.AppsecResult{
		HasScan:      true,
		Scan:         *scan,
		OpenCounts:   counts,
		DefectGroups: summaries,
	}, nil
}

func (w *Wrapper) severityCounts(ctx context.Context, token, gitRepo string) (domain.SeverityCounts, error) {
	var counts domain.SeverityCounts
	for _, severity := range domain.OpenSeverities {
		count, err := w.client.OpenCount(ctx, token, gitRepo, severity)
		if err != nil {
			return domain.SeverityCounts{}, err
		}
		switch severity {
		case domain.SeverityCritical:
			counts.Critical = count
		case domain.SeverityHigh:
			counts.High = count
		case domain.SeverityMedium:
			counts.Medium = count
		case domain.SeverityLow:
			counts.Low = count
		}
	}
	return counts, nil
}

func (w *Wrapper) fetchAndParseReport(ctx context.Context, token, gitRepo string) ([]domain.DefectGroupSummary, error) {
	operationUUID, err := w.client.GenerateReport(ctx, token, gitRepo)
	if err != nil {
		return nil, err
	}

	deadline := time.Now().Add(w.pollTimeout)
	for {
		done, failed, err := w.client.PollOperation(ctx, token, gitRepo, operationUUID)
		if err != nil {
			return nil, err
		}
		if failed {
			return nil, domain.ErrReportFailed
		}
		if done {
			break
		}
		if time.Now().After(deadline) {
			return nil, domain.ErrReportTimeout
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(w.pollInterval):
		}
	}

	raw, err := w.client.FetchReport(ctx, token, gitRepo)
	if err != nil {
		return nil, err
	}

	summaries, err := sarif.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	return summaries, nil
	// raw goes out of scope here — nothing keeps a copy of the report past this point.
}

var errMissingToken = errors.New("missing authorization token in gRPC metadata")

func tokenFromContext(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", errMissingToken
	}

	values := md.Get("authorization")
	if len(values) == 0 {
		return "", errMissingToken
	}

	token := strings.TrimSpace(values[0])
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimPrefix(token, "bearer ")
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errMissingToken
	}
	return token, nil
}
