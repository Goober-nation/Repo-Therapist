package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Repo struct {
	SourcecraftUUID uuid.UUID `db:"sourcecraft_uuid" json:"sourcecraft_uuid"`

	OrgSlug  string `db:"org_slug" json:"org_slug"`
	RepoSlug string `db:"repo_slug" json:"repo_slug"`
	WebURL   string `json:"web_url"`

	Visibility     string     `db:"visibility" json:"visibility"`
	LastActivityAt *time.Time `db:"last_activity_at" json:"last_activity_at"`

	RatingValue float64         `json:"rating"`
	Language    PrimaryLanguage `json:"language"`
}

type PrimaryLanguage struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}
type RepoLock struct {
	SourcecraftUUID uuid.UUID `db:"sourcecraft_uuid"`

	Status          string    `db:"status"`
	StatusUpdatedAt time.Time `db:"status_updated_at"`
}

type RepoGrade struct {
	SourcecraftUUID uuid.UUID `db:"sourcecraft_uuid"`

	Score          *int16          `db:"score"`
	CategoryScores json.RawMessage `db:"category_scores"`
	Advice         json.RawMessage `db:"advice"`

	MissingData []string `db:"missing_data"`

	GradedAt *time.Time `db:"graded_at"`
}

type RepoAppSec struct {
	SourcecraftUUID uuid.UUID `db:"sourcecraft_uuid"`

	DataTier string `db:"data_tier"`

	ScanUUID   *string `db:"scan_uuid"`
	ScanStatus *string `db:"scan_status"`
	ScanType   *string `db:"scan_type"`

	CommitHash *string `db:"commit_hash"`

	TimeStarted  *time.Time `db:"time_started"`
	TimeFinished *time.Time `db:"time_finished"`

	TotalDefectGroups *int `db:"total_defect_groups"`
	FoundDefectGroups *int `db:"found_defect_groups"`
	OpenDefectGroups  *int `db:"open_defect_groups"`

	OpenCriticalCount *int `db:"open_critical_count"`
	OpenHighCount     *int `db:"open_high_count"`
	OpenMediumCount   *int `db:"open_medium_count"`
	OpenLowCount      *int `db:"open_low_count"`

	DefectGroups json.RawMessage `db:"defect_groups"`

	Score *int16 `db:"score"`

	CheckedAt *time.Time `db:"checked_at"`
}

type RepoCI struct {
	SourcecraftUUID uuid.UUID `db:"sourcecraft_uuid"`

	DataTier   string `db:"data_tier"`
	DataSource string `db:"data_source"`

	HasCI *bool `db:"has_ci"`

	SampledPRCount *int `db:"sampled_pr_count"`

	SuccessCount *int `db:"success_count"`
	FailureCount *int `db:"failure_count"`

	Workflows json.RawMessage `db:"workflows"`

	AvgDurationSeconds *float64 `db:"avg_duration_seconds"`

	Score *int16 `db:"score"`

	CheckedAt *time.Time `db:"checked_at"`
}

type RepoLLMResponse struct {
	SourcecraftUUID uuid.UUID
	RawResponse     json.RawMessage
	ModelUsed       string
	EvaluatedAt     *time.Time
	// GradedAt is graded_at of the grade the response describes; nil for rows stored before
	// it was recorded.
	GradedAt *time.Time
}
type RepoLLMResponsePublic struct {
	SourcecraftUUID uuid.UUID `db:"sourcecraft_uuid"`

	RawResponse json.RawMessage `db:"raw_response"`

	ModelUsed string `db:"model_used"`

	EvaluatedAt *time.Time `db:"evaluated_at"`

	GradedAt *time.Time `db:"graded_at"`
}

type RepoLLMResponsePrivate struct {
	SourcecraftUUID uuid.UUID `db:"sourcecraft_uuid"`

	RawResponse json.RawMessage `db:"raw_response"`

	ModelUsed string `db:"model_used"`

	EvaluatedAt *time.Time `db:"evaluated_at"`

	GradedAt *time.Time `db:"graded_at"`
}
