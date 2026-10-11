// Package service implements repository-service's core work: given an org/repo slug, resolve the
// repo via sourcecraft-wrapper-service, git clone it, and upload the tree + git history +
// issues/milestones to S3/MinIO.
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"repo-therapist/runtimeconfig"
	"strconv"
	"strings"
	"time"

	"repo-therapist/repository-service/internal/cidata"
	"repo-therapist/repository-service/internal/deadcode"
	"repo-therapist/repository-service/internal/gitlog"
	"repo-therapist/repository-service/internal/issuedata"
	"repo-therapist/repository-service/internal/prdata"
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
// the service's own SOURCECRAFT_PAT. A failure after the repo id is resolved still returns a
// Result carrying SourcecraftUUID, so the failure can be correlated downstream.
//
// jobID scopes the S3 export to repos/<uuid>/<jobID>, so overlapping runs of one repo never
// share or delete each other's files; empty (one-shot CLI) keeps the legacy repos/<uuid>.
func CloneAndUpload(ctx context.Context, org, repo, jobID, token string) (res *Result, err error) {
	var knownID string
	defer func() {
		if err != nil && knownID != "" {
			res = &Result{SourcecraftUUID: knownID}
		}
	}()

	pat, err := resolvePAT(token)
	if err != nil {
		return nil, err
	}

	scClient, err := scwrapper.Dial(envOrDefault("SC_WRAPPER_ADDR", "localhost:8082"), pat)
	if err != nil {
		return nil, fmt.Errorf("connecting to sourcecraft-wrapper-service: %w", err)
	}
	defer scClient.Close()

	log.Printf("resolving repo id for %s/%s", org, repo)
	repoID, err := scClient.GetRepoID(ctx, org, repo)
	if err != nil {
		return nil, fmt.Errorf("resolving repo id: %w", err)
	}
	knownID = repoID
	gitURLs, err := scClient.GetGitURL(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("fetching git url: %w", err)
	}
	log.Printf("resolved: id=%s", repoID)

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
	log.Printf("cloning %s (history since %s)", gitURLs.HTTPURL, since.Format("2006-01-02"))
	tmpDir, err := cloneWithDiskFallback(ctx, cloneTempBase(), os.TempDir(), func(dir string) error {
		return gitClone(ctx, cloneURL, since, dir)
	})
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

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
		AccessKey: runtimeconfig.Getenv("S3_ACCESS_KEY"),
		SecretKey: runtimeconfig.Getenv("S3_SECRET_KEY"),
		Bucket:    envOrDefault("S3_BUCKET", "repo-content"),
		UseSSL:    false,
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to S3: %w", err)
	}

	keyPrefix := S3Prefix(repoID, jobID)
	// A previous export may still be there (Grade kept or failed to delete it); clear it so files
	// removed from the repo since then aren't graded again.
	if n, err := s3Client.DeletePrefix(ctx, keyPrefix+"/"); err != nil {
		log.Printf("clearing previous S3 export for %s failed: %v (stale files may remain)", repoID, err)
	} else if n > 0 {
		log.Printf("cleared %d stale S3 objects under %s", n, keyPrefix)
	}
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

	// Dead-code export is best-effort: without it only Code health's unused-functions metric is
	// no-data, so a scan or upload failure must not fail the clone.
	log.Printf("scanning for unused private functions in %s", tmpDir)
	if deadExport, err := deadcode.Scan(tmpDir); err != nil {
		log.Printf("dead-code scan failed for %s/%s: %v (metric will be no-data)", org, repo, err)
	} else if deadJSON, err := json.Marshal(deadExport); err != nil {
		log.Printf("marshaling dead-code export failed: %v", err)
	} else if err := s3Client.PutObject(ctx, keyPrefix+"/_meta/dead-functions.json", deadJSON, "application/json"); err != nil {
		log.Printf("uploading dead-code export failed: %v", err)
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

	// Issue replies sample: the newest ISSUE_REPLY_SAMPLE_LIMIT issues with who commented and when,
	// for the first-reply metric. Advisory like the pull request sample: a fetch failure never fails
	// the export (the issue's replies are marked unavailable).
	replyLimit := issueReplySampleLimit()
	log.Printf("fetching comments of the %d newest issues for %s/%s", replyLimit, org, repo)
	repliesJSON, err := json.Marshal(issuedata.Collect(ctx, scClient, issues, replyLimit))
	if err != nil {
		return nil, fmt.Errorf("marshaling issue replies: %w", err)
	}
	if err := s3Client.PutObject(ctx, keyPrefix+"/_meta/issue-replies.json", repliesJSON, "application/json"); err != nil {
		return nil, fmt.Errorf("uploading issue replies: %w", err)
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

	// Review analytics sample: the newest PR_SAMPLE_LIMIT pull requests with reviewers and comment
	// metadata. Informational only, so a fetch failure never fails the export (available=false).
	sampleLimit := prSampleLimit()
	log.Printf("fetching %d newest pull requests for %s/%s", sampleLimit, org, repo)
	pullsJSON, err := json.Marshal(prdata.Collect(ctx, scClient, repoID, sampleLimit))
	if err != nil {
		return nil, fmt.Errorf("marshaling pull requests: %w", err)
	}
	if err := s3Client.PutObject(ctx, keyPrefix+"/_meta/pulls.json", pullsJSON, "application/json"); err != nil {
		return nil, fmt.Errorf("uploading pull requests: %w", err)
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

// S3Prefix is a job's export prefix (no trailing slash).
func S3Prefix(repoID, jobID string) string {
	if jobID == "" {
		return "repos/" + repoID
	}
	return "repos/" + repoID + "/" + jobID
}

func resolvePAT(token string) (string, error) {
	if token != "" {
		return token, nil
	}
	if pat := runtimeconfig.Getenv("SOURCECRAFT_PAT"); pat != "" {
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
	var stderr bytes.Buffer
	cmd.Stdout = os.Stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	if err := cmd.Run(); err != nil {
		if checkoutOnlyFailed(ctx, stderr.String(), destDir) {
			skipped, restoreErr := restoreCheckout(ctx, destDir)
			if restoreErr != nil {
				return fmt.Errorf("restoring working tree after failed checkout: %w", restoreErr)
			}
			log.Printf("warning: git could not check out the working tree; restored it without %d file(s) whose names exceed the %d-byte limit", skipped, maxNameBytes)
			return nil
		}
		return fmt.Errorf("git %s: %w", strings.Join(redactURL(args), " "), err)
	}
	return nil
}

// maxNameBytes is the longest file name (one path component) the filesystems we clone onto
// accept: Linux NAME_MAX. Cyrillic letters take two bytes each, so such names are easy to hit.
const maxNameBytes = 255

// checkoutOnlyFailed reports a clone whose objects were fetched but whose working-tree checkout
// failed ("Clone succeeded, but checkout failed", exit 128). The commit history is then complete,
// so the clone is worth keeping; any other failure still fails the clone. HEAD must resolve in
// destDir, so a clone that never fetched anything is not mistaken for this case.
func checkoutOnlyFailed(ctx context.Context, stderr, destDir string) bool {
	if !strings.Contains(stderr, "Clone succeeded, but checkout failed") {
		return false
	}
	return exec.CommandContext(ctx, "git", "-C", destDir, "rev-parse", "--verify", "HEAD").Run() == nil
}

// restoreCheckout rebuilds the working tree after a failed checkout, which leaves it empty. It
// refills the index and checks out every file of HEAD except those with a path component over maxNameBytes, and returns
// how many files were skipped. Skipped files are simply absent from the later scans.
func restoreCheckout(ctx context.Context, destDir string) (int, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", destDir, "ls-tree", "-r", "-z", "--name-only", "HEAD").Output()
	if err != nil {
		return 0, fmt.Errorf("listing files: %w", err)
	}

	var keep bytes.Buffer
	skipped := 0
	for _, path := range strings.Split(string(out), "\x00") {
		if path == "" {
			continue
		}
		if hasLongName(path) {
			skipped++
			continue
		}
		keep.WriteString(path)
		keep.WriteByte(0)
	}

	// The failed checkout also leaves the index empty; refill it so checkout-index finds the files.
	if out, err := exec.CommandContext(ctx, "git", "-C", destDir, "read-tree", "HEAD").CombinedOutput(); err != nil {
		return 0, fmt.Errorf("reading tree: %w: %s", err, strings.TrimSpace(string(out)))
	}

	cmd := exec.CommandContext(ctx, "git", "-C", destDir, "checkout-index", "-f", "-z", "--stdin")
	cmd.Stdin = &keep
	if out, err := cmd.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("checking out files: %w: %s", err, strings.TrimSpace(string(out)))
	}

	return skipped, nil
}

func hasLongName(path string) bool {
	for _, part := range strings.Split(path, "/") {
		if len(part) > maxNameBytes {
			return true
		}
	}
	return false
}

// defaultMinShmFreeBytes is the free-space floor cloneTempBase requires on /dev/shm before using
// it. docker-compose.yml gives the container 400m of shm; a repo that outgrows what is free is
// retried on disk by cloneWithDiskFallback. Overridable via CLONE_SHM_MIN_FREE_BYTES.
const defaultMinShmFreeBytes = 150 * 1024 * 1024

// cloneWithDiskFallback runs clone into a fresh temp dir under base and returns that dir. If base
// is not diskBase and the clone fails (typically tmpfs filling up mid-clone, since a repo's size
// can't be known in advance), the partial clone is removed and the clone is retried once under
// diskBase. On error no temp dir is left behind.
func cloneWithDiskFallback(ctx context.Context, base, diskBase string, clone func(dir string) error) (string, error) {
	dir, err := cloneInto(base, clone)
	if err == nil || base == diskBase || ctx.Err() != nil {
		return dir, err
	}
	log.Printf("clone under %s failed (%v), retrying on disk under %s", base, err, diskBase)
	return cloneInto(diskBase, clone)
}

func cloneInto(base string, clone func(dir string) error) (string, error) {
	dir, err := os.MkdirTemp(base, "repo-therapist-clone-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	if err := clone(dir); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("git clone: %w", err)
	}
	return dir, nil
}

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
	if dir := runtimeconfig.Getenv("CLONE_TMP_DIR"); dir != "" {
		return dir
	}

	const shm = "/dev/shm"
	info, err := os.Stat(shm)
	if err != nil || !info.IsDir() {
		return os.TempDir()
	}

	minFree := int64(defaultMinShmFreeBytes)
	if v := runtimeconfig.Getenv("CLONE_SHM_MIN_FREE_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			minFree = n
		} else {
			log.Printf("ignoring invalid CLONE_SHM_MIN_FREE_BYTES=%q, using %d", v, minFree)
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

// prSampleLimit returns PR_SAMPLE_LIMIT (0 switches the pull request sample off); an invalid or
// negative value falls back to prdata.DefaultSampleLimit.
func prSampleLimit() int {
	v := runtimeconfig.Getenv("PR_SAMPLE_LIMIT")
	if v == "" {
		return prdata.DefaultSampleLimit
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		log.Printf("ignoring invalid PR_SAMPLE_LIMIT=%q, using %d", v, prdata.DefaultSampleLimit)
		return prdata.DefaultSampleLimit
	}
	return n
}

func envOrDefault(key, def string) string {
	if v := runtimeconfig.Getenv(key); v != "" {
		return v
	}
	return def
}

// issueReplySampleLimit returns ISSUE_REPLY_SAMPLE_LIMIT (0 switches the issue reply sample off); an
// invalid or negative value falls back to issuedata.DefaultSampleLimit.
func issueReplySampleLimit() int {
	v := runtimeconfig.Getenv("ISSUE_REPLY_SAMPLE_LIMIT")
	if v == "" {
		return issuedata.DefaultSampleLimit
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		log.Printf("ignoring invalid ISSUE_REPLY_SAMPLE_LIMIT=%q, using %d", v, issuedata.DefaultSampleLimit)
		return issuedata.DefaultSampleLimit
	}
	return n
}
