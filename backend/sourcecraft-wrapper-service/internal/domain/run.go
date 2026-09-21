package domain

import "time"

type Runs struct {
	Runs          []Run  `json:"runs"`
	NextPageToken string `json:"next_page_token"`
}

type Run struct {
	ID            string     `json:"id"`
	Slug          string     `json:"slug"`
	Dates         Dates      `json:"dates"`
	Status        string     `json:"status"`
	Workflows     []Workflow `json:"workflows"`
	EventType     string     `json:"event_type"`
	ErrorMessages []string   `json:"error_messages"`
	Pull          *Pull      `json:"pull"`
	User          *RunUser   `json:"user"`
}

type Workflow struct {
	ID            string   `json:"id"`
	Slug          string   `json:"slug"`
	Description   string   `json:"description"`
	Dates         Dates    `json:"dates"`
	Status        string   `json:"status"`
	Tasks         []Task   `json:"tasks"`
	Progress      Progress `json:"progress"`
	EnvironmentID string   `json:"environment_id"`
}

type Task struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	Dates       Dates     `json:"dates"`
	Status      string    `json:"status"`
	Cubes       []Cube    `json:"cubes"`
	Progress    Progress  `json:"progress"`
	Relations   Relations `json:"relations"`
}

type Cube struct {
	ID        string      `json:"id"`
	Slug      string      `json:"slug"`
	Dates     Dates       `json:"dates"`
	Status    string      `json:"status"`
	Artifacts []*Artifact `json:"artifacts"`
	Relations Relations   `json:"relations"`
}

type Progress struct {
	Percent     float64 `json:"percent"`
	CurrentCube *Cube   `json:"current_cube"`
}

type Artifact struct {
	ID          string `json:"id"`
	LocalPath   string `json:"local_path"`
	Dates       Dates  `json:"dates"`
	Status      string `json:"status"`
	DownloadURL string `json:"download_url"`
}

type Relations struct {
	Needs []Relation `json:"needs"`
}

type Relation struct {
	ID   string `json:"id,omitempty"`
	Slug string `json:"slug,omitempty"`
}

type Dates struct {
	CreatedAt  *time.Time `json:"created_at"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	UpdatedAt  *time.Time `json:"updated_at"`
}

type Pull struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

type RunUser struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}
