//go:build manual

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"repo-therapist/grade-service/internal/config"
	"repo-therapist/grade-service/internal/gitlog"
	"repo-therapist/grade-service/internal/scoring"
)

// TestHardeningCalibrationManual scores real repositories' histories with and without the
// anti-gaming filters, to pick thresholds that stay inside the no-harm bound (docs/grading/
// 00-overview.md, "Устойчивость к накрутке"). It reads, from CALIB_DIR:
//
//	clones/<org>__<repo>/       shallow git clones (history of the last year)
//	exports/<org>__<repo>/      issues.json and milestones.json in repository-service's export shape
//	db_categories.json          {"<org>__<repo>": {"documentation": 40, "code_health": null, ...}}
//
// Documentation and Code health stay at their stored values; only Activity and Issues are
// recomputed, so a delta is exactly what the filters change. Run:
//
//	CALIB_DIR=<dir> go test -tags=manual ./internal/app/ -run TestHardeningCalibrationManual -v
func TestHardeningCalibrationManual(t *testing.T) {
	dir := os.Getenv("CALIB_DIR")
	if dir == "" {
		t.Skip("CALIB_DIR not set")
	}
	cfg := loadShippedConfig(t)
	now := time.Now()

	var stored map[string]map[string]*float64
	raw, err := os.ReadFile(filepath.Join(dir, "db_categories.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}

	type variant struct {
		name  string
		lines int
		a, e  bool
	}
	variants := []variant{
		{"A", 0, true, false}, {"E", 0, false, true},
		{"C2", 2, false, false}, {"C3", 3, false, false}, {"C4", 4, false, false}, {"C5", 5, false, false}, {"C8", 8, false, false},
		{"A+E+C2", 2, true, true}, {"A+E+C3", 3, true, true}, {"A+E+C4", 4, true, true}, {"A+E+C5", 5, true, true},
	}

	entries, err := os.ReadDir(filepath.Join(dir, "clones"))
	if err != nil {
		t.Fatal(err)
	}

	var lastIssues float64
	var lastIssuesData bool
	score := func(inputs scoring.ActivityInputs, others map[string]*float64, v *variant) (activity, overall float64) {
		c := *cfg
		c.Categories = map[string]config.CategoryConfig{}
		for k, cat := range cfg.Categories {
			c.Categories[k] = cat
		}
		for _, id := range []string{"activity", "issues"} {
			cat := c.Categories[id]
			// Baseline and variants start from "no anti-gaming filters", whatever the shipped
			// config says, so a delta is exactly what the variant's filters change.
			cat.CommitFilters.MinChangedLines = 0
			cat.IssueFilters = config.IssueFilters{}
			if v != nil {
				cat.CommitFilters.MinChangedLines = v.lines
				if v.a {
					cat.IssueFilters.InstantCloseSeconds = 30
				}
				if v.e {
					cat.IssueFilters.MilestonesNeedIssues = true
				}
			}
			c.Categories[id] = cat
		}
		cats := map[string]scoring.CategoryResult{
			"activity": scoring.ComputeCategory("activity", c.Categories["activity"], inputs, now),
			"issues":   scoring.ComputeCategory("issues", c.Categories["issues"], inputs, now),
		}
		for _, id := range []string{"documentation", "code_health"} {
			if s := others[id]; s != nil {
				cats[id] = scoring.CategoryResult{ID: id, Score: *s}
			} else {
				cats[id] = scoring.CategoryResult{ID: id, NoData: true}
			}
		}
		weights := map[string]float64{}
		for id, cat := range c.Categories {
			weights[id] = cat.Weight
		}
		o, _ := scoring.CombineCategories(cats, publicCategoryIDs, weights)
		lastIssues, lastIssuesData = cats["issues"].Score, !cats["issues"].NoData
		return cats["activity"].Score, o
	}

	var header strings.Builder
	fmt.Fprintf(&header, "%-44s %6s", "repo", "base")
	for _, v := range variants {
		fmt.Fprintf(&header, " %7s", v.name)
	}
	t.Log("\n(overall score change vs. baseline; baseline column is the recomputed overall)\n" + header.String())

	maxDelta := map[string]float64{}
	for _, e := range entries {
		name := e.Name()
		commits, err := gitlog.ExtractHistory(context.Background(), filepath.Join(dir, "clones", name), now.AddDate(0, 0, -365))
		if err != nil {
			t.Logf("%s: %v", name, err)
			continue
		}
		inputs := scoring.ActivityInputs{Commits: commits, CommitsAvailable: true}
		if data, err := os.ReadFile(filepath.Join(dir, "exports", name, "issues.json")); err == nil {
			if json.Unmarshal(data, &inputs.Issues) == nil {
				inputs.IssuesAvailable = true
			}
		}
		if data, err := os.ReadFile(filepath.Join(dir, "exports", name, "milestones.json")); err == nil {
			if json.Unmarshal(data, &inputs.Milestones) == nil {
				inputs.MilestonesAvailable = true
			}
		}

		baseActivity, base := score(inputs, stored[name], nil)
		baseIssues, baseIssuesData := lastIssues, lastIssuesData
		allActivity, _ := score(inputs, stored[name], &variants[len(variants)-2])
		t.Logf("  detail %-40s activity %3.0f -> %3.0f (A+E+C4), issues %3.0f(data=%v) -> %3.0f(data=%v)", strings.ReplaceAll(name, "__", "/"), baseActivity, allActivity, baseIssues, baseIssuesData, lastIssues, lastIssuesData)
		var row strings.Builder
		fmt.Fprintf(&row, "%-44s %6.1f", strings.ReplaceAll(name, "__", "/"), base)
		for i := range variants {
			_, o := score(inputs, stored[name], &variants[i])
			d := o - base
			fmt.Fprintf(&row, " %+7.1f", d)
			if abs(d) > maxDelta[variants[i].name] {
				maxDelta[variants[i].name] = abs(d)
			}
		}
		t.Log(row.String())
	}

	names := make([]string, 0, len(maxDelta))
	for _, v := range variants {
		names = append(names, v.name)
	}
	sort.Strings(names)
	var summary strings.Builder
	for _, v := range variants {
		fmt.Fprintf(&summary, " %s=%.1f", v.name, maxDelta[v.name])
	}
	t.Log("max |overall change| per variant:" + summary.String())
}
