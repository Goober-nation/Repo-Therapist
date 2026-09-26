package service

import "testing"

func TestResolvePAT(t *testing.T) {
	t.Setenv("SOURCECRAFT_PAT", "service-pat")
	if got, _ := resolvePAT("user-token"); got != "user-token" {
		t.Errorf("request token should win, got %q", got)
	}
	if got, _ := resolvePAT(""); got != "service-pat" {
		t.Errorf("empty token should fall back to env, got %q", got)
	}

	t.Setenv("SOURCECRAFT_PAT", "")
	if _, err := resolvePAT(""); err == nil {
		t.Error("no token and no env PAT should error")
	}
}

func TestRedactURLHidesToken(t *testing.T) {
	cloneURL, err := authenticatedCloneURL("https://git.sourcecraft.dev/org/repo.git", "secret-token")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range redactURL([]string{"clone", cloneURL, "/tmp/x"}) {
		if a == cloneURL {
			t.Fatalf("clone URL with token not redacted: %v", a)
		}
	}
}
