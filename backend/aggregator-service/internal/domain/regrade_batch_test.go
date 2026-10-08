package domain

import (
	"errors"
	"testing"
)

func intp(v int) *int { return &v }

func TestBatchFilterValidate(t *testing.T) {
	cases := []struct {
		name   string
		filter BatchFilter
		ok     bool
	}{
		{"empty", BatchFilter{}, false},
		{"all", BatchFilter{All: true}, true},
		{"all with criteria", BatchFilter{All: true, Languages: []string{"go"}}, false},
		{"language", BatchFilter{Languages: []string{"go"}}, true},
		{"score range", BatchFilter{MinScore: intp(10), MaxScore: intp(60)}, true},
		{"inverted range", BatchFilter{MinScore: intp(70), MaxScore: intp(20)}, false},
		{"score out of range", BatchFilter{MaxScore: intp(101)}, false},
		{"negative score", BatchFilter{MinScore: intp(-1)}, false},
		{"age", BatchFilter{OlderThanDays: intp(30)}, true},
		{"zero age", BatchFilter{OlderThanDays: intp(0)}, false},
		{"huge age", BatchFilter{OlderThanDays: intp(MaxBatchDays + 1)}, false},
		{"picked repo", BatchFilter{Repos: []string{"org/repo"}}, true},
		{"bad repo", BatchFilter{Repos: []string{"just-a-name"}}, false},
		{"repo with query", BatchFilter{Repos: []string{"org/repo?x=1"}}, false},
	}

	for _, tc := range cases {
		err := tc.filter.Normalize().Validate()
		if tc.ok && err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
		}

		if !tc.ok && !errors.Is(err, ErrInvalidBatch) {
			t.Errorf("%s: want ErrInvalidBatch, got %v", tc.name, err)
		}
	}
}

func TestBatchFilterLimits(t *testing.T) {
	many := make([]string, MaxBatchRepos+1)
	for i := range many {
		many[i] = "org/repo" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}

	if err := (BatchFilter{Repos: many}).Normalize().Validate(); !errors.Is(err, ErrInvalidBatch) {
		t.Fatalf("too many repos: got %v", err)
	}

	langs := make([]string, MaxBatchLanguages+1)
	for i := range langs {
		langs[i] = "lang" + string(rune('a'+i))
	}

	if err := (BatchFilter{Languages: langs}).Normalize().Validate(); !errors.Is(err, ErrInvalidBatch) {
		t.Fatalf("too many languages: got %v", err)
	}
}

func TestBatchFilterNormalize(t *testing.T) {
	f := BatchFilter{Languages: []string{" Go ", "go", "", "Rust"}, Repos: []string{"a/b", " a/b ", "A/B"}}.Normalize()

	if len(f.Languages) != 2 || f.Languages[0] != "go" || f.Languages[1] != "rust" {
		t.Fatalf("languages: %v", f.Languages)
	}

	if len(f.Repos) != 1 || f.Repos[0] != "a/b" {
		t.Fatalf("repos: %v", f.Repos)
	}
}
