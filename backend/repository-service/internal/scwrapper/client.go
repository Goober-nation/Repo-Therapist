// Package scwrapper is a gRPC client for sourcecraft-wrapper-service (see
// backend/sourcecraft-wrapper-service on the scwrapper-service branch — not yet merged to main).
// The wrapper holds no credentials itself: every call must carry the caller's SourceCraft PAT as
// an "authorization" gRPC metadata value, which it forwards to the SourceCraft API per-request.
package scwrapper

import (
	"context"
	"fmt"
	"time"

	"repo-therapist/repository-service/internal/scwrapper/gen"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Client struct {
	conn *grpc.ClientConn
	rpc  gen.CodeHealthClient
	pat  string
}

// Dial connects to sourcecraft-wrapper-service at addr (e.g. "localhost:8083"). pat is the
// SourceCraft PAT attached to every RPC made through this client.
func Dial(addr, pat string) (*Client, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dialing sourcecraft-wrapper-service at %s: %w", addr, err)
	}
	return &Client{conn: conn, rpc: gen.NewCodeHealthClient(conn), pat: pat}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) authed(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.pat)
}

// GetRepoID resolves an org/repo slug pair to the wrapper's opaque repo ID, used by every other
// call below.
func (c *Client) GetRepoID(ctx context.Context, orgSlug, repoSlug string) (string, error) {
	resp, err := c.rpc.GetRepoIDBySlugs(c.authed(ctx), &gen.GetRepoIDBySlugsRequest{
		OrgSlug:  orgSlug,
		RepoSlug: repoSlug,
	})
	if err != nil {
		return "", err
	}
	return resp.GetRepoId(), nil
}

// GitURLs is the pair of clone URLs the wrapper returns for a repo ID.
type GitURLs struct {
	HTTPURL string
	SSHURL  string
}

func (c *Client) GetGitURL(ctx context.Context, repoID string) (GitURLs, error) {
	resp, err := c.rpc.GetGitURL(c.authed(ctx), &gen.GetGitURLRequest{RepoId: repoID})
	if err != nil {
		return GitURLs{}, err
	}
	return GitURLs{HTTPURL: resp.GetHttpUrl(), SSHURL: resp.GetSshUrl()}, nil
}

// Issue is the subset of the wrapper's Issue message Activity's and Issues' metrics need.
// StatusType is the fixed backend lifecycle enum (initial/in_progress/paused/completed/
// cancelled — see sourcecraft-wrapper-service's IssueStatus.status_type), not the per-repo
// customizable Status slug/name — Issues category metrics key off StatusType since a repo's own
// slug/name for "closed" isn't guaranteed consistent across repos/orgs.
type Issue struct {
	ID         string `json:"id"`
	Slug       string `json:"slug"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	StatusType string `json:"status_type"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// GetIssues fetches every issue for a repo, following the wrapper's page tokens.
func (c *Client) GetIssues(ctx context.Context, repoID string) ([]Issue, error) {
	resp, err := c.rpc.GetIssues(c.authed(ctx), &gen.GetIssuesRequest{RepoId: repoID})
	if err != nil {
		return nil, err
	}
	issues := make([]Issue, 0, len(resp.GetIssues()))
	for _, i := range resp.GetIssues() {
		issues = append(issues, Issue{
			ID:         i.GetId(),
			Slug:       i.GetSlug(),
			Title:      i.GetTitle(),
			Status:     i.GetStatus().GetSlug(),
			StatusType: i.GetStatus().GetStatusType(),
			CreatedAt:  timestampRFC3339(i.GetCreatedAt()),
			UpdatedAt:  timestampRFC3339(i.GetUpdatedAt()),
		})
	}
	return issues, nil
}

// Milestone is the subset of the wrapper's Milestone message Activity's release-cadence
// replacement metrics need.
type Milestone struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Deadline  string `json:"deadline"`
	CreatedAt string `json:"created_at"`
}

// GetMilestones fetches every milestone for a repo.
func (c *Client) GetMilestones(ctx context.Context, repoID string) ([]Milestone, error) {
	resp, err := c.rpc.GetMilestones(c.authed(ctx), &gen.GetMilestonesRequest{RepoId: repoID})
	if err != nil {
		return nil, err
	}
	milestones := make([]Milestone, 0, len(resp.GetItems()))
	for _, m := range resp.GetItems() {
		milestones = append(milestones, Milestone{
			ID:        m.GetId(),
			Slug:      m.GetSlug(),
			Name:      m.GetName(),
			Status:    m.GetStatus(),
			Deadline:  timestampRFC3339(m.GetDeadline()),
			CreatedAt: timestampRFC3339(m.GetCreatedAt()),
		})
	}
	return milestones, nil
}

func timestampRFC3339(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return ""
	}
	return ts.AsTime().Format(time.RFC3339Nano)
}
