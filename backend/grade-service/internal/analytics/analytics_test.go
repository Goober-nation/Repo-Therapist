package analytics

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"repo-therapist/grade-service/internal/config"
	"repo-therapist/grade-service/internal/gitlog"
)

var now = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func testConfig() config.AnalyticsConfig {
	return config.AnalyticsConfig{Enabled: true, WindowDays: 180, MinCommits: 5, TopAuthors: 5, TopAreas: 8, TopSubareas: 10, TopFiles: 10, MinMergedPRs: 3, MinReviewedPRs: 3, WaitingDays: 7, MinIssues: 5, MinIssueAuthors: 3, IssueCapDays: 30, ImportShare: 0.8}
}

type spec struct {
	name, email string
	daysAgo     int
	files       []gitlog.FileChange
}

func commits(specs ...spec) []gitlog.Commit {
	out := make([]gitlog.Commit, len(specs))
	for i, s := range specs {
		out[i] = gitlog.Commit{
			Hash: fmt.Sprintf("h%d", i), AuthorName: s.name, AuthorEmail: s.email,
			AuthoredAt: now.AddDate(0, 0, -s.daysAgo), Parents: []string{"p"}, FilesChanged: s.files,
		}
	}
	return out
}

func repeat(n int, s spec) []spec {
	out := make([]spec, n)
	for i := range out {
		out[i] = s
		out[i].daysAgo = s.daysAgo + i
	}
	return out
}

func file(path string, lines int) gitlog.FileChange {
	return gitlog.FileChange{Path: path, Added: lines}
}

func TestDisabledReturnsNil(t *testing.T) {
	cfg := testConfig()
	cfg.Enabled = false
	if got := Compute(cfg, Inputs{CommitsAvailable: true}, now); got != nil {
		t.Errorf("got %+v, want nil when disabled", got)
	}
}

func TestBusFactorIsTheSmallestGroupWithHalfTheCommits(t *testing.T) {
	cases := []struct {
		name  string
		specs []spec
		want  int
	}{
		{"one author did everything", repeat(10, spec{name: "Ann", email: "ann@x"}), 1},
		{"one author has exactly half", append(repeat(5, spec{name: "Ann", email: "ann@x"}), repeat(5, spec{name: "Bob", email: "bob@x"})...), 1},
		{"even split between four needs two", append(append(append(
			repeat(3, spec{name: "A", email: "a@x"}), repeat(3, spec{name: "B", email: "b@x"})...),
			repeat(3, spec{name: "C", email: "c@x"})...), repeat(3, spec{name: "D", email: "d@x"})...), 2},
		{"a dominant author and a long tail", append(repeat(6, spec{name: "Ann", email: "ann@x"}),
			spec{name: "B", email: "b@x"}, spec{name: "C", email: "c@x"}, spec{name: "D", email: "d@x"}, spec{name: "E", email: "e@x"}), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Compute(testConfig(), Inputs{CommitsAvailable: true, Commits: commits(tc.specs...)}, now).BusFactor
			if got.Status != StatusOK || got.Value != tc.want || got.Commits != len(tc.specs) {
				t.Errorf("bus factor = %+v, want value %d over %d commits", got, tc.want, len(tc.specs))
			}
		})
	}
}

func TestBusFactorAuthorsAreMergedByEmailAndNeverExposeEmails(t *testing.T) {
	specs := append(repeat(6, spec{name: "Ann Lee", email: "Ann@X.dev"}), repeat(2, spec{name: "ann", email: "ann@x.dev"})...)
	specs = append(specs, repeat(4, spec{name: "Bob", email: "bob@x.dev"})...)

	got := Compute(testConfig(), Inputs{CommitsAvailable: true, Commits: commits(specs...)}, now)

	bf := got.BusFactor
	if bf.Authors != 2 || bf.TopAuthors[0].Name != "Ann Lee" || bf.TopAuthors[0].Commits != 8 || bf.TopAuthors[0].Share != 0.67 {
		t.Errorf("bus factor = %+v, want one merged author with 8 commits (67%%) named by the most common name", bf)
	}

	raw := fmt.Sprintf("%+v", got)
	if strings.Contains(strings.ToLower(raw), "@") {
		t.Errorf("the result exposes an email: %s", raw)
	}
}

