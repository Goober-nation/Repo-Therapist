// Package todomarkers scans a git checkout for TODO/FIXME-style markers and, for each one, uses
// git blame to find how old the line is. Written before the clone's temp dir is discarded, same
// lifecycle as gitlog's commit history extraction — see
// backend/repository-service/internal/service/clone.go. See docs/grading/code-health.md for the
// grading rationale.
package todomarkers

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Markers is the fixed set of substrings scanned for, word-boundary matched, case-sensitive.
// Kept as a plain list (not a signature) so extending the scan (e.g. adding "XXX"/"HACK" later)
// is a one-line change here, not a rewrite of the scan logic.
var Markers = []string{"TODO", "FIXME"}

// skipDirs are directory names never descended into, beyond ".git" (already skipped by
// storage.UploadDir separately for the tree upload) — dependency trees that aren't the repo's
// own code and would otherwise dominate the marker/LOC counts.
var skipDirs = map[string]bool{
	".git":         true,
	"vendor":       true,
	"node_modules": true,
}

// Marker is one TODO/FIXME occurrence with its blame-derived age.
type Marker struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Type string `json:"type"`
	// AgeDays is time.Since(the blamed commit's author date), in days.
	AgeDays float64 `json:"age_days"`
	// AgeCapped is true when the blamed commit is a shallow-clone boundary commit (listed in
	// .git/shallow) — its true origin predates the clone's --shallow-since window, so AgeDays is
	// set to exactly the clone's history window (a lower bound, "at least this old"), not a
	// precise age. See docs/grading/code-health.md's Known limitations.
	AgeCapped bool `json:"age_capped"`
}

// Export is what gets marshaled to _meta/todo-markers.json. TotalLOC/ScannedFiles cover every
// scanned text file, not just the ones with markers — Code health's density_per_kloc metric
// needs the repo-wide denominator.
type Export struct {
	Markers      []Marker `json:"markers"`
	TotalLOC     int      `json:"total_loc"`
	ScannedFiles int      `json:"scanned_files"`
}

var markerPattern = regexp.MustCompile(`\b(` + strings.Join(Markers, "|") + `)\b`)

// Scan walks repoDir for TODO/FIXME markers and blames each hit. historyDays is the
// --shallow-since window repository-service cloned with (see clone.go), used to cap the age of
// markers whose true origin is outside the shallow clone's history.
func Scan(ctx context.Context, repoDir string, historyDays int) (Export, error) {
	type fileMarker struct {
		relPath string
		line    int
		kind    string
	}

	var (
		export      Export
		fileMarkers []fileMarker
	)

	err := filepath.WalkDir(repoDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		// A symlink that points nowhere or at a directory has no text to scan; one broken link must
		// not fail the whole export. A symlink to a file is scanned as before.
		if d.Type()&fs.ModeSymlink != 0 {
			if info, statErr := os.Stat(path); statErr != nil || info.IsDir() {
				return nil
			}
		}

		isText, lines, err := scanFile(path)
		if err != nil {
			return fmt.Errorf("scanning %s: %w", path, err)
		}
		if !isText {
			return nil
		}

		rel, err := filepath.Rel(repoDir, path)
		if err != nil {
			return fmt.Errorf("computing relative path for %s: %w", path, err)
		}
		rel = filepath.ToSlash(rel)

		export.ScannedFiles++
		for _, l := range lines {
			export.TotalLOC++
			if kind := matchMarker(l.text); kind != "" {
				fileMarkers = append(fileMarkers, fileMarker{relPath: rel, line: l.number, kind: kind})
			}
		}
		return nil
	})
	if err != nil {
		return Export{}, err
	}

	if len(fileMarkers) == 0 {
		return export, nil
	}

	shallowBoundary, err := readShallowBoundary(repoDir)
	if err != nil {
		return Export{}, fmt.Errorf("reading shallow boundary: %w", err)
	}

	byFile := make(map[string][]fileMarker)
	for _, fm := range fileMarkers {
		byFile[fm.relPath] = append(byFile[fm.relPath], fm)
	}

	now := time.Now()
	for relPath, marks := range byFile {
		blame, err := blameFile(ctx, repoDir, relPath)
		if err != nil {
			return Export{}, fmt.Errorf("blaming %s: %w", relPath, err)
		}
		for _, fm := range marks {
			info, ok := blame[fm.line]
			marker := Marker{File: fm.relPath, Line: fm.line, Type: fm.kind}
			switch {
			case !ok:
				// Blame couldn't attribute the line (rare — e.g. a race with the working tree);
				// leave age at zero value rather than fail the whole scan over one line.
			case shallowBoundary[info.hash]:
				marker.AgeDays = float64(historyDays)
				marker.AgeCapped = true
			default:
				marker.AgeDays = now.Sub(info.authorTime).Hours() / 24
			}
			export.Markers = append(export.Markers, marker)
		}
	}

	return export, nil
}

