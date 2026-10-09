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

// maxRecvMsgBytes raises gRPC's 4 MB default receive cap: a repo's full issue list comes back in
// one message, and large issue bodies pushed one real repo to 9.5 MB (2026-09-28), failing it
// outright on every attempt.
const maxRecvMsgBytes = 64 << 20

// Dial connects to sourcecraft-wrapper-service at addr (e.g. "localhost:8082"). pat is the
// SourceCraft PAT attached to every RPC made through this client.
func Dial(addr, pat string) (*Client, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxRecvMsgBytes)))
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
// call below. Goes through GetRepositoryBySlugs: the wrapper still declares GetRepoIDBySlugs in
// its proto but no longer implements it (answers Unimplemented).
func (c *Client) GetRepoID(ctx context.Context, orgSlug, repoSlug string) (string, error) {
	resp, err := c.rpc.GetRepositoryBySlugs(c.authed(ctx), &gen.GetRepositoryBySlugsRequest{
		OrgSlug:  orgSlug,
		RepoSlug: repoSlug,
	})
	if err != nil {
		return "", err
	}
	return resp.GetId(), nil
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
	// MilestoneID is the milestone the issue is attached to ("" when none); Grade service counts a
	// milestone only if an issue is attached to it (docs/grading/00-overview.md).
	MilestoneID string `json:"milestone_id,omitempty"`
	// AuthorSlug is who opened the issue; the first-reply metric ignores the author's own comments.
	AuthorSlug string `json:"author,omitempty"`
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

			MilestoneID: i.GetMilestone().GetId(),
			AuthorSlug:  i.GetAuthor().GetSlug(),
		})
	}
	return issues, nil
}

// IssueComment is who commented on an issue and when; the text is never fetched into the export.
type IssueComment struct {
	Author    string `json:"author"`
	CreatedAt string `json:"created_at"`
}