func TestBusFactorListsAtMostTopAuthors(t *testing.T) {
	var specs []spec
	for i := 0; i < 9; i++ {
		specs = append(specs, repeat(2, spec{name: fmt.Sprintf("dev%d", i), email: fmt.Sprintf("d%d@x", i)})...)
	}
	got := Compute(testConfig(), Inputs{CommitsAvailable: true, Commits: commits(specs...)}, now).BusFactor
	if len(got.TopAuthors) != 5 || got.Authors != 9 || got.Value != 5 {
		t.Errorf("bus factor = %+v, want 5 listed of 9 and a value of 5 (18 commits, 9 needed for half)", got)
	}
}

func TestNoDataWhenCommitsAreUnreadableOrFew(t *testing.T) {
	few := Compute(testConfig(), Inputs{CommitsAvailable: true, Commits: commits(repeat(4, spec{name: "Ann", email: "a@x"})...)}, now)
	if few.BusFactor.Status != StatusNoData || few.Ownership.Status != StatusNoData || few.BusFactor.Value != 0 {
		t.Errorf("4 commits: %+v, want no data (a bus factor of 1 over 4 commits is not a finding)", few)
	}

	unreadable := Compute(testConfig(), Inputs{CommitsAvailable: false}, now)
	if unreadable.BusFactor.Status != StatusNoData || unreadable.Ownership.Status != StatusNoData {
		t.Errorf("unreadable commits: %+v, want no data", unreadable)
	}

	if unreadable.BusFactor.TopAuthors == nil || unreadable.Ownership.Areas == nil {
		t.Error("lists must be empty slices, not null, so consumers can iterate them")
	}
}

func TestOnlyCommitsInTheWindowCount(t *testing.T) {
	specs := append(repeat(10, spec{name: "Old", email: "old@x"}), repeat(6, spec{name: "New", email: "new@x"})...)
	for i := 0; i < 10; i++ {
		specs[i].daysAgo += 200 // outside the 180-day window
	}
	for i := 10; i < 16; i++ {
		specs[i].daysAgo = 5 + (i - 10)
	}
	got := Compute(testConfig(), Inputs{CommitsAvailable: true, Commits: commits(specs...)}, now).BusFactor
	if got.Commits != 6 || got.Authors != 1 || got.TopAuthors[0].Name != "New" {
		t.Errorf("bus factor = %+v, want only the 6 recent commits by New", got)
	}
}

func TestOwnershipAreas(t *testing.T) {
	specs := []spec{}
	specs = append(specs, repeat(4, spec{name: "Ann", email: "ann@x", files: []gitlog.FileChange{file("src/a.go", 10)}})...)
	specs = append(specs, spec{name: "Bob", email: "bob@x", daysAgo: 20, files: []gitlog.FileChange{file("src/a.go", 10)}})
	specs = append(specs, repeat(3, spec{name: "Bob", email: "bob@x", daysAgo: 30, files: []gitlog.FileChange{file("docs/guide.md", 5)}})...)
	specs = append(specs, spec{name: "Ann", email: "ann@x", daysAgo: 40, files: []gitlog.FileChange{file("README.md", 2), {Path: "logo.png", Added: -1, Deleted: -1, Binary: true}}})

	got := Compute(testConfig(), Inputs{CommitsAvailable: true, Commits: commits(specs...)}, now).Ownership
	if got.Status != StatusOK || len(got.Areas) != 3 {
		t.Fatalf("ownership = %+v", got)
	}

	src, docs, root := got.Areas[0], got.Areas[1], got.Areas[2]
	if src.Path != "src/" || src.Lines != 50 || src.TopOwner != "Ann" || src.TopOwnerShare != 0.8 || src.Owners != 2 || src.Files != 1 {
		t.Errorf("src area = %+v, want Ann owning 80%% of 50 lines with two owners", src)
	}
	if docs.Path != "docs/" || docs.TopOwner != "Bob" || docs.TopOwnerShare != 1 || docs.Owners != 1 {
		t.Errorf("docs area = %+v", docs)
	}
	if root.Path != RootArea || root.Files != 2 || root.Lines != 3 {
		t.Errorf("root area = %+v, want the README and the binary file (counted as one line) under %q", root, RootArea)
	}
}

