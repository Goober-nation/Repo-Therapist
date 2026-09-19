// Package scoring implements the Repo Health Score grading policy (docs/grading-policy.md)
// against config/grading.yaml. Every metric is layered: a `source` (which S3 _meta/*.json file),
// an `aggregation` (raw timestamped events -> one number), and a `formula` (that number -> a 0-1
// score) — each independently swappable, which is what lets a future formula or aggregation type
// (e.g. time-decay) be added without touching existing metric definitions.
package scoring

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
}

type CategoryConfig struct {
	Weight  float64        `yaml:"weight"`
	Public  bool           `yaml:"public"`
	Status  string         `yaml:"status"`
	Metrics []MetricConfig `yaml:"metrics"`
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
