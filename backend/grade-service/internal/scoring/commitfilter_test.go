package scoring

import (
	"path/filepath"
	"testing"
	"time"

	"repo-therapist/grade-service/internal/config"
	"repo-therapist/grade-service/internal/gitlog"
)

func TestFilterCommits(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	past := now.Add(-24 * time.Hour)
	files := []gitlog.FileChange{{Path: "main.go", Added: 1}}
	commits := []gitlog.Commit{
		{Hash: "normal", AuthorName: "Ann", AuthorEmail: "ann@x.dev", AuthoredAt: past, Parents: []string{"p"}, FilesChanged: files},
		{Hash: "future", AuthorName: "Ann", AuthorEmail: "ann@x.dev", AuthoredAt: now.Add(time.Hour), Parents: []string{"p"}, FilesChanged: files},
		{Hash: "empty", AuthorName: "Ann", AuthorEmail: "ann@x.dev", AuthoredAt: past, Parents: []string{"p"}},
		{Hash: "empty-root", AuthorName: "Ann", AuthorEmail: "ann@x.dev", AuthoredAt: past, Parents: []string{}},
		{Hash: "merge", AuthorName: "Ann", AuthorEmail: "ann@x.dev", AuthoredAt: past, Parents: []string{"a", "b"}},
		{Hash: "legacy-no-parents", AuthorName: "Ann", AuthorEmail: "ann@x.dev", AuthoredAt: past},
		{Hash: "bot-name", AuthorName: "dependabot[bot]", AuthorEmail: "49699333+dependabot@users.noreply.x", AuthoredAt: past, Parents: []string{"p"}, FilesChanged: files},
		{Hash: "bot-email", AuthorName: "Renovate", AuthorEmail: "renovate-bot@x.dev", AuthoredAt: past, Parents: []string{"p"}, FilesChanged: files},
		{Hash: "human-robot", AuthorName: "Abbott", AuthorEmail: "robotics@x.dev", AuthoredAt: past, Parents: []string{"p"}, FilesChanged: files},
	}

	cases := []struct {
		name    string
		filters config.CommitFilters
		dropped []string
	}{
		{"all off", config.CommitFilters{}, nil},
		{"future only", config.CommitFilters{FutureDated: true}, []string{"future"}},
		// A merge has no numstat files either, so it must not be mistaken for empty; a legacy
		// export without parents is kept because emptiness can't be told apart from a merge.
		{"empty only", config.CommitFilters{Empty: true}, []string{"empty", "empty-root"}},
		{"merges only", config.CommitFilters{Merges: true}, []string{"merge"}},
		{"bots only", config.CommitFilters{Bots: true}, []string{"bot-name", "bot-email"}},
		{"shipped defaults", config.CommitFilters{FutureDated: true, Empty: true, Bots: true},
			[]string{"future", "empty", "empty-root", "bot-name", "bot-email"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kept := map[string]bool{}
			for _, c := range FilterCommits(commits, tc.filters, now) {
				kept[c.Hash] = true
			}
			dropped := map[string]bool{}
			for _, h := range tc.dropped {
				dropped[h] = true
			}
			for _, c := range commits {
				if kept[c.Hash] == dropped[c.Hash] {
					t.Errorf("commit %q: kept=%v, want dropped=%v", c.Hash, kept[c.Hash], dropped[c.Hash])
				}
			}
		})
	}
}

// The shipped config/grading.yaml turns on future/empty/bots and leaves merges off.
func TestShippedCommitFilterDefaults(t *testing.T) {
	cfg, err := config.LoadConfig(filepath.Join("..", "..", "..", "..", "config", "grading.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := config.CommitFilters{FutureDated: true, Empty: true, Bots: true, Merges: false}
	if got := cfg.Categories["activity"].CommitFilters; got != want {
		t.Errorf("activity commit_filters = %+v, want %+v", got, want)
	}
}
