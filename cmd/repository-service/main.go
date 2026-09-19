// repository-service — first working slice (per backlog/phase-1-repository-service.md):
// given an org/repo slug, fetch the repo's metadata from SourceCraft, git clone its default
// branch, and upload the tree to S3/MinIO. Manually triggered via CLI flags — no Kafka/
// Aggregator integration yet, that comes later.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"repo-therapist/internal/gitlog"
	"repo-therapist/internal/sourcecraft"
	"repo-therapist/internal/storage"
)

func main() {
	org := flag.String("org", "", "SourceCraft org slug (required)")
	repo := flag.String("repo", "", "SourceCraft repo slug (required)")
	flag.Parse()

	if *org == "" || *repo == "" {
		fmt.Fprintln(os.Stderr, "usage: repository-service --org=<org_slug> --repo=<repo_slug>")
		os.Exit(2)
	}

	if err := run(*org, *repo); err != nil {
		log.Fatalf("repository-service: %v", err)
	}
}

func run(org, repo string) error {
	pat := os.Getenv("SOURCECRAFT_PAT")
	if pat == "" {
		return fmt.Errorf("SOURCECRAFT_PAT environment variable is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	scClient := sourcecraft.NewClient(os.Getenv("SOURCECRAFT_BASE_URL"), pat)

	log.Printf("fetching repo metadata for %s/%s", org, repo)
	repoInfo, err := scClient.GetRepo(ctx, org, repo)
	if err != nil {
		return fmt.Errorf("fetching repo metadata: %w", err)
	}
	log.Printf("resolved: id=%s default_branch=%s visibility=%s", repoInfo.ID, repoInfo.DefaultBranch, repoInfo.Visibility)

	tmpDir, err := os.MkdirTemp(cloneTempBase(), "repo-therapist-clone-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	cloneURL, err := authenticatedCloneURL(repoInfo.CloneURL.HTTPS, pat)
	if err != nil {
		return fmt.Errorf("building authenticated clone URL: %w", err)
	}

	historyDays, err := strconv.Atoi(envOrDefault("GIT_HISTORY_DAYS", "365"))
	if err != nil {
		return fmt.Errorf("parsing GIT_HISTORY_DAYS: %w", err)
	}
	since := time.Now().AddDate(0, 0, -historyDays)

	log.Printf("cloning %s (branch %s, history since %s) into %s",
		repoInfo.CloneURL.HTTPS, repoInfo.DefaultBranch, since.Format("2006-01-02"), tmpDir)
	if err := gitClone(ctx, cloneURL, repoInfo.DefaultBranch, since, tmpDir); err != nil {
		return fmt.Errorf("git clone: %w", err)
	}

	log.Printf("extracting commit history since %s", since.Format("2006-01-02"))
	commits, err := gitlog.ExtractHistory(ctx, tmpDir, since)
	if err != nil {
		return fmt.Errorf("extracting git history: %w", err)
	}
	commitsJSON, err := json.Marshal(commits)
	if err != nil {
		return fmt.Errorf("marshaling commit history: %w", err)
	}

	s3Client, err := storage.NewClient(ctx, storage.Config{
		Endpoint:  envOrDefault("S3_ENDPOINT", "localhost:9000"),
		AccessKey: envOrDefault("S3_ACCESS_KEY", "repotherapist"),
		SecretKey: envOrDefault("S3_SECRET_KEY", "repotherapist123"),
		Bucket:    envOrDefault("S3_BUCKET", "repo-content"),
		UseSSL:    false,
	})
	if err != nil {
		return fmt.Errorf("connecting to S3: %w", err)
	}

	keyPrefix := "repos/" + repoInfo.ID
	log.Printf("uploading tree to s3://%s/%s", envOrDefault("S3_BUCKET", "repo-content"), keyPrefix)
	count, err := s3Client.UploadDir(ctx, tmpDir, keyPrefix)
	if err != nil {
		return fmt.Errorf("uploading to S3: %w", err)
	}

	historyKey := keyPrefix + "/_meta/commits.json"
	if err := s3Client.PutObject(ctx, historyKey, commitsJSON, "application/json"); err != nil {
		return fmt.Errorf("uploading commit history: %w", err)
	}

	// Pulls/releases: direct SourceCraft API calls, same pattern as GetRepo above — a stand-in
	// for the eventual Sourcecraft-wrapper RPC, not a permanent architecture choice.
	log.Printf("fetching pull requests for %s/%s", org, repo)
	pulls, err := scClient.ListPulls(ctx, org, repo)
	if err != nil {
		return fmt.Errorf("listing pull requests: %w", err)
	}
	pullsJSON, err := json.Marshal(pulls)
	if err != nil {
		return fmt.Errorf("marshaling pull requests: %w", err)
	}
	if err := s3Client.PutObject(ctx, keyPrefix+"/_meta/pulls.json", pullsJSON, "application/json"); err != nil {
		return fmt.Errorf("uploading pull requests: %w", err)
	}

	log.Printf("fetching releases for %s/%s", org, repo)
	releases, err := scClient.ListReleases(ctx, org, repo)
	if err != nil {
		return fmt.Errorf("listing releases: %w", err)
	}
	releasesJSON, err := json.Marshal(releases)
	if err != nil {
		return fmt.Errorf("marshaling releases: %w", err)
	}
	if err := s3Client.PutObject(ctx, keyPrefix+"/_meta/releases.json", releasesJSON, "application/json"); err != nil {
		return fmt.Errorf("uploading releases: %w", err)
	}

	log.Printf("done: uploaded %d files + %d commits + %d pulls + %d releases for %s/%s (sourcecraft_uuid=%s) to key prefix %q",
		count, len(commits), len(pulls), len(releases), org, repo, repoInfo.ID, keyPrefix)
	return nil
}

// authenticatedCloneURL injects the PAT into an HTTPS clone URL as basic-auth credentials.
func authenticatedCloneURL(rawCloneURL, pat string) (string, error) {
	u, err := url.Parse(rawCloneURL)
	if err != nil {
		return "", fmt.Errorf("parsing clone URL %q: %w", rawCloneURL, err)
	}
	u.User = url.UserPassword("oauth2", pat)
	return u.String(), nil
}

// gitClone clones only the given branch, and only commits authored on/after since — git's
// --shallow-since does this natively server/client-side, no manual commit-by-commit walking
// needed to bound history by date instead of by commit count.
func gitClone(ctx context.Context, cloneURL, branch string, since time.Time, destDir string) error {
	args := []string{
		"clone", "--branch", branch, "--single-branch",
		"--shallow-since=" + since.Format("2006-01-02"),
		cloneURL, destDir,
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(redactURL(args), " "), err)
	}
	return nil
}

// defaultMinShmFreeBytes is the free-space floor cloneTempBase requires on /dev/shm before using
// it — matches the container's recommended `--shm-size=150m` (docker-compose.yml doesn't set
// this yet since no repository-service container exists there; set it to match when it does).
// Overridable via CLONE_SHM_MIN_FREE_BYTES.
const defaultMinShmFreeBytes = 150 * 1024 * 1024

// cloneTempBase picks where the temporary clone gets written. Writing the full checked-out tree
// to real disk on every analysis run, repeated on every periodic regrade, is real avoidable I/O —
// so this prefers a RAM-backed tmpfs mount over disk when one's available and has enough room,
// cutting that cost to near-zero. CLONE_TMP_DIR overrides explicitly.
//
// Rather than guessing a repo's size ahead of time (SourceCraft's API exposes counts — forks,
// PRs, issues — not byte size or file count), this checks /dev/shm's actual free space right
// before cloning and falls back to disk if it's too low. Self-adapting: no per-repo size to
// predict, and it naturally protects against concurrent clones starving each other of the same
// shared RAM, which a static per-repo threshold wouldn't catch.
func cloneTempBase() string {
	if dir := os.Getenv("CLONE_TMP_DIR"); dir != "" {
		return dir
	}

	const shm = "/dev/shm"
	info, err := os.Stat(shm)
	if err != nil || !info.IsDir() {
		return os.TempDir()
	}

	minFree := int64(defaultMinShmFreeBytes)
	if v := os.Getenv("CLONE_SHM_MIN_FREE_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			minFree = n
		}
	}

	free, err := shmFreeBytes(shm)
	if err != nil {
		log.Printf("could not stat %s free space (%v), falling back to disk for clone temp dir", shm, err)
		return os.TempDir()
	}
	if free < uint64(minFree) {
		log.Printf("%s has %d bytes free, below the %d byte floor — falling back to disk for clone temp dir", shm, free, minFree)
		return os.TempDir()
	}

	if f, err := os.CreateTemp(shm, ".write-test-*"); err == nil {
		f.Close()
		os.Remove(f.Name())
		return shm
	}
	return os.TempDir()
}

func shmFreeBytes(path string) (uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

// redactURL returns a copy of args with any element containing "://" (the clone URL, which
// carries the PAT as basic-auth credentials) replaced with a redacted placeholder, so the PAT
// never ends up in an error message or log line.
func redactURL(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.Contains(a, "://") {
			out[i] = "<redacted-clone-url>"
		} else {
			out[i] = a
		}
	}
	return out
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
