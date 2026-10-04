package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sourcecraft-wrapper-service/internal/domain"

	"google.golang.org/grpc/metadata"
)

func authCtx() context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer pat"))
}

func newTestWrapper(t *testing.T, h http.HandlerFunc) *SCWrapper {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer pat" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return NewSCWrapperService(*srv.Client(), srv.URL)
}

func TestGetMyOrgsPicksPersonalOrg(t *testing.T) {
	scw := newTestWrapper(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			fmt.Fprint(w, `{"username":"alice"}`)
		case "/me/orgs":
			fmt.Fprint(w, `{"organizations":[{"id":"1","slug":"team","display_name":"Team"},{"id":"2","slug":"alice","display_name":"alice Personal"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got, err := scw.GetMyOrgs(authCtx())
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultOrgSlug != "alice" || len(got.Organizations) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got.Organizations[0].Personal || !got.Organizations[1].Personal {
		t.Fatalf("personal flags wrong: %+v", got.Organizations)
	}
}

func TestGetMyOrgsFallsBackToFirstOrgWithoutUsername(t *testing.T) {
	scw := newTestWrapper(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/me/orgs" {
			fmt.Fprint(w, `{"organizations":[{"slug":"team"},{"slug":"other"}]}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})

	got, err := scw.GetMyOrgs(authCtx())
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultOrgSlug != "team" || got.Organizations[0].Personal {
		t.Fatalf("got %+v", got)
	}
}

func TestGetMyOrgsFollowsPages(t *testing.T) {
	scw := newTestWrapper(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/user":
			fmt.Fprint(w, `{"username":"b"}`)
		case r.URL.Query().Get("page_token") == "":
			fmt.Fprint(w, `{"organizations":[{"slug":"a"}],"next_page_token":"t2"}`)
		default:
			fmt.Fprint(w, `{"organizations":[{"slug":"b"}]}`)
		}
	})

	got, err := scw.GetMyOrgs(authCtx())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Organizations) != 2 || got.DefaultOrgSlug != "b" {
		t.Fatalf("got %+v", got)
	}
}

func TestGetMyOrgsRequiresToken(t *testing.T) {
	scw := NewSCWrapperService(http.Client{}, "http://unused")
	if _, err := scw.GetMyOrgs(context.Background()); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("err = %v", err)
	}
}

func repoPage(n int, next string) string {
	items := make([]string, n)
	for i := range items {
		items[i] = fmt.Sprintf(`{"id":"r%d","slug":"r%d","organization":{"slug":"team"}}`, i, i)
	}
	return fmt.Sprintf(`{"repositories":[%s],"next_page_token":%q}`, strings.Join(items, ","), next)
}

func TestGetOrgReposCollectsUntilBatch(t *testing.T) {
	var calls atomic.Int32
	scw := newTestWrapper(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/team/repos" || r.URL.Query().Get("page_size") != "100" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		n := calls.Add(1)
		fmt.Fprint(w, repoPage(3, fmt.Sprintf("p%d", n+1)))
	})
	scw.OrgReposBatch = 5

	got, err := scw.GetOrgRepos(authCtx(), "team", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repositories) != 6 || got.NextPageToken != "p3" || calls.Load() != 2 {
		t.Fatalf("repos=%d next=%q calls=%d", len(got.Repositories), got.NextPageToken, calls.Load())
	}
}

func TestGetOrgReposStartsAtPageToken(t *testing.T) {
	scw := newTestWrapper(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page_token") != "p7" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, repoPage(2, ""))
	})

	got, err := scw.GetOrgRepos(authCtx(), "team", "p7")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repositories) != 2 || got.NextPageToken != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestGetOrgReposReturnsPartialWhenLaterPageStalls(t *testing.T) {
	scw := newTestWrapper(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page_token") == "" {
			fmt.Fprint(w, repoPage(1, "slow"))
			return
		}
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	scw.OrgReposBudget = 100 * time.Millisecond

	start := time.Now()
	got, err := scw.GetOrgRepos(authCtx(), "team", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repositories) != 1 || got.NextPageToken != "slow" {
		t.Fatalf("got %+v", got)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("budget not enforced: %v", time.Since(start))
	}
}

func TestGetOrgReposMapsStatuses(t *testing.T) {
	for code, want := range map[int]error{
		http.StatusNotFound:  domain.ErrRepositoryNotFound,
		http.StatusForbidden: domain.ErrNoAccess,
	} {
		scw := newTestWrapper(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) })
		if _, err := scw.GetOrgRepos(authCtx(), "team", ""); !errors.Is(err, want) {
			t.Fatalf("status %d: err = %v", code, err)
		}
	}
}
