package domain

import (
	"time"

	"github.com/google/uuid"
)

type Repo struct {
	SourcecraftUUID uuid.UUID `db:"sourcecraft_uuid" json:"sourcecraft_uuid"`

	OrgSlug  string `db:"org_slug" json:"org_slug"`
	RepoSlug string `db:"repo_slug" json:"repo_slug"`

	Name        string  `db:"name" json:"name"`
	Description *string `db:"description" json:"description"`
	WebURL      string  `db:"web_url" json:"web_url"`

	DefaultBranch string `db:"default_branch" json:"default_branch"`

	Visibility string `db:"visibility" json:"visibility"`

	PrimaryLanguage *string `db:"primary_language" json:"primary_language"`

	RatingValue      *float64 `db:"rating_value" json:"rating_value"`
	RatingPercentile *float64 `db:"rating_percentile" json:"rating_percentile"`
	RatingReactions  []byte   `db:"rating_reactions" json:"rating_reactions"`

	Counters []byte `db:"counters" json:"counters"`

	LastCommitAt   *time.Time `db:"last_commit_at" json:"last_commit_at"`
	LastActivityAt *time.Time `db:"last_activity_at" json:"last_activity_at"`
}

type RepoLock struct {
	SourcecraftUUID uuid.UUID `db:"sourcecraft_uuid" json:"sourcecraft_uuid"`

	Status          string    `db:"status" json:"status"`
	StatusUpdatedAt time.Time `db:"status_updated_at" json:"status_updated_at"`
}

type RepoGrade struct {
	SourcecraftUUID uuid.UUID `db:"sourcecraft_uuid" json:"sourcecraft_uuid"`

	PublicScore           *int16 `db:"public_score" json:"public_score"`
	PublicCategoryScores  []byte `db:"public_category_scores" json:"public_category_scores"`
	PublicRecommendations []byte `db:"public_recommendations" json:"public_recommendations"`

	PrivateScore           *int16 `db:"private_score" json:"private_score"`
	PrivateCategoryScores  []byte `db:"private_category_scores" json:"private_category_scores"`
	PrivateRecommendations []byte `db:"private_recommendations" json:"private_recommendations"`

	MissingData []string `db:"missing_data" json:"missing_data"`

	GradedAt *time.Time `db:"graded_at" json:"graded_at"`
}

type RepoAppSec struct {
	SourcecraftUUID uuid.UUID `db:"sourcecraft_uuid" json:"sourcecraft_uuid"`

	DataTier string `db:"data_tier" json:"data_tier"`

	ScanUUID   *string `db:"scan_uuid" json:"scan_uuid"`
	ScanStatus *string `db:"scan_status" json:"scan_status"`
	ScanType   *string `db:"scan_type" json:"scan_type"`

	CommitHash *string `db:"commit_hash" json:"commit_hash"`

	TimeStarted  *time.Time `db:"time_started" json:"time_started"`
	TimeFinished *time.Time `db:"time_finished" json:"time_finished"`

	TotalDefectGroups *int `db:"total_defect_groups" json:"total_defect_groups"`
	FoundDefectGroups *int `db:"found_defect_groups" json:"found_defect_groups"`
	OpenDefectGroups  *int `db:"open_defect_groups" json:"open_defect_groups"`

	OpenCriticalCount *int `db:"open_critical_count" json:"open_critical_count"`
	OpenHighCount     *int `db:"open_high_count" json:"open_high_count"`
	OpenMediumCount   *int `db:"open_medium_count" json:"open_medium_count"`
	OpenLowCount      *int `db:"open_low_count" json:"open_low_count"`

	DefectGroups []byte `db:"defect_groups" json:"defect_groups"`

	Score *int16 `db:"score" json:"score"`

	CheckedAt *time.Time `db:"checked_at" json:"checked_at"`
}

type RepoCI struct {
	SourcecraftUUID uuid.UUID `db:"sourcecraft_uuid" json:"sourcecraft_uuid"`

	DataTier   string `db:"data_tier" json:"data_tier"`
	DataSource string `db:"data_source" json:"data_source"`

	HasCI *bool `db:"has_ci" json:"has_ci"`

	SampledPRCount *int `db:"sampled_pr_count" json:"sampled_pr_count"`

	SuccessCount *int `db:"success_count" json:"success_count"`
	FailureCount *int `db:"failure_count" json:"failure_count"`

	Workflows []byte `db:"workflows" json:"workflows"`

	AvgDurationSeconds *float64 `db:"avg_duration_seconds" json:"avg_duration_seconds"`

	Score *int16 `db:"score" json:"score"`

	CheckedAt *time.Time `db:"checked_at" json:"checked_at"`
}

type RepoLLMResponsePublic struct {
	SourcecraftUUID uuid.UUID `db:"sourcecraft_uuid" json:"sourcecraft_uuid"`

	RawResponse []byte `db:"raw_response" json:"raw_response"`

	ModelUsed *string `db:"model_used" json:"model_used"`

	EvaluatedAt *time.Time `db:"evaluated_at" json:"evaluated_at"`
}

type RepoLLMResponsePrivate struct {
	SourcecraftUUID uuid.UUID `db:"sourcecraft_uuid" json:"sourcecraft_uuid"`

	RawResponse []byte `db:"raw_response" json:"raw_response"`

	ModelUsed *string `db:"model_used" json:"model_used"`

	EvaluatedAt *time.Time `db:"evaluated_at" json:"evaluated_at"`
}
