package config

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Recommendation priorities, most urgent first. A recommendation carries exactly one.
const (
	PriorityCritical = "critical"
	PriorityHigh     = "high"
	PriorityMedium   = "medium"
	PriorityLow      = "low"
)

// Priorities lists the valid priority values, most urgent first.
var Priorities = []string{PriorityCritical, PriorityHigh, PriorityMedium, PriorityLow}

// ValidPriority reports whether p is one of Priorities.
func ValidPriority(p string) bool { return slices.Contains(Priorities, p) }

// RecommendationEntry is one catalog entry: the recommendation's priority and its Russian text
// (without a "[priority]" tag).
type RecommendationEntry struct {
	Priority string `yaml:"priority"`
	Text     string `yaml:"text"`
}

// LoadRecommendations reads the metric-ID -> {priority, text} catalog
// (config/recommendations.yaml) used by scoring.BuildRecommendations. A missing file is not an
// error — recommendations are best-effort; grading itself must not fail just because the catalog
// isn't there yet (e.g. before it's been written for every metric). A present file with an entry
// lacking text or with an unknown priority is an error, so a typo stops the service at startup.
func LoadRecommendations(path string) (map[string]RecommendationEntry, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]RecommendationEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var catalog map[string]RecommendationEntry
	if err := yaml.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if catalog == nil {
		catalog = map[string]RecommendationEntry{}
	}
	for id, entry := range catalog {
		entry.Text = strings.TrimSpace(entry.Text)
		if entry.Text == "" {
			return nil, fmt.Errorf("%s: %s has no text", path, id)
		}
		if !ValidPriority(entry.Priority) {
			return nil, fmt.Errorf("%s: %s has priority %q, want one of %s", path, id, entry.Priority, strings.Join(Priorities, ", "))
		}
		catalog[id] = entry
	}
	return catalog, nil
}
