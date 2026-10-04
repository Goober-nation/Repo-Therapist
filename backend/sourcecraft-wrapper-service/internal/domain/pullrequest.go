package domain

import "time"

type PullRequestUser struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

type PullRequest struct {
	ID        string          `json:"id"`
	Slug      string          `json:"slug"`
	Status    string          `json:"status"`
	Author    PullRequestUser `json:"author"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type PullRequests struct {
	PullRequests []PullRequest `json:"pull_requests"`
}

type PullRequestReviewer struct {
	User           PullRequestUser `json:"user"`
	ReviewDecision string          `json:"review_decision"`
	CreatedAt      time.Time       `json:"created_at"`
}

type PullRequestComment struct {
	Author      PullRequestUser `json:"author"`
	CreatedAt   time.Time       `json:"created_at"`
	Type        string          `json:"type"`
	IsDeleted   bool            `json:"is_deleted"`
	IsPublished bool            `json:"is_published"`
}

// PullRequestActivity is the first page of one pull request's reviewers and comments.
type PullRequestActivity struct {
	Reviewers []PullRequestReviewer
	Comments  []PullRequestComment
}
