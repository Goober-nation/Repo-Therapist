package service

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"sourcecraft-wrapper-service/internal/domain"
)

func TestGetIssueCommentsKeepsAuthorAndTimeOnly(t *testing.T) {
	var path, pageSize string
	scw := newTestWrapper(t, func(w http.ResponseWriter, r *http.Request) {
		path, pageSize = r.URL.Path, r.URL.Query().Get("page_size")
		fmt.Fprint(w, `{"issue_comments":[
			{"id":"c1","body":"secret text","author":{"id":"u1","slug":"alice"},"created_at":"2026-06-15T20:52:34Z","updated_at":"2026-06-15T21:00:00Z"},
			{"id":"c2","body":"more","author":{"id":"u2","slug":"bob"},"created_at":"2026-06-16T08:00:00Z"}]}`)
	})

	got, err := scw.GetIssueComments(authCtx(), "issue-9")
	if err != nil {
		t.Fatal(err)
	}

	if path != "/issues/id:issue-9/comments" || pageSize != "100" {
		t.Errorf("asked %q with page_size %q, want /issues/id:issue-9/comments and 100", path, pageSize)
	}
	if len(got) != 2 || got[0].Author.Slug != "alice" || got[1].Author.Slug != "bob" || got[1].CreatedAt.Hour() != 8 {
		t.Errorf("got %+v", got)
	}
}

func TestGetIssueCommentsMapsErrors(t *testing.T) {
	scw := newTestWrapper(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })
	if _, err := scw.GetIssueComments(authCtx(), "x"); !errors.Is(err, domain.ErrRepositoryNotFound) {
		t.Errorf("404 err = %v, want ErrRepositoryNotFound", err)
	}

	scw = newTestWrapper(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) })
	if _, err := scw.GetIssueComments(authCtx(), "x"); !errors.Is(err, domain.ErrExternalAPI) {
		t.Errorf("502 err = %v, want ErrExternalAPI", err)
	}

	if _, err := scw.GetIssueComments(t.Context(), "x"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("no token err = %v, want ErrUnauthenticated", err)
	}
}
