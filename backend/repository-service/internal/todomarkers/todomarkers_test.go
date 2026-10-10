package todomarkers

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runGit runs a git command in dir, failing the test on error.
func runGit(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func commitAt(t *testing.T, dir string, when time.Time, msg string) {
	t.Helper()
	ts := when.Format(time.RFC3339)
	runGit(t, dir, []string{
		"GIT_AUTHOR_DATE=" + ts, "GIT_COMMITTER_DATE=" + ts,
	}, "commit", "-m", msg, "--allow-empty-message", "-q")
}

// buildRepo creates a real git repo with:
//   - a recent commit adding a TODO on line 1 of recent.go
//   - an older commit (still within historyDays) adding a FIXME on line 1 of older.go
//   - a vendor/ file with a TODO that must be skipped
//   - a binary file with a byte sequence that looks like TODO, must be skipped
func buildRepo(t *testing.T) (dir string, historyDays int) {
	t.Helper()
	dir = t.TempDir()
	runGit(t, dir, nil, "init", "-q", "-b", "main")
	runGit(t, dir, nil, "config", "user.email", "test@example.com")
	runGit(t, dir, nil, "config", "user.name", "Test")

	writeFile(t, dir, "recent.go", "// TODO: fix this\npackage main\n")
	runGit(t, dir, nil, "add", "recent.go")
	commitAt(t, dir, time.Now().Add(-2*24*time.Hour), "add recent todo")

	writeFile(t, dir, "older.go", "// FIXME: old bug\npackage main\n")
	runGit(t, dir, nil, "add", "older.go")
	commitAt(t, dir, time.Now().Add(-40*24*time.Hour), "add older fixme")

	if err := os.MkdirAll(filepath.Join(dir, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "vendor/dep.go", "// TODO: should never be scanned\n")
	runGit(t, dir, nil, "add", "vendor/dep.go")
	commitAt(t, dir, time.Now(), "add vendor file")

	writeFile(t, dir, "blob.bin", "\x00\x01TODO: not real\x02\x03")
	runGit(t, dir, nil, "add", "blob.bin")
	commitAt(t, dir, time.Now(), "add binary")

	return dir, 365
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScan(t *testing.T) {
	dir, historyDays := buildRepo(t)

	export, err := Scan(context.Background(), dir, historyDays)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if len(export.Markers) != 2 {
		t.Fatalf("expected 2 markers (vendor/ and binary must be skipped), got %d: %+v", len(export.Markers), export.Markers)
	}

	byFile := make(map[string]Marker)
	for _, m := range export.Markers {
		byFile[m.File] = m
	}

	recent, ok := byFile["recent.go"]
	if !ok {
		t.Fatal("missing marker for recent.go")
	}
	if recent.Type != "TODO" {
		t.Errorf("recent.go marker type = %q, want TODO", recent.Type)
	}
	if recent.AgeCapped {
		t.Error("recent.go marker should not be age-capped (well within a full, non-shallow clone)")
	}
	if recent.AgeDays < 1.5 || recent.AgeDays > 2.5 {
		t.Errorf("recent.go AgeDays = %v, want ~2", recent.AgeDays)
	}

	older, ok := byFile["older.go"]
	if !ok {
		t.Fatal("missing marker for older.go")
	}
	if older.Type != "FIXME" {
		t.Errorf("older.go marker type = %q, want FIXME", older.Type)
	}
	if older.AgeDays < 39 || older.AgeDays > 41 {
		t.Errorf("older.go AgeDays = %v, want ~40", older.AgeDays)
	}

	if export.ScannedFiles != 2 {
		t.Errorf("ScannedFiles = %d, want 2 (recent.go+older.go; vendor/ and binary excluded)", export.ScannedFiles)
	}
	if export.TotalLOC == 0 {
		t.Error("TotalLOC = 0, want > 0")
	}
}

// TestScanShallowBoundaryCap verifies that a marker whose true origin predates a shallow clone's
// --shallow-since cutoff gets its age capped at historyDays (AgeCapped: true) rather than an
// artificially-recent age derived from the shallow boundary commit's synthetic date.
func TestScanShallowBoundaryCap(t *testing.T) {
	origin := t.TempDir()
	runGit(t, origin, nil, "init", "-q", "-b", "main")
	runGit(t, origin, nil, "config", "user.email", "test@example.com")
	runGit(t, origin, nil, "config", "user.name", "Test")

	// A commit older still than the ancient one below — its only purpose is to give git a parent
	// to truncate, so the ancient commit itself becomes the shallow clone's grafted boundary
	// commit (a repo's very first commit has no parent to cut, so it can never become a shallow
	// boundary on its own).
	writeFile(t, origin, "root.go", "package main\n")
	runGit(t, origin, nil, "add", "root.go")
	commitAt(t, origin, time.Now().Add(-2000*24*time.Hour), "root commit")

	// A genuinely ancient commit that will fall outside the shallow window.
	writeFile(t, origin, "ancient.go", "// TODO: predates the shallow window\npackage main\n")
	runGit(t, origin, nil, "add", "ancient.go")
	commitAt(t, origin, time.Now().Add(-1000*24*time.Hour), "ancient commit")

	// A recent commit that touches an unrelated file, keeping the branch alive up to now so a
	// --shallow-since clone actually has something within its window to anchor to.
	writeFile(t, origin, "recent.go", "package main\n")
	runGit(t, origin, nil, "add", "recent.go")
	commitAt(t, origin, time.Now().Add(-1*24*time.Hour), "recent commit")

	historyDays := 30
	shallowDir := t.TempDir()
	since := time.Now().AddDate(0, 0, -historyDays).Format("2006-01-02")
	// --shallow-since is silently ignored for local-path clones (git warns "use file:// instead")
	// — the file:// scheme is required to exercise real shallow-clone behavior here, matching
	// clone.go's own use of --shallow-since against a real remote HTTPS URL.
	runGit(t, shallowDir, nil, "clone", "--single-branch", "--shallow-since="+since, "file://"+origin, ".")

	export, err := Scan(context.Background(), shallowDir, historyDays)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if len(export.Markers) != 1 {
		t.Fatalf("expected 1 marker, got %d: %+v", len(export.Markers), export.Markers)
	}
	m := export.Markers[0]
	if !m.AgeCapped {
		t.Error("expected AgeCapped=true for a marker on a shallow-clone boundary commit")
	}
	if m.AgeDays != float64(historyDays) {
		t.Errorf("AgeDays = %v, want exactly historyDays (%d)", m.AgeDays, historyDays)
	}
}

func TestScanNoMarkers(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, nil, "init", "-q", "-b", "main")
	runGit(t, dir, nil, "config", "user.email", "test@example.com")
	runGit(t, dir, nil, "config", "user.name", "Test")
	writeFile(t, dir, "clean.go", "package main\n\nfunc main() {}\n")
	runGit(t, dir, nil, "add", "clean.go")
	commitAt(t, dir, time.Now(), "clean commit")

	export, err := Scan(context.Background(), dir, 365)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(export.Markers) != 0 {
		t.Errorf("expected 0 markers, got %d", len(export.Markers))
	}
	if export.TotalLOC == 0 {
		t.Error("TotalLOC = 0, want > 0 for a non-empty repo")
	}
}

// A dangling symlink or a symlink to a directory is skipped; it must not fail the scan.
func TestScanSkipsBrokenSymlinks(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, nil, "init", "-q", "-b", "main")
	runGit(t, dir, nil, "config", "user.email", "test@example.com")
	runGit(t, dir, nil, "config", "user.name", "Test")
	writeFile(t, dir, "clean.go", "package main\n\nfunc main() {}\n")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing/target", filepath.Join(dir, "dangling")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if err := os.Symlink("sub", filepath.Join(dir, "todir")); err != nil {
		t.Skip("symlinks unavailable")
	}
	runGit(t, dir, nil, "add", "clean.go")
	commitAt(t, dir, time.Now(), "clean commit")

	export, err := Scan(context.Background(), dir, 365)
	if err != nil {
		t.Fatalf("Scan failed on a broken symlink: %v", err)
	}
	if export.ScannedFiles != 1 {
		t.Errorf("ScannedFiles = %d, want 1 (clean.go only)", export.ScannedFiles)
	}
}

// A single line longer than the scanner buffer (minified GeoJSON, notebooks with outputs) used to
// fail the whole repo; the file is now read only up to maxScanBytes.
func TestScanFileCutsOffHugeLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.geojson")
	content := "// TODO: first line\n" + strings.Repeat("x", maxLineBytes+1024) + "\n// TODO: after\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	isText, lines, err := scanFile(path)
	if err != nil {
		t.Fatalf("scanFile: %v", err)
	}
	if !isText || len(lines) == 0 || lines[0].text != "// TODO: first line" {
		t.Fatalf("isText=%v lines=%d", isText, len(lines))
	}
	for _, l := range lines {
		if len(l.text) > maxScanBytes {
			t.Fatalf("line %d is %d bytes, want <= %d", l.number, len(l.text), maxScanBytes)
		}
	}
}
