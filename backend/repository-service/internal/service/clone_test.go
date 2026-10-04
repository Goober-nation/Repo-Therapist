package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestPRSampleLimit(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
	}{{"", 10}, {"25", 25}, {"0", 0}, {"-1", 10}, {"abc", 10}} {
		t.Setenv("PR_SAMPLE_LIMIT", tc.value)
		if got := prSampleLimit(); got != tc.want {
			t.Errorf("PR_SAMPLE_LIMIT=%q: got %d, want %d", tc.value, got, tc.want)
		}
	}
}

// gitIn runs git in dir and returns its trimmed stdout.
func gitIn(t *testing.T, dir string, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// A repo tracking a file name over the 255-byte limit clones fine but cannot be checked out;
// gitClone must keep the clone and restore every file that can be written instead of failing.
func TestGitCloneKeepsCloneWhenCheckoutFails(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	src := t.TempDir()
	gitIn(t, src, "", "init", "-q")
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, src, "", "add", "README.md")
	blob := gitIn(t, src, "x\n", "hash-object", "-w", "--stdin")
	tooLong := strings.Repeat("a", 300) + ".txt"
	gitIn(t, src, "", "update-index", "--add", "--cacheinfo", "100644,"+blob+","+tooLong)
	gitIn(t, src, "", "commit", "-q", "-m", "init")

	dest := filepath.Join(t.TempDir(), "clone")
	err := gitClone(context.Background(), "file://"+src, time.Now().AddDate(0, 0, -30), dest)
	if err != nil {
		t.Fatalf("gitClone failed on a checkout-only failure: %v", err)
	}
	if got := gitIn(t, dest, "", "rev-list", "--count", "HEAD"); got != "1" {
		t.Errorf("commits in clone = %s, want 1", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
		t.Errorf("README.md was not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, tooLong)); err == nil {
		t.Errorf("over-long file name should have been skipped")
	}
}

func TestGitCloneStillFailsWhenNothingWasCloned(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dest := filepath.Join(t.TempDir(), "clone")
	err := gitClone(context.Background(), "file://"+filepath.Join(t.TempDir(), "missing"), time.Now().AddDate(0, 0, -30), dest)
	if err == nil {
		t.Fatal("cloning a missing repo should still fail")
	}
}
