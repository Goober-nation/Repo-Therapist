package service

import (
	"fmt"
	"net/http"
	"testing"
)

func TestListPublicReposPageAsksForFullPagesInPlatformOrder(t *testing.T) {
	var seen []string

	scw := newTestWrapper(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.RawQuery)
		fmt.Fprint(w, `{"repositories":[{"id":"1","slug":"a"}],"next_page_token":"tok 2"}`)
	})

	page, err := scw.ListPublicReposPage(authCtx(), "tok 1")
	if err != nil {
		t.Fatal(err)
	}

	if len(page.Repositories) != 1 || page.NextPageToken != "tok 2" {
		t.Fatalf("page = %+v", page)
	}
	if len(seen) != 1 || seen[0] != "page_size=100&page_token=tok+1" {
		t.Fatalf("query = %v, want page_size=100 with the token and no sort", seen)
	}
}

// The full listing used to send no page_size: the platform's default of 20 times the page cap stopped
// it at 1000 repositories.
func TestListAllPublicReposAsksForFullPages(t *testing.T) {
	calls := 0

	scw := newTestWrapper(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got := r.URL.Query().Get("page_size"); got != "100" {
			t.Errorf("page_size = %q, want 100", got)
		}

		next := ""
		if calls < 3 {
			next = fmt.Sprintf("p%d", calls+1)
		}
		fmt.Fprintf(w, `{"repositories":[{"id":"%d"}],"next_page_token":%q}`, calls, next)
	})

	repos, err := scw.ListAllPublicRepos(authCtx())
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 3 || calls != 3 {
		t.Fatalf("repos = %d, calls = %d, want 3 pages followed", len(repos), calls)
	}
}
