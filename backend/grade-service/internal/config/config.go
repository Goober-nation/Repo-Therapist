// Package config defines and loads the Repo Health Score grading policy schema
// (docs/grading/00-overview.md, config/grading.yaml). Every metric is layered: a `source` (which S3
// _meta/*.json file), an `aggregation` (raw timestamped events -> one number), and a `formula`
// (that number -> a 0-1 score) — each independently swappable, which is what lets a future
// formula or aggregation type (e.g. time-decay) be added without touching existing metric
// definitions. The scoring package (internal/scoring) computes against these types.
package config

import (
	"fmt"
	"math"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	StatusCalculated    = "calculated"
	StatusNotCalculated = "not_calculated"
)

type Config struct {
	Version    int                       `yaml:"version"`
	Categories map[string]CategoryConfig `yaml:"categories"`
	Confidence ConfidenceConfig          `yaml:"confidence"`
}

// Low-confidence reason codes; ConfidenceConfig.Reasons must have a text for each.
const (
	ReasonLowCode     = "LOW_CODE"
	ReasonLowHistory  = "LOW_HISTORY"
	ReasonLowCoverage = "LOW_COVERAGE"
)

// ConfidenceConfig drives the low-confidence flag of public grades (internal/scoring/confidence.go).
// The flag never changes a score. Absent or enabled:false turns the flag off entirely.
type ConfidenceConfig struct {
	Enabled bool `yaml:"enabled"`
	// WindowDays is how far back commits count toward the history signal.
	WindowDays int `yaml:"window_days"`
	// MinFiles / MinLOC: fewer scanned files or lines than either -> little code.
	MinFiles int `yaml:"min_files"`
	MinLOC   int `yaml:"min_loc"`
	// MinCommits: fewer kept commits in the window -> little history. MinCommitsSingleAuthor is the
	// same bar when every one of them has the same author.
	MinCommits             int `yaml:"min_commits"`
	MinCommitsSingleAuthor int `yaml:"min_commits_single_author"`
	// MinCategoriesWithData: fewer public categories with data than this -> little coverage.
	MinCategoriesWithData int `yaml:"min_categories_with_data"`
	// Reasons maps a reason code to its pre-written, deliberately generic Russian text.
	Reasons map[string]string `yaml:"reasons"`
}

type CategoryConfig struct {
	Weight  float64        `yaml:"weight"`
	Public  bool           `yaml:"public"`
	Status  string         `yaml:"status"`
	Metrics []MetricConfig `yaml:"metrics"`
	// CommitFilters drops commits before any commit-sourced metric sees them. Only Activity sets
	// it (docs/grading/activity.md); absent means no filtering.
	CommitFilters CommitFilters `yaml:"commit_filters"`
	// IssueFilters drops issues and milestones that look manufactured before any issue-sourced
	// metric sees them; absent means no filtering.
	IssueFilters IssueFilters `yaml:"issue_filters"`
}

// IssueFilters toggles which issues and milestones are excluded from issues.json- and
// milestones.json-sourced metrics (docs/grading/00-overview.md, "Устойчивость к накрутке").
type IssueFilters struct {
	// InstantCloseSeconds drops issues that were closed within this many seconds of being created
	// (0 = off): an issue opened and closed in the same moment is not work that was tracked.
	InstantCloseSeconds int `yaml:"instant_close_seconds"`
	// MilestonesNeedIssues counts a milestone only if an issue is attached to it.
	MilestonesNeedIssues bool `yaml:"milestones_need_issues"`
}

// CommitFilters toggles which commits are excluded from commits.json-sourced metrics.
type CommitFilters struct {
	FutureDated bool `yaml:"future_dated"`
	Empty       bool `yaml:"empty"`
	Bots        bool `yaml:"bots"`
	Merges      bool `yaml:"merges"`
	// MinChangedLines drops non-merge commits that changed fewer lines than this (added +
	// deleted, binary files count as substantial; 0 = off), so one-line filler commits do not
	// count as activity.
	MinChangedLines int `yaml:"min_changed_lines"`
}

type MetricConfig struct {
	ID          string            `yaml:"id"`
	Status      string            `yaml:"status"`
	Weight      float64           `yaml:"weight"`
	Source      string            `yaml:"source"`
	Aggregation AggregationConfig `yaml:"aggregation"`
	Formula     FormulaConfig     `yaml:"formula"`
}

