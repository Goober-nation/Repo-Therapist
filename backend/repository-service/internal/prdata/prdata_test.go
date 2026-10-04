package prdata

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"repo-therapist/repository-service/internal/scwrapper"
)

type fakeFetcher struct {
	prs      []scwrapper.PullRequest
	listErr  error
	failFor  map[string]bool
	gotLimit int
	calls    atomic.Int32
}

func (f *fakeFetcher) GetPullRequests(_ context.Context, _ string, limit int) ([]scwrapper.PullRequest, error) {
	f.gotLimit = limit
	return f.prs, f.listErr
}

func (f *fakeFetcher) GetPullRequestActivity(_ context.Context, id string) (scwrapper.PullRequestActivity, error) {
	f.calls.Add(1)
	if f.failFor[id] {
		return scwrapper.PullRequestActivity{}, errors.New("boom")
	}
	return scwrapper.PullRequestActivity{
		Reviewers: []scwrapper.PullRequestReviewer{{User: scwrapper.PullRequestUser{Slug: "bob"}, Decision: "approve"}},
		Comments:  []scwrapper.PullRequestComment{{Author: scwrapper.PullRequestUser{Slug: "carol"}, Type: "default"}},
	}, nil
}

func samplePRs(n int) []scwrapper.PullRequest {
	prs := make([]scwrapper.PullRequest, n)
	for i := range prs {
		prs[i] = scwrapper.PullRequest{ID: string(rune('a' + i)), Slug: string(rune('1' + i)), Status: "merged", Author: scwrapper.PullRequestUser{Slug: "alice"}}
	}
	return prs
}

func TestCollectKeepsOrderAndActivity(t *testing.T) {
	f := &fakeFetcher{prs: samplePRs(6)}
	export := Collect(context.Background(), f, "repo", 10)

	if !export.Available || export.SampleLimit != 10 || len(export.PullRequests) != 6 {
		t.Fatalf("export = %+v", export)
	}
	for i, pr := range export.PullRequests {
		if pr.Slug != string(rune('1'+i)) || !pr.ActivityAvailable || pr.Author != "alice" || len(pr.Reviewers) != 1 || len(pr.Comments) != 1 {
			t.Errorf("pr %d = %+v", i, pr)
		}
	}
	if f.calls.Load() != 6 {
		t.Errorf("activity calls = %d, want 6", f.calls.Load())
	}
}

func TestCollectMarksFailedActivityUnavailable(t *testing.T) {
	f := &fakeFetcher{prs: samplePRs(3), failFor: map[string]bool{"b": true}}
	export := Collect(context.Background(), f, "repo", 10)

	if !export.Available {
		t.Fatal("export should stay available when one activity fetch fails")
	}
	if export.PullRequests[1].ActivityAvailable || len(export.PullRequests[1].Reviewers) != 0 {
		t.Errorf("failed pr = %+v, want unavailable and empty", export.PullRequests[1])
	}
	if !export.PullRequests[0].ActivityAvailable || !export.PullRequests[2].ActivityAvailable {
		t.Error("other pull requests must keep their activity")
	}
}

func TestCollectListFailureOrZeroLimitIsUnavailable(t *testing.T) {
	failing := &fakeFetcher{listErr: errors.New("403")}
	if export := Collect(context.Background(), failing, "repo", 10); export.Available || len(export.PullRequests) != 0 {
		t.Errorf("list failure: %+v", export)
	}

	off := &fakeFetcher{prs: samplePRs(2)}
	if export := Collect(context.Background(), off, "repo", 0); export.Available || off.calls.Load() != 0 {
		t.Errorf("limit 0 must not fetch: %+v", export)
	}
}

func TestCollectTrimsToLimit(t *testing.T) {
	f := &fakeFetcher{prs: samplePRs(5)}
	if export := Collect(context.Background(), f, "repo", 3); len(export.PullRequests) != 3 || f.gotLimit != 3 {
		t.Errorf("export = %d prs, requested limit %d", len(export.PullRequests), f.gotLimit)
	}
}
