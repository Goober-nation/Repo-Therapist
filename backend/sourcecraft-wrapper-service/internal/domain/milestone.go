package domain

import "time"

type Milestones struct {
	Items         []Milestone `json:"items"`
	NextPageToken string      `json:"next_page_token"`
}

type Milestone struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Slug        string        `json:"slug"`
	Description string        `json:"description"`
	StartDate   time.Time     `json:"start_date"`
	Deadline    time.Time     `json:"deadline"`
	Status      string        `json:"status"`
	Author      MilestoneUser `json:"author"`
	UpdatedBy   MilestoneUser `json:"updated_by"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

type MilestoneUser struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}
