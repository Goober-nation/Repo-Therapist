package issuedata

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"repo-therapist/repository-service/internal/scwrapper"
)

type fakeFetcher struct {
	failFor map[string]bool
	asked   []string
	calls   atomic.Int32
}

func (f *fakeFetcher) GetIssueComments(_ context.Context, id string) ([]scwrapper.IssueComment, error) {
	f.calls.Add(1)
	if f.failFor[id] {
		return nil, errors.New("boom")
	}

	return []scwrapper.IssueComment{{Author: "bob", CreatedAt: "2026-10-01T10:00:00Z"}}, nil
}

func sample(n int) []scwrapper.Issue {
	issues := make([]scwrapper.Issue, n)
	for i := range issues {
		issues[i] = scwrapper.Issue{
			ID: fmt.Sprintf("id%02d", i), Slug: fmt.Sprint(i + 1), StatusType: "initial", AuthorSlug: "alice",
			CreatedAt: fmt.Sprintf("2026-09-%02dT10:00:00Z", i+1), UpdatedAt: "2026-10-01T00:00:00Z",
		}
	}

	return issues
}

func TestCollectTakesTheNewestIssuesAndTheirComments(t *testing.T) {
	f := &fakeFetcher{}
	export := Collect(context.Background(), f, sample(12), 5)

	if !export.Available || export.SampleLimit != 5 || len(export.Issues) != 5 || f.calls.Load() != 5 {
		t.Fatalf("export = %+v, calls = %d, want 5 issues and 5 calls", export, f.calls.Load())
	}

	// Newest first: slugs 12, 11, 10, 9, 8.
	for i, want := range []string{"12", "11", "10", "9", "8"} {
		got := export.Issues[i]
		if got.Slug != want || got.Author != "alice" || !got.RepliesAvailable || len(got.Comments) != 1 || got.Comments[0].Author != "bob" {
			t.Errorf("issue %d = %+v, want slug %s with bob's comment", i, got, want)
		}
	}
}

func TestCollectMarksOnlyTheFailedIssueUnavailable(t *testing.T) {
	f := &fakeFetcher{failFor: map[string]bool{"id02": true}}
	export := Collect(context.Background(), f, sample(4), 10)

	if len(export.Issues) != 4 {
		t.Fatalf("issues = %d, want 4 (fewer than the limit)", len(export.Issues))
	}

	for _, issue := range export.Issues {
		failed := issue.Slug == "3"
		if issue.RepliesAvailable == failed || (failed && issue.Comments == nil) {
			t.Errorf("issue %s: available=%v comments=%v, want unavailable only for slug 3 and a non-nil list", issue.Slug, issue.RepliesAvailable, issue.Comments)
		}
	}
}

func TestCollectOffAndEmpty(t *testing.T) {
	f := &fakeFetcher{}

	off := Collect(context.Background(), f, sample(3), 0)
	if off.Available || len(off.Issues) != 0 || off.Issues == nil || f.calls.Load() != 0 {
		t.Errorf("limit 0 = %+v, calls %d, want unavailable, an empty non-nil list and no calls", off, f.calls.Load())
	}

	none := Collect(context.Background(), f, nil, 10)
	if !none.Available || len(none.Issues) != 0 || none.Issues == nil {
		t.Errorf("no issues = %+v, want available with an empty list (readable, genuinely none)", none)
	}
}

func TestCollectDoesNotReorderTheCallersSlice(t *testing.T) {
	issues := sample(3)
	Collect(context.Background(), &fakeFetcher{}, issues, 10)

	if issues[0].Slug != "1" || issues[2].Slug != "3" {
		t.Errorf("the input slice was reordered: %+v", issues)
	}
}
