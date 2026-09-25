package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadRecommendations reads the flat metric-ID -> recommendation-text catalog
// (config/recommendations.yaml) used by scoring.BuildRecommendations. A missing file is not an
// error — recommendations are best-effort; grading itself must not fail just because the catalog
// isn't there yet (e.g. before it's been written for every metric).
func LoadRecommendations(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var catalog map[string]string
	if err := yaml.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return catalog, nil
}
