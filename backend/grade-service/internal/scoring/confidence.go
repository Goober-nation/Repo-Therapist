package scoring

import (
	"strings"
	"time"

	"repo-therapist/grade-service/internal/config"
	"repo-therapist/grade-service/internal/gitlog"
)

// ConfidenceReason is one pre-written, generic reason a grade is marked low-confidence. Text is
// deliberately vague about thresholds so it does not tell anyone how to game the flag.
type ConfidenceReason struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// Confidence says whether a public grade rests on little data. It never changes the score.
type Confidence struct {
	LowConfidence bool               `json:"low_confidence"`
	Reasons       []ConfidenceReason `json:"reasons"`
}

// ConfidenceInputs are the repo-level facts the code and history signals read. An unavailable
// source (the export could not be read) skips its signal: no data is not a defect.
type ConfidenceInputs struct {
	CodeAvailable bool
	ScannedFiles  int
	TotalLOC      int

	CommitsAvailable bool
	// Commits are the commits that survived the Activity commit filters (empty, bot, future-dated),
	// so padding the history with them does not make a repo look established.
	Commits []gitlog.Commit
}

// ComputeConfidence marks a public grade low-confidence when any signal fires:
//   - little code: fewer files or lines than the configured floors;
//   - little history: few kept commits in the window, or a single author below a higher bar;
//   - little coverage: fewer than the configured number of categoryIDs have data.
//
// It returns nil when the flag is disabled.
func ComputeConfidence(cfg config.ConfidenceConfig, in ConfidenceInputs, categories map[string]CategoryResult, categoryIDs []string, now time.Time) *Confidence {
	if !cfg.Enabled {
		return nil
	}

	reasons := []ConfidenceReason{}
	add := func(code string) {
		reasons = append(reasons, ConfidenceReason{Code: code, Text: cfg.Reasons[code]})
	}

	if in.CodeAvailable && (in.ScannedFiles < cfg.MinFiles || in.TotalLOC < cfg.MinLOC) {
		add(config.ReasonLowCode)
	}

	if in.CommitsAvailable && littleHistory(cfg, in.Commits, now) {
		add(config.ReasonLowHistory)
	}

	withData := 0
	for _, id := range categoryIDs {
		if cat, ok := categories[id]; ok && !cat.NoData {
			withData++
		}
	}
	if withData < cfg.MinCategoriesWithData {
		add(config.ReasonLowCoverage)
	}

	return &Confidence{LowConfidence: len(reasons) > 0, Reasons: reasons}
}

func littleHistory(cfg config.ConfidenceConfig, commits []gitlog.Commit, now time.Time) bool {
	cutoff := now.AddDate(0, 0, -cfg.WindowDays)
	authors := map[string]struct{}{}
	count := 0
	for _, c := range commits {
		if c.AuthoredAt.Before(cutoff) {
			continue
		}
		count++
		author := strings.ToLower(strings.TrimSpace(c.AuthorEmail))
		if author == "" {
			author = strings.ToLower(strings.TrimSpace(c.AuthorName))
		}
		authors[author] = struct{}{}
	}

	return count < cfg.MinCommits || (len(authors) <= 1 && count < cfg.MinCommitsSingleAuthor)
}