func TestOwnershipRenamesCountUnderTheNewPath(t *testing.T) {
	cases := map[string]string{
		"src/{old => new}/file.go": "src/new/file.go",
		"old.txt => new.txt":       "new.txt",
		"{ => lib}/util.go":        "lib/util.go",
		"plain/path.go":            "plain/path.go",
	}
	for in, want := range cases {
		if got := normalizePath(in); got != want {
			t.Errorf("normalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOwnershipListsAtMostTopAreasByLines(t *testing.T) {
	var specs []spec
	for i := 0; i < 12; i++ {
		specs = append(specs, spec{name: "Ann", email: "a@x", daysAgo: i, files: []gitlog.FileChange{file(fmt.Sprintf("pkg%02d/f.go", i), 10+i)}})
	}
	got := Compute(testConfig(), Inputs{CommitsAvailable: true, Commits: commits(specs...)}, now).Ownership
	if len(got.Areas) != 8 || got.Areas[0].Path != "pkg11/" || got.Areas[7].Path != "pkg04/" {
		t.Errorf("areas = %+v, want the 8 busiest directories, busiest first", got.Areas)
	}
}

func TestCodeownersCoverage(t *testing.T) {
	codeowners := `# owners
*              @everyone
/docs/         @writers
/src/legacy/   
*.md           @writers
`
	specs := repeat(6, spec{name: "Ann", email: "a@x", files: []gitlog.FileChange{file("src/main.go", 3)}})
	specs = append(specs,
		spec{name: "Ann", email: "a@x", daysAgo: 30, files: []gitlog.FileChange{file("src/legacy/old.go", 3), file("docs/a.md", 1), file("README.md", 1)}},
	)

	got := Compute(testConfig(), Inputs{CommitsAvailable: true, Commits: commits(specs...), CodeownersPath: ".github/CODEOWNERS", Codeowners: []byte(codeowners)}, now).Ownership.Codeowners

	if !got.Present || got.Path != ".github/CODEOWNERS" || got.Rules != 4 {
		t.Errorf("codeowners = %+v, want a present file with 4 rules", got)
	}
	// 4 changed files: src/main.go (*), docs/a.md (/docs/), README.md (*.md) are owned;
	// src/legacy/old.go matches the later owner-less rule, so it is deliberately unowned.
	if got.CoveredShare == nil || *got.CoveredShare != 0.75 {
		t.Errorf("covered share = %v, want 0.75", got.CoveredShare)
	}
}

func TestCodeownersAbsentOrWithoutChangedFiles(t *testing.T) {
	enough := commits(repeat(6, spec{name: "Ann", email: "a@x", files: []gitlog.FileChange{file("a.go", 1)}})...)

	none := Compute(testConfig(), Inputs{CommitsAvailable: true, Commits: enough}, now).Ownership.Codeowners
	if none.Present || none.CoveredShare != nil || none.Rules != 0 {
		t.Errorf("no file: %+v, want absent with no coverage", none)
	}

	empty := commits(repeat(6, spec{name: "Ann", email: "a@x"})...)
	noFiles := Compute(testConfig(), Inputs{CommitsAvailable: true, Commits: empty, CodeownersPath: "CODEOWNERS", Codeowners: []byte("* @a")}, now).Ownership.Codeowners
	if !noFiles.Present || noFiles.CoveredShare != nil {
		t.Errorf("no changed files: %+v, want present but no coverage figure", noFiles)
	}

	// The file is a fact of the repo even when commits could not be read.
	unreadable := Compute(testConfig(), Inputs{CommitsAvailable: false, CodeownersPath: "CODEOWNERS", Codeowners: []byte("* @a\n/x/ @b")}, now)
	if !unreadable.Ownership.Codeowners.Present || unreadable.Ownership.Codeowners.Rules != 2 || unreadable.Ownership.Status != StatusNoData {
		t.Errorf("unreadable commits: %+v", unreadable.Ownership)
	}
}

func TestPatternMatching(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"*", "any/file.go", true},
		{"*.go", "main.go", true},
		{"*.go", "pkg/deep/main.go", true},
		{"*.go", "main.rs", false},
		{"/docs/", "docs/a.md", true},
		{"/docs/", "other/docs/a.md", false},
		{"docs/", "docs/a.md", true},
		{"docs/*.md", "docs/a.md", true},
		{"docs/*.md", "docs/sub/a.md", false},
		{"docs/**", "docs/sub/a.md", true},
		{"/src/main.go", "src/main.go", true},
		{"/src/main.go", "pkg/src/main.go", false},
		{"Makefile", "build/Makefile", true},
		{"a?c.txt", "abc.txt", true},
		{"a?c.txt", "a/c.txt", false},
		{"/src", "src/x/y.go", true},
	}
	for _, tc := range cases {
		re := patternRegexp(tc.pattern)
		if re == nil {
			t.Fatalf("pattern %q did not compile", tc.pattern)
		}
		if got := re.MatchString(tc.path); got != tc.want {
			t.Errorf("pattern %q on %q = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}

	if patternRegexp("/") != nil || patternRegexp("") != nil {
		t.Error("an empty pattern must not become a match-everything rule")
	}
}

func TestOwnershipSubareasAndTopFiles(t *testing.T) {
	var specs []spec
	specs = append(specs, repeat(3, spec{name: "Ann", email: "ann@x", files: []gitlog.FileChange{file("src/api/handler.go", 20), file("src/main.go", 4)}})...)
	specs = append(specs, repeat(2, spec{name: "Bob", email: "bob@x", daysAgo: 10, files: []gitlog.FileChange{file("src/api/handler.go", 5), file("src/ui/view.go", 30)}})...)
	specs = append(specs, spec{name: "Bob", email: "bob@x", daysAgo: 20, files: []gitlog.FileChange{file("README.md", 2)}})

	got := Compute(testConfig(), Inputs{CommitsAvailable: true, Commits: commits(specs...)}, now).Ownership

	// Depth-2 directories: files directly in src/ or in the root belong to no subdirectory.
	if len(got.Subareas) != 2 {
		t.Fatalf("subareas = %+v, want src/api/ and src/ui/ only", got.Subareas)
	}
	api, ui := got.Subareas[0], got.Subareas[1]
	if api.Path != "src/api/" || api.Lines != 70 || api.TopOwner != "Ann" || api.TopOwnerShare != 0.86 || api.Owners != 2 {
		t.Errorf("src/api/ = %+v, want first (70 lines), Ann owning 60 of them (0.86), two owners", api)
	}
	if ui.Path != "src/ui/" || ui.Lines != 60 || ui.TopOwner != "Bob" || ui.TopOwnerShare != 1 || ui.Owners != 1 {
		t.Errorf("src/ui/ = %+v, want second (60 lines), Bob owning all of it", ui)
	}

	// Files: ordered by changed lines, each with its own main owner.
	if len(got.Files) != 4 || got.Files[0].Path != "src/api/handler.go" || got.Files[0].Lines != 70 || got.Files[0].TopOwner != "Ann" {
		t.Fatalf("files = %+v, want the most changed file first", got.Files)
	}
	if got.Files[1].Path != "src/ui/view.go" || got.Files[1].TopOwner != "Bob" || got.Files[1].TopOwnerShare != 1 {
		t.Errorf("second file = %+v", got.Files[1])
	}
}

func TestOwnershipSubareaHelpers(t *testing.T) {
	for in, want := range map[string]string{
		"a/b/c.go":   "a/b/",
		"a/b/c/d.go": "a/b/",
		"a/b.go":     "",
		"b.go":       "",
	} {
		if got := subareaOf(in); got != want {
			t.Errorf("subareaOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOwnershipTopFilesAndSubareasAreCapped(t *testing.T) {
	cfg := testConfig()
	cfg.TopSubareas, cfg.TopFiles = 2, 3

	var specs []spec
	for i := 0; i < 6; i++ {
		specs = append(specs, spec{name: "Ann", email: "a@x", daysAgo: i, files: []gitlog.FileChange{file(fmt.Sprintf("d/s%d/f.go", i), 10+i)}})
	}

	got := Compute(cfg, Inputs{CommitsAvailable: true, Commits: commits(specs...)}, now).Ownership
	if len(got.Subareas) != 2 || got.Subareas[0].Path != "d/s5/" || len(got.Files) != 3 || got.Files[0].Path != "d/s5/f.go" {
		t.Errorf("subareas = %+v files = %+v, want the 2 / 3 busiest", got.Subareas, got.Files)
	}
}

func TestOwnershipViewsAreEmptyListsWhenThereIsNoData(t *testing.T) {
	got := Compute(testConfig(), Inputs{CommitsAvailable: true, Commits: nil}, now).Ownership
	if got.Subareas == nil || got.Files == nil || len(got.Subareas) != 0 || len(got.Files) != 0 {
		t.Errorf("subareas = %v files = %v, want empty non-nil lists (JSON [])", got.Subareas, got.Files)
	}
}
