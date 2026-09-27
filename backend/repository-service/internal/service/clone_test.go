package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

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

func TestCloneWithDiskFallback(t *testing.T) {
	ctx := context.Background()
	mem, disk := t.TempDir(), t.TempDir()

	t.Run("memory succeeds", func(t *testing.T) {
		dir, err := cloneWithDiskFallback(ctx, mem, disk, func(string) error { return nil })
		if err != nil || filepath.Dir(dir) != mem {
			t.Fatalf("got dir=%q err=%v, want dir under %s", dir, err, mem)
		}
		os.RemoveAll(dir)
	})

	t.Run("memory fails, disk retry", func(t *testing.T) {
		var tried []string
		dir, err := cloneWithDiskFallback(ctx, mem, disk, func(d string) error {
			tried = append(tried, d)
			if filepath.Dir(d) == mem {
				return errors.New("no space left on device")
			}
			return nil
		})
		if err != nil || filepath.Dir(dir) != disk {
			t.Fatalf("got dir=%q err=%v, want dir under %s", dir, err, disk)
		}
		if len(tried) != 2 {
			t.Fatalf("clone called %d times, want 2", len(tried))
		}
		if _, err := os.Stat(tried[0]); !os.IsNotExist(err) {
			t.Fatalf("partial memory clone %s not removed", tried[0])
		}
		os.RemoveAll(dir)
	})

	t.Run("disk base fails without retry", func(t *testing.T) {
		calls := 0
		_, err := cloneWithDiskFallback(ctx, disk, disk, func(string) error { calls++; return errors.New("boom") })
		if err == nil || calls != 1 {
			t.Fatalf("got err=%v calls=%d, want error after 1 call", err, calls)
		}
	})

	t.Run("cancelled context skips retry", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		calls := 0
		_, err := cloneWithDiskFallback(cctx, mem, disk, func(string) error { calls++; return errors.New("killed") })
		if err == nil || calls != 1 {
			t.Fatalf("got err=%v calls=%d, want error after 1 call", err, calls)
		}
	})
}