type AggregationConfig struct {
	Type          string `yaml:"type"`
	WindowDays    int    `yaml:"window_days"`
	Field         string `yaml:"field"`
	MinSampleSize int    `yaml:"min_sample_size"`
	// StalenessDays is abandoned_share's "untouched for N days" threshold — distinct from
	// WindowDays (which issues to consider at all).
	StalenessDays int `yaml:"staleness_days"`
}

type FormulaConfig struct {
	Type         string  `yaml:"type"`
	Direction    string  `yaml:"direction"`
	Critical     float64 `yaml:"critical"`
	Healthy      float64 `yaml:"healthy"`
	HealthyLow   float64 `yaml:"healthy_low"`
	HealthyHigh  float64 `yaml:"healthy_high"`
	CriticalLow  float64 `yaml:"critical_low"`
	CriticalHigh float64 `yaml:"critical_high"`
	Provisional  bool    `yaml:"provisional"`
}

// LoadConfig reads and validates config/grading.yaml. Validation catches the exact class of bug
// the unified formula was designed to prevent — e.g. a "-" direction metric with healthy >
// critical (backwards) — at load time, not silently at scoring time.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("validating %s: %w", path, err)
	}
	return &cfg, nil
}

func validate(cfg *Config) error {
	if err := validateConfidence(cfg.Confidence); err != nil {
		return fmt.Errorf("confidence: %w", err)
	}
	for catID, cat := range cfg.Categories {
		if cat.Status != StatusCalculated {
			continue
		}
		var weightSum float64
		for _, m := range cat.Metrics {
			if err := validateFormula(m.Formula); err != nil {
				return fmt.Errorf("category %q metric %q: %w", catID, m.ID, err)
			}
			if m.Status == StatusCalculated {
				weightSum += m.Weight
			}
		}
		if len(cat.Metrics) > 0 && math.Abs(weightSum-1.0) > 0.01 {
			return fmt.Errorf("category %q: calculated metrics' weights sum to %.3f, want ~1.0", catID, weightSum)
		}
	}
	return nil
}

func validateFormula(f FormulaConfig) error {
	switch f.Type {
	case "linear_clamp":
		switch f.Direction {
		case "+":
			if f.Healthy <= f.Critical {
				return fmt.Errorf("direction \"+\" needs healthy (%v) > critical (%v)", f.Healthy, f.Critical)
			}
		case "-":
			if f.Healthy >= f.Critical {
				return fmt.Errorf("direction \"-\" needs healthy (%v) < critical (%v)", f.Healthy, f.Critical)
			}
		default:
			return fmt.Errorf("linear_clamp needs direction \"+\" or \"-\", got %q", f.Direction)
		}
	case "band":
		if !(f.CriticalLow < f.HealthyLow && f.HealthyLow <= f.HealthyHigh && f.HealthyHigh < f.CriticalHigh) {
			return fmt.Errorf("band needs critical_low < healthy_low <= healthy_high < critical_high")
		}
	case "gate", "":
		// gate: no numeric bounds to validate. "": category/metric with no metrics list yet
		// (not_calculated categories skip this function entirely, see caller).
	default:
		return fmt.Errorf("unknown formula type %q", f.Type)
	}
	return nil
}

func validateConfidence(c ConfidenceConfig) error {
	if !c.Enabled {
		return nil
	}
	if c.WindowDays <= 0 {
		return fmt.Errorf("window_days must be positive, got %d", c.WindowDays)
	}
	for name, value := range map[string]int{
		"min_files": c.MinFiles, "min_loc": c.MinLOC, "min_commits": c.MinCommits,
		"min_commits_single_author": c.MinCommitsSingleAuthor, "min_categories_with_data": c.MinCategoriesWithData,
	} {
		if value < 0 {
			return fmt.Errorf("%s must not be negative, got %d", name, value)
		}
	}
	for _, code := range []string{ReasonLowCode, ReasonLowHistory, ReasonLowCoverage} {
		if c.Reasons[code] == "" {
			return fmt.Errorf("reasons: no text for %s", code)
		}
	}
	return nil
}
