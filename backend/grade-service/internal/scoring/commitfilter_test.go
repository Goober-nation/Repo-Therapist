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

func TestFilterCommitsMinChangedLines(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	past := now.Add(-24 * time.Hour)
	commit := func(hash string, parents []string, files ...gitlog.FileChange) gitlog.Commit {
		return gitlog.Commit{Hash: hash, AuthorName: "Ann", AuthorEmail: "ann@x.dev", AuthoredAt: past, Parents: parents, FilesChanged: files}
	}
	commits := []gitlog.Commit{
		commit("one-line", []string{"p"}, gitlog.FileChange{Path: "a", Added: 1}),
		commit("two-lines", []string{"p"}, gitlog.FileChange{Path: "a", Added: 1, Deleted: 1}),
		commit("three-lines-over-two-files", []string{"p"}, gitlog.FileChange{Path: "a", Added: 1}, gitlog.FileChange{Path: "b", Added: 1, Deleted: 1}),
		commit("binary-only", []string{"p"}, gitlog.FileChange{Path: "logo.png", Added: -1, Deleted: -1, Binary: true}),
		commit("binary-plus-one-line", []string{"p"}, gitlog.FileChange{Path: "a", Added: 1}, gitlog.FileChange{Path: "logo.png", Added: -1, Deleted: -1, Binary: true}),
		commit("merge", []string{"a", "b"}),
		commit("empty", []string{"p"}),
		commit("legacy-no-parents-one-line", nil, gitlog.FileChange{Path: "a", Added: 1}),
	}

	dropped := func(floor int) map[string]bool {
		kept := map[string]bool{}
		for _, c := range FilterCommits(commits, config.CommitFilters{MinChangedLines: floor}, now) {
			kept[c.Hash] = true
		}
		out := map[string]bool{}
		for _, c := range commits {
			if !kept[c.Hash] {
				out[c.Hash] = true
			}
		}
		return out
	}

	if got := dropped(0); len(got) != 0 {
		t.Errorf("floor 0 (off) dropped %v", got)
	}

	two := dropped(2)
	if len(two) != 2 || !two["one-line"] || !two["legacy-no-parents-one-line"] {
		t.Errorf("floor 2 dropped %v, want exactly the one-line commits", two)
	}

	three := dropped(3)
	for _, h := range []string{"one-line", "two-lines", "legacy-no-parents-one-line"} {
		if !three[h] {
			t.Errorf("floor 3 kept %q", h)
		}
	}
	for _, h := range []string{"three-lines-over-two-files", "binary-only", "binary-plus-one-line", "merge", "empty"} {
		if three[h] {
			t.Errorf("floor 3 dropped %q: lines of all files are summed, a binary change is substantial, and merges/empty commits are other filters' business", h)
		}
	}
}
