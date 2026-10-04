package service

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"sourcecraft-wrapper-service/internal/domain"
)

func TestGetPullRequestsAsksForNewestFirstAndCapsLimit(t *testing.T) {
	var query map[string][]string
	scw := newTestWrapper(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/id:repo-1/pulls" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		query = r.URL.Query()
		fmt.Fprint(w, `{"pull_requests":[
			{"id":"a","slug":"3","status":"merged","author":{"id":"u1","slug":"alice"},"created_at":"2026-06-15T20:52:34Z","updated_at":"2026-06-15T21:00:00Z"},
			{"id":"b","slug":"2","status":"open","author":{"id":"u2","slug":"bob"},"created_at":"2026-06-14T20:52:34Z","updated_at":"2026-06-14T21:00:00Z"},
			{"id":"c","slug":"1","status":"merged","author":{"id":"u1","slug":"alice"},"created_at":"2026-06-13T20:52:34Z","updated_at":"2026-06-13T21:00:00Z"}]}`)
	})

	got, err := scw.GetPullRequests(authCtx(), "repo-1", 2)
	if err != nil {
		t.Fatal(err)
	}

	if query["sort_by"][0] != "-created_at" || query["page_size"][0] != "2" {
		t.Errorf("query = %v, want sort_by=-created_at page_size=2", query)
	}
	if len(got.PullRequests) != 2 || got.PullRequests[0].Slug != "3" || got.PullRequests[0].Author.Slug != "alice" || got.PullRequests[1].Status != "open" {
		t.Errorf("got %+v, want the first two pull requests", got.PullRequests)
	}

	// Zero and oversized limits fall back to the page-size cap.
	for _, limit := range []int{0, 500} {
		if _, err := scw.GetPullRequests(authCtx(), "repo-1", limit); err != nil {
			t.Fatal(err)
		}
		if query["page_size"][0] != "100" {
			t.Errorf("limit %d: page_size = %s, want 100", limit, query["page_size"][0])
		}
	}
}

func TestGetPullRequestsMapsNotFoundAndNoToken(t *testing.T) {
	scw := newTestWrapper(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })

	if _, err := scw.GetPullRequests(authCtx(), "missing", 10); !errors.Is(err, domain.ErrRepositoryNotFound) {
		t.Errorf("err = %v, want ErrRepositoryNotFound", err)
	}
	if _, err := scw.GetPullRequests(t.Context(), "repo-1", 10); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("err = %v, want ErrUnauthenticated", err)
	}
}

func TestGetPullRequestActivityKeepsMetadataOnly(t *testing.T) {
	scw := newTestWrapper(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pulls/id:pr-1/reviewers":
			fmt.Fprint(w, `{"reviewers":[{"user":{"id":"u2","slug":"bob"},"review_decision":"approve","created_at":"2026-06-15T21:00:00Z"}]}`)
		case "/pulls/id:pr-1/comments":
			fmt.Fprint(w, `{"pull_request_comments":[{"id":"c1","body":"secret text","author":{"id":"u3","slug":"carol"},"created_at":"2026-06-15T22:00:00Z","type":"appsec","is_deleted":false,"is_published":true}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got, err := scw.GetPullRequestActivity(authCtx(), "pr-1")
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Reviewers) != 1 || got.Reviewers[0].User.Slug != "bob" || got.Reviewers[0].ReviewDecision != "approve" {
		t.Errorf("reviewers = %+v", got.Reviewers)
	}
	if len(got.Comments) != 1 || got.Comments[0].Author.Slug != "carol" || got.Comments[0].Type != "appsec" || !got.Comments[0].IsPublished {
		t.Errorf("comments = %+v", got.Comments)
	}
}

func TestGetPullRequestActivityFailsWhenEitherCallFails(t *testing.T) {
	scw := newTestWrapper(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/pulls/id:pr-1/reviewers" {
			fmt.Fprint(w, `{"reviewers":[]}`)
			return
		}
		w.WriteHeader(http.StatusBadGateway)
	})

	if _, err := scw.GetPullRequestActivity(authCtx(), "pr-1"); !errors.Is(err, domain.ErrExternalAPI) {
		t.Errorf("err = %v, want ErrExternalAPI", err)
	}
}