// GetIssueComments fetches the comments (first page) of one issue by its ID.
func (c *Client) GetIssueComments(ctx context.Context, issueID string) ([]IssueComment, error) {
	resp, err := c.rpc.GetIssueComments(c.authed(ctx), &gen.GetIssueCommentsRequest{IssueId: issueID})
	if err != nil {
		return nil, err
	}
	comments := make([]IssueComment, 0, len(resp.GetComments()))
	for _, m := range resp.GetComments() {
		comments = append(comments, IssueComment{Author: m.GetAuthor().GetSlug(), CreatedAt: timestampRFC3339(m.GetCreatedAt())})
	}
	return comments, nil
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

// CIRun is the subset of the wrapper's Run message CI/CD's metrics need (docs/grading/ci-cd.md).
// Status is SourceCraft's Run.Status enum (created/prepared/processing/success/failed/canceled/
// timeout/skipped/awaiting_approval/rejected).
type CIRun struct {
	Slug       string `json:"slug"`
	Status     string `json:"status"`
	EventType  string `json:"event_type"`
	CreatedAt  string `json:"created_at"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
}

// GetCICDRuns fetches the first page of a repo's CI runs, newest first. The underlying
// /cicd/runs endpoint only answers for a token with owner-level access — any error here
// (including that 403, which the wrapper currently surfaces as a generic Internal) means "runs
// unavailable", not "no runs".
func (c *Client) GetCICDRuns(ctx context.Context, repoID string) ([]CIRun, error) {
	resp, err := c.rpc.GetCiCDRuns(c.authed(ctx), &gen.GetCiCDRunsRequest{RepoId: repoID})
	if err != nil {
		return nil, err
	}
	runs := make([]CIRun, 0, len(resp.GetRuns()))
	for _, r := range resp.GetRuns() {
		d := r.GetDates()
		runs = append(runs, CIRun{
			Slug:       r.GetSlug(),
			Status:     r.GetStatus(),
			EventType:  r.GetEventType(),
			CreatedAt:  runTimestamp(d.GetCreatedAt()),
			StartedAt:  runTimestamp(d.GetStartedAt()),
			FinishedAt: runTimestamp(d.GetFinishedAt()),
		})
	}
	return runs, nil
}

// runTimestamp is timestampRFC3339 plus one CI-specific rule: SourceCraft reports a queued run's
// (status "prepared") started_at/finished_at as 1970-01-01T00:00:00Z rather than omitting them
// (seen live, 2026-09-26) — treated as absent so it can never be mistaken for a real time.
func runTimestamp(ts *timestamppb.Timestamp) string {
	if ts == nil || ts.AsTime().Unix() <= 0 {
		return ""
	}
	return timestampRFC3339(ts)
}

func timestampRFC3339(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return ""
	}
	return ts.AsTime().Format(time.RFC3339Nano)
}

// PullRequestUser identifies a pull request participant; only the slug is used for grading.
type PullRequestUser struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

// PullRequest is the subset of the wrapper's PullRequest message the review analytics needs.
// Status is SourceCraft's enum: draft, open, discarded, merging, merged.
type PullRequest struct {
	ID        string          `json:"id"`
	Slug      string          `json:"slug"`
	Status    string          `json:"status"`
	Author    PullRequestUser `json:"author"`
	CreatedAt string          `json:"created_at"`
	UpdatedAt string          `json:"updated_at"`
}

// PullRequestReviewer is one reviewer decision (approve, trust, block, abstain).
type PullRequestReviewer struct {
	User      PullRequestUser `json:"user"`
	Decision  string          `json:"decision"`
	CreatedAt string          `json:"created_at"`
}

// PullRequestComment is comment metadata only; the text is never fetched into the export.
type PullRequestComment struct {
	Author      PullRequestUser `json:"author"`
	CreatedAt   string          `json:"created_at"`
	Type        string          `json:"type"`
	IsDeleted   bool            `json:"is_deleted"`
	IsPublished bool            `json:"is_published"`
}

// PullRequestActivity is the first page of a pull request's reviewers and comments.
type PullRequestActivity struct {
	Reviewers []PullRequestReviewer
	Comments  []PullRequestComment
}

func pullRequestUser(u *gen.PullRequestUser) PullRequestUser {
	return PullRequestUser{ID: u.GetId(), Slug: u.GetSlug()}
}

// GetPullRequests fetches the newest limit pull requests of a repo (any status), newest first.
func (c *Client) GetPullRequests(ctx context.Context, repoID string, limit int) ([]PullRequest, error) {
	resp, err := c.rpc.GetPullRequests(c.authed(ctx), &gen.GetPullRequestsRequest{RepoId: repoID, Limit: int32(limit)})
	if err != nil {
		return nil, err
	}
	prs := make([]PullRequest, 0, len(resp.GetPullRequests()))
	for _, p := range resp.GetPullRequests() {
		prs = append(prs, PullRequest{
			ID:        p.GetId(),
			Slug:      p.GetSlug(),
			Status:    p.GetStatus(),
			Author:    pullRequestUser(p.GetAuthor()),
			CreatedAt: timestampRFC3339(p.GetCreatedAt()),
			UpdatedAt: timestampRFC3339(p.GetUpdatedAt()),
		})
	}
	return prs, nil
}

// GetPullRequestActivity fetches the reviewers and comments of one pull request by its ID.
func (c *Client) GetPullRequestActivity(ctx context.Context, pullRequestID string) (PullRequestActivity, error) {
	resp, err := c.rpc.GetPullRequestActivity(c.authed(ctx), &gen.GetPullRequestActivityRequest{PullRequestId: pullRequestID})
	if err != nil {
		return PullRequestActivity{}, err
	}
	activity := PullRequestActivity{
		Reviewers: make([]PullRequestReviewer, 0, len(resp.GetReviewers())),
		Comments:  make([]PullRequestComment, 0, len(resp.GetComments())),
	}
	for _, r := range resp.GetReviewers() {
		activity.Reviewers = append(activity.Reviewers, PullRequestReviewer{
			User:      pullRequestUser(r.GetUser()),
			Decision:  r.GetReviewDecision(),
			CreatedAt: timestampRFC3339(r.GetCreatedAt()),
		})
	}
	for _, m := range resp.GetComments() {
		activity.Comments = append(activity.Comments, PullRequestComment{
			Author:      pullRequestUser(m.GetAuthor()),
			CreatedAt:   timestampRFC3339(m.GetCreatedAt()),
			Type:        m.GetType(),
			IsDeleted:   m.GetIsDeleted(),
			IsPublished: m.GetIsPublished(),
		})
	}
	return activity, nil
}
