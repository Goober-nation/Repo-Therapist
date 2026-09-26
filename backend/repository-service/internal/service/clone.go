// Package service implements repository-service's core work: given an org/repo slug, resolve the
// repo via sourcecraft-wrapper-service, git clone it, and upload the tree + git history +
// issues/milestones to S3/MinIO.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"repo-therapist/repository-service/internal/cidata"
	"repo-therapist/repository-service/internal/gitlog"
	"repo-therapist/repository-service/internal/scwrapper"
	"repo-therapist/repository-service/internal/storage"
	"repo-therapist/repository-service/internal/todomarkers"
)

// Result carries everything the Kafka listen-mode needs to build a repo.ready message — the
// one-shot CLI mode uses it too, just for the final log line instead of a publish.
type Result struct {
	SourcecraftUUID string
	S3Prefix        string
	FileCount       int
	CommitCount     int
	IssueCount      int
	MilestoneCount  int
	TodoMarkerCount int
}

// CloneAndUpload is the shared entry point for both the one-shot CLI path and the Kafka listen
// loop (via transport.handleRequest). token is the caller's SourceCraft PAT; empty falls back to
// the service's own SOURCECRAFT_PAT.
func CloneAndUpload(ctx context.Context, org, repo, token string) (*Result, error) {
	pat, err := resolvePAT(token)
	if err != nil {
		return nil, err
	}

	scClient, err := scwrapper.Dial(envOrDefault("SC_WRAPPER_ADDR", "localhost:8083"), pat)
	if err != nil {
		return nil, fmt.Errorf("connecting to sourcecraft-wrapper-service: %w", err)
	}
	defer scClient.Close()

	log.Printf("resolving repo id for %s/%s", org, repo)
	repoID, err := scClient.GetRepoID(ctx, org, repo)
	if err != nil {
		return nil, fmt.Errorf("resolving repo id: %w", err)
	}
	gitURLs, err := scClient.GetGitURL(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("fetching git url: %w", err)
	}
	log.Printf("resolved: id=%s", repoID)

	tmpDir, err := os.MkdirTemp(cloneTempBase(), "repo-therapist-clone-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	cloneURL, err := authenticatedCloneURL(gitURLs.HTTPURL, pat)
	if err != nil {
		return nil, fmt.Errorf("building authenticated clone URL: %w", err)
	}

	historyDays, err := strconv.Atoi(envOrDefault("GIT_HISTORY_DAYS", "365"))
	if err != nil {
		return nil, fmt.Errorf("parsing GIT_HISTORY_DAYS: %w", err)
	}
	since := time.Now().AddDate(0, 0, -historyDays)

	// No --branch: the wrapper doesn't report default_branch, so this relies on git clone's own
	// default of checking out whatever HEAD points to on the remote.
	log.Printf("cloning %s (history since %s) into %s", gitURLs.HTTPURL, since.Format("2006-01-02"), tmpDir)
	if err := gitClone(ctx, cloneURL, since, tmpDir); err != nil {
		return nil, fmt.Errorf("git clone: %w", err)
	}

	log.Printf("extracting commit history since %s", since.Format("2006-01-02"))
	commits, err := gitlog.ExtractHistory(ctx, tmpDir, since)
	if err != nil {
		return nil, fmt.Errorf("extracting git history: %w", err)
	}
	commitsJSON, err := json.Marshal(commits)
	if err != nil {
		return nil, fmt.Errorf("marshaling commit history: %w", err)
	}

	s3Client, err := storage.NewClient(ctx, storage.Config{
		Endpoint:  envOrDefault("S3_ENDPOINT", "localhost:9000"),
		AccessKey: envOrDefault("S3_ACCESS_KEY", "repotherapist"),
		SecretKey: envOrDefault("S3_SECRET_KEY", "repotherapist123"),
		Bucket:    envOrDefault("S3_BUCKET", "repo-content"),
		UseSSL:    false,
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to S3: %w", err)
	}

	keyPrefix := "repos/" + repoID
	log.Printf("uploading tree to s3://%s/%s", envOrDefault("S3_BUCKET", "repo-content"), keyPrefix)
	fileCount, err := s3Client.UploadDir(ctx, tmpDir, keyPrefix)
	if err != nil {
		return nil, fmt.Errorf("uploading to S3: %w", err)
	}

	historyKey := keyPrefix + "/_meta/commits.json"
	if err := s3Client.PutObject(ctx, historyKey, commitsJSON, "application/json"); err != nil {
		return nil, fmt.Errorf("uploading commit history: %w", err)
	}

	log.Printf("scanning for TODO/FIXME markers in %s", tmpDir)
	todoExport, err := todomarkers.Scan(ctx, tmpDir, historyDays)
	if err != nil {
		return nil, fmt.Errorf("scanning todo markers: %w", err)
	}
	todoJSON, err := json.Marshal(todoExport)
	if err != nil {
		return nil, fmt.Errorf("marshaling todo markers: %w", err)
	}
	if err := s3Client.PutObject(ctx, keyPrefix+"/_meta/todo-markers.json", todoJSON, "application/json"); err != nil {
		return nil, fmt.Errorf("uploading todo markers: %w", err)
	}

	// CI/CD export: a runs fetch failure (typically 403 — /cicd/runs needs owner access) is not
	// fatal, it's recorded as runs_available=false and CI/CD grades as no-data.
	ciExport := cidata.Export{ConfigFiles: cidata.DetectConfigs(tmpDir), Runs: []scwrapper.CIRun{}}
	log.Printf("fetching CI runs for %s/%s", org, repo)
	if runs, err := scClient.GetCICDRuns(ctx, repoID); err != nil {
		log.Printf("CI runs unavailable for %s/%s: %v (CI/CD will be no-data)", org, repo, err)
	} else {
		ciExport.RunsAvailable = true
		ciExport.Runs = cidata.SelectRuns(runs, time.Now(), cidata.WindowDays, cidata.MaxRuns)
	}
	ciJSON, err := json.Marshal(ciExport)
	if err != nil {
		return nil, fmt.Errorf("marshaling CI runs: %w", err)
	}
	if err := s3Client.PutObject(ctx, keyPrefix+"/_meta/ci-runs.json", ciJSON, "application/json"); err != nil {
		return nil, fmt.Errorf("uploading CI runs: %w", err)
	}

	log.Printf("fetching issues for %s/%s", org, repo)
	issues, err := scClient.GetIssues(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("listing issues: %w", err)
	}
	issuesJSON, err := json.Marshal(issues)
	if err != nil {
		return nil, fmt.Errorf("marshaling issues: %w", err)
	}
	if err := s3Client.PutObject(ctx, keyPrefix+"/_meta/issues.json", issuesJSON, "application/json"); err != nil {
		return nil, fmt.Errorf("uploading issues: %w", err)
	}

	log.Printf("fetching milestones for %s/%s", org, repo)
	milestones, err := scClient.GetMilestones(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("listing milestones: %w", err)
	}
	milestonesJSON, err := json.Marshal(milestones)
	if err != nil {
		return nil, fmt.Errorf("marshaling milestones: %w", err)
	}
	if err := s3Client.PutObject(ctx, keyPrefix+"/_meta/milestones.json", milestonesJSON, "application/json"); err != nil {
		return nil, fmt.Errorf("uploading milestones: %w", err)
	}

	return &Result{
		SourcecraftUUID: repoID,
		S3Prefix:        keyPrefix,
		FileCount:       fileCount,
		CommitCount:     len(commits),
		IssueCount:      len(issues),
		MilestoneCount:  len(milestones),
		TodoMarkerCount: len(todoExport.Markers),
	}, nil
}

func resolvePAT(token string) (string, error) {
	if token != "" {
		return token, nil
	}
	if pat := os.Getenv("SOURCECRAFT_PAT"); pat != "" {
		return pat, nil
	}
	return "", fmt.Errorf("no request token and SOURCECRAFT_PAT environment variable is not set")
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

// gitClone clones the remote's default branch (whatever HEAD points to — no default_branch is
// available from the wrapper, so this relies on git's own default instead of naming one), and
// only commits authored on/after since — git's --shallow-since does this natively
// server/client-side, no manual commit-by-commit walking needed to bound history by date instead
// of by commit count.
func gitClone(ctx context.Context, cloneURL string, since time.Time, destDir string) error {
	args := []string{
		"clone", "--single-branch",
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