func matchMarker(line string) string {
	m := markerPattern.FindString(line)
	return m
}

const (
	maxLineBytes = 10 * 1024 * 1024
	maxScanBytes = maxLineBytes / 2
)

type textLine struct {
	number int
	text   string
}

// scanFile reports whether path looks like a text file (no null byte in the first 8KB, the same
// heuristic git/ripgrep use) and, if so, returns its non-blank lines.
func scanFile(path string) (isText bool, lines []textLine, err error) {
	f, err := os.Open(path)
	if err != nil {
		return false, nil, err
	}
	defer f.Close()

	head := make([]byte, 8192)
	n, err := f.Read(head)
	if err != nil && err.Error() != "EOF" {
		return false, nil, err
	}
	if bytes.IndexByte(head[:n], 0) != -1 {
		return false, nil, nil
	}
	if _, err := f.Seek(0, 0); err != nil {
		return false, nil, err
	}

	// Only the first half of the line buffer's worth of the file is read: no line within it can
	// outgrow the buffer, so minified data or notebooks with long lines are cut off, not fatal.
	scanner := bufio.NewScanner(io.LimitReader(f, maxScanBytes))
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		text := scanner.Text()
		if strings.TrimSpace(text) == "" {
			continue
		}
		lines = append(lines, textLine{number: lineNum, text: text})
	}
	if err := scanner.Err(); err != nil {
		return false, nil, err
	}
	return true, lines, nil
}

// readShallowBoundary returns the set of commit hashes listed in .git/shallow — the boundary
// commits a --shallow-since clone was cut off at. Absent (non-shallow clone) is not an error, just
// an empty set.
func readShallowBoundary(repoDir string) (map[string]bool, error) {
	data, err := os.ReadFile(filepath.Join(repoDir, ".git", "shallow"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	boundary := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			boundary[line] = true
		}
	}
	return boundary, nil
}

type blameInfo struct {
	hash       string
	authorTime time.Time
}

// blameFile runs one `git blame --porcelain` for the whole file (not per marker line) — cheap
// even for files with many markers, and never touches files without any marker at all, unlike a
// full-repo blame.
func blameFile(ctx context.Context, repoDir, relPath string) (map[int]blameInfo, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "blame", "--porcelain", "--", relPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git blame: %w (stderr: %s)", err, stderr.String())
	}
	return parsePorcelainBlame(stdout.String())
}

// parsePorcelainBlame extracts, per output line number, the commit hash and author-time.
// git blame --porcelain repeats the full commit header only the first time a hash appears in the
// output; this tracks already-seen hashes so later occurrences (attributed via the leading
// "<hash> <orig-line> <final-line>" line) resolve correctly.
func parsePorcelainBlame(output string) (map[int]blameInfo, error) {
	result := make(map[int]blameInfo)
	seen := make(map[string]time.Time)

	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	var curHash string
	var curFinalLine int
	var curAuthorTime time.Time

	headerRe := regexp.MustCompile(`^([0-9a-f]{40}) \d+ (\d+)`)

	for scanner.Scan() {
		line := scanner.Text()
		if m := headerRe.FindStringSubmatch(line); m != nil {
			curHash = m[1]
			curFinalLine, _ = strconv.Atoi(m[2])
			if t, ok := seen[curHash]; ok {
				curAuthorTime = t
				result[curFinalLine] = blameInfo{hash: curHash, authorTime: curAuthorTime}
			}
			continue
		}
		if strings.HasPrefix(line, "author-time ") {
			epoch, err := strconv.ParseInt(strings.TrimPrefix(line, "author-time "), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parsing author-time %q: %w", line, err)
			}
			curAuthorTime = time.Unix(epoch, 0)
			seen[curHash] = curAuthorTime
			result[curFinalLine] = blameInfo{hash: curHash, authorTime: curAuthorTime}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
