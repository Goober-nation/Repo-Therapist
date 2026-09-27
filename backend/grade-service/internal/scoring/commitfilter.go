package scoring

import (
	"strings"
	"time"

	"repo-therapist/grade-service/internal/config"
	"repo-therapist/grade-service/internal/gitlog"
)

// botSuffixes mark automated accounts by author name or email local part (e.g.
// "dependabot[bot]", "renovate-bot", "ci_bot"). Deliberately narrow: a human whose name merely
// contains "bot" must not be dropped.
var botSuffixes = []string{"[bot]", "-bot", "_bot"}

// FilterCommits returns the commits the enabled filters keep (docs/grading/activity.md):
//   - FutureDated: authored after now — a wrong clock or a forged date, not real recent activity;
//   - Merges: 2+ parents;
//   - Empty: a non-merge commit that changed no files. Needs Parents: plain `git log --numstat`
//     also lists no files for merges, so a commit with unknown parents (nil, old export) is kept;
//   - Bots: author name or email local part ends with a bot suffix.
//
// The input slice is not modified.
func FilterCommits(commits []gitlog.Commit, f config.CommitFilters, now time.Time) []gitlog.Commit {
	if !f.FutureDated && !f.Empty && !f.Bots && !f.Merges {
		return commits
	}
	kept := make([]gitlog.Commit, 0, len(commits))
	for _, c := range commits {
		switch {
		case f.FutureDated && c.AuthoredAt.After(now):
		case f.Merges && len(c.Parents) > 1:
		case f.Empty && c.Parents != nil && len(c.Parents) <= 1 && len(c.FilesChanged) == 0:
		case f.Bots && isBotAuthor(c.AuthorName, c.AuthorEmail):
		default:
			kept = append(kept, c)
		}
	}
	return kept
}

func isBotAuthor(name, email string) bool {
	local, _, _ := strings.Cut(email, "@")
	for _, s := range []string{strings.ToLower(strings.TrimSpace(name)), strings.ToLower(local)} {
		for _, suffix := range botSuffixes {
			if strings.HasSuffix(s, suffix) {
				return true
			}
		}
	}
	return false
}
