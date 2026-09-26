// Package cidata builds the _meta/ci-runs.json export CI/CD grading reads
// (docs/grading/ci-cd.md): which CI config files exist in the clone, plus a windowed, capped
// sample of the repo's CI runs from sourcecraft-wrapper-service.
package cidata

import (
	"os"
	"path/filepath"
	"sort"
	"time"

	"repo-therapist/repository-service/internal/scwrapper"
)

// ConfigPaths are the repo-relative CI config locations checked in the clone. SourceCraft's CI/CD
// reference documents exactly one: .sourcecraft/ci.yaml.
var ConfigPaths = []string{".sourcecraft/ci.yaml"}

const (
	// WindowDays / MaxRuns: runs created within the last 180 days, newest 25 at most (decided
	// 2026-09-26, see docs/grading/ci-cd.md).
	WindowDays = 180
	MaxRuns    = 25
)

// Export is the _meta/ci-runs.json shape. RunsAvailable=false means the runs fetch failed (most
// commonly: the token doesn't own the repo) — Runs is then empty and means nothing, which is
// distinct from RunsAvailable=true with an empty Runs (readable, genuinely zero runs).
type Export struct {
	ConfigFiles   []string          `json:"config_files"`
	RunsAvailable bool              `json:"runs_available"`
	Runs          []scwrapper.CIRun `json:"runs"`
}

// DetectConfigs returns which of ConfigPaths exist as regular files under dir.
func DetectConfigs(dir string) []string {
	found := []string{}
	for _, p := range ConfigPaths {
		if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err == nil && info.Mode().IsRegular() {
			found = append(found, p)
		}
	}
	return found
}

// SelectRuns keeps runs created within windowDays of now, newest first, at most maxRuns. Runs
// with an unparseable/missing created_at are dropped — they can't be placed in the window.
func SelectRuns(runs []scwrapper.CIRun, now time.Time, windowDays, maxRuns int) []scwrapper.CIRun {
	cutoff := now.AddDate(0, 0, -windowDays)
	type dated struct {
		run scwrapper.CIRun
		at  time.Time
	}
	kept := make([]dated, 0, len(runs))
	for _, r := range runs {
		at, err := time.Parse(time.RFC3339Nano, r.CreatedAt)
		if err != nil || at.Before(cutoff) {
			continue
		}
		kept = append(kept, dated{r, at})
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].at.After(kept[j].at) })
	if len(kept) > maxRuns {
		kept = kept[:maxRuns]
	}
	out := make([]scwrapper.CIRun, len(kept))
	for i, d := range kept {
		out[i] = d.run
	}
	return out
}
