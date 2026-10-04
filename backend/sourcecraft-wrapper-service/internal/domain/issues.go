package domain

import "time"

type Issues struct {
	Issues        []Issue `json:"issues"`
	NextPageToken string  `json:"next_page_token"`
}

type Issue struct {
	ID          string          `json:"id"`
	Slug        string          `json:"slug"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Status      IssueStatus     `json:"status"`
	Author      IssueUser       `json:"author"`
	UpdatedBy   IssueUser       `json:"updated_by"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Assignee    *IssueUser      `json:"assignee"`
	Labels      []Label         `json:"labels"`
	LinkedPRs   []LinkedPR      `json:"linked_prs"`
	Priority    string          `json:"priority"`
	Visibility  string          `json:"visibility"`
	Milestone   *IssueMilestone `json:"milestone"`
	Deadline    *time.Time      `json:"deadline"`
	StartedAt   *time.Time      `json:"started_at"`
	CompletedAt *time.Time      `json:"completed_at"`
	Repository  RepositoryRef   `json:"repository"`
}

type IssueStatus struct {
	ID         string `json:"id"`
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	StatusType string `json:"status_type"`
}

type IssueUser struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

type Label struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type LinkedPR struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

type IssueMilestone struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

type RepositoryRef struct {
	ID           string       `json:"id"`
	Slug         string       `json:"slug"`
	Organization Organization `json:"organization"`
}

type Organization struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

// IssueComment is one comment of an issue; only who and when are read, never the text.
type IssueComment struct {
	Author    IssueUser `json:"author"`
	CreatedAt time.Time `json:"created_at"`
}
