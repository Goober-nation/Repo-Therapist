// Package gitlog extracts a structured, S3-batch-friendly commit history from a local git
// checkout — a parsed JSON export rather than raw .git/ objects, so Grade service can read
// history the same way it reads any other file: individually, in parallel, without needing
// git tooling or the full object database. See backlog/phase-1-repository-service.md.
package gitlog

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	recordSep = "\x1e"
	unitSep   = "\x1f"
)

type FileChange struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`   // -1 if binary (git reports "-")
	Deleted int    `json:"deleted"` // -1 if binary
	Binary  bool   `json:"binary,omitempty"`
}

type Commit struct {
	Hash         string       `json:"hash"`
	AuthorName   string       `json:"author_name"`
	AuthorEmail  string       `json:"author_email"`
	AuthoredAt   time.Time    `json:"authored_at"`
	Subject      string       `json:"subject"`
	FilesChanged []FileChange `json:"files_changed"`
}

// ExtractHistory runs `git log --numstat` against repoDir (default branch, current HEAD) for
// commits authored on/after since, and returns them as structured records.
func ExtractHistory(ctx context.Context, repoDir string, since time.Time) ([]Commit, error) {
	format := recordSep + "%H" + unitSep + "%an" + unitSep + "%ae" + unitSep + "%aI" + unitSep + "%s"
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "log",
		"--since="+since.Format("2006-01-02"),
		"--date=iso-strict",
		"--pretty=format:"+format,
		"--numstat",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git log: %w (stderr: %s)", err, stderr.String())
	}

	return parseGitLog(stdout.String())
}

func parseGitLog(output string) ([]Commit, error) {
	if strings.TrimSpace(output) == "" {
		return []Commit{}, nil
	}

	var commits []Commit
	records := strings.Split(output, recordSep)
	for _, record := range records {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		scanner := bufio.NewScanner(strings.NewReader(record))
		if !scanner.Scan() {
			continue
		}
		header := strings.Split(scanner.Text(), unitSep)
		if len(header) != 5 {
			return nil, fmt.Errorf("unexpected git log header field count: %q", scanner.Text())
		}
		authoredAt, err := time.Parse(time.RFC3339, header[3])
		if err != nil {
			return nil, fmt.Errorf("parsing commit date %q: %w", header[3], err)
		}

		commit := Commit{
			Hash:        header[0],
			AuthorName:  header[1],
			AuthorEmail: header[2],
			AuthoredAt:  authoredAt,
			Subject:     header[4],
		}

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "\t", 3)
			if len(parts) != 3 {
				continue
			}
			fc := FileChange{Path: parts[2]}
			if parts[0] == "-" || parts[1] == "-" {
				fc.Binary = true
				fc.Added, fc.Deleted = -1, -1
			} else {
				fc.Added, _ = strconv.Atoi(parts[0])
				fc.Deleted, _ = strconv.Atoi(parts[1])
			}
			commit.FilesChanged = append(commit.FilesChanged, fc)
		}

		commits = append(commits, commit)
	}
	return commits, nil
}
