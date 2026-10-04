// Package analytics computes the informational repository analytics shown next to the Repo Health
// Score: bus factor and the ownership map (docs/grading/analytics.md). It never feeds the score.
// Everything here is derived from data Grade service already reads (commits.json and the repo's
// CODEOWNERS file), so a repo whose sources are unreadable gets "no_data", not a made-up zero.
package analytics

import (
	"math"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"repo-therapist/grade-service/internal/config"
	"repo-therapist/grade-service/internal/gitlog"
	"repo-therapist/grade-service/internal/issuereplies"
)

// Version is the schema version of the analytics object in grading.results.
const Version = 4

// Statuses of one analytics block.
const (
	StatusOK     = "ok"
	StatusNoData = "no_data"
)

// RootArea names the area of files that live in the repo root.
const RootArea = "(корень)"

// ownerShareFloor: an author with at least this share of an area's changed lines counts as one of
// its owners.
const ownerShareFloor = 0.10

// Inputs are what the analytics read. CommitsAvailable false (commits.json unreadable) makes both
// commit-based blocks "no_data".
type Inputs struct {
	CommitsAvailable bool
	// Commits are the commits that survived the Activity commit filters, so filler commits do not
	// shape the bus factor either.
	Commits []gitlog.Commit
	// CodeownersPath / Codeowners: the CODEOWNERS file found in the repo ("" when none).
	CodeownersPath string
	Codeowners     []byte
	// Pulls is the _meta/pulls.json export; nil when the file is missing or unreadable.
	Pulls *PullsExport
	// IssueReplies is the _meta/issue-replies.json export after the instant-close filter; nil when
	// the file is missing or unreadable.
	IssueReplies *issuereplies.Export
}

type AuthorShare struct {
	Name    string  `json:"name"`
	Commits int     `json:"commits"`
	Share   float64 `json:"share"`
}

// BusFactor is the smallest number of authors who together made at least half of the commits.
type BusFactor struct {
	Status     string        `json:"status"`
	Value      int           `json:"value,omitempty"`
	Commits    int           `json:"commits"`
	Authors    int           `json:"authors"`
	TopAuthors []AuthorShare `json:"top_authors"`
}

// Area is one top-level directory of the repo and who changed it.
type Area struct {
	Path          string  `json:"path"`
	Files         int     `json:"files"`
	Lines         int     `json:"lines"`
	TopOwner      string  `json:"top_owner"`
	TopOwnerShare float64 `json:"top_owner_share"`
	Owners        int     `json:"owners"`
}

// FileOwner is one frequently changed file and who changed it most.
type FileOwner struct {
	Path          string  `json:"path"`
	Lines         int     `json:"lines"`
	TopOwner      string  `json:"top_owner"`
	TopOwnerShare float64 `json:"top_owner_share"`
	Owners        int     `json:"owners"`
}

// Codeowners describes the repo's CODEOWNERS file against the files changed in the window.
type Codeowners struct {
	Present bool   `json:"present"`
	Path    string `json:"path,omitempty"`
	Rules   int    `json:"rules"`
	// CoveredShare is the share of the window's changed files matched by a rule that names an
	// owner; nil when there is no file or no changed files to compare with.
	CoveredShare *float64 `json:"covered_share"`
}

type Ownership struct {
	Status string `json:"status"`
	// Areas are the top-level directories, Subareas the busiest directories two levels deep
	// ("src/api/"), Files the most changed files; each with its main owner.
	Areas      []Area      `json:"areas"`
	Subareas   []Area      `json:"subareas"`
	Files      []FileOwner `json:"files"`
	Codeowners Codeowners  `json:"codeowners"`
}

type Result struct {
	Version    int       `json:"version"`
	WindowDays int       `json:"window_days"`
	BusFactor  BusFactor `json:"bus_factor"`
	Ownership  Ownership `json:"ownership"`
	Reviews    Reviews   `json:"reviews"`
	// IssueReplies is the first-reply block of the newest issues.
	IssueReplies IssueReplies `json:"issue_replies"`
}

// Compute builds the analytics, or nil when they are switched off in config.
func Compute(cfg config.AnalyticsConfig, in Inputs, now time.Time) *Result {
	if !cfg.Enabled {
		return nil
	}

	result := &Result{
		Version:      Version,
		WindowDays:   cfg.WindowDays,
		BusFactor:    BusFactor{Status: StatusNoData, TopAuthors: []AuthorShare{}},
		Ownership:    Ownership{Status: StatusNoData, Areas: []Area{}, Subareas: []Area{}, Files: []FileOwner{}},
		Reviews:      reviewStats(cfg, in.Pulls, now),
		IssueReplies: issueReplyStats(cfg, in.IssueReplies, now),
	}

	// CODEOWNERS is reported whether or not commits are readable: its presence is a fact of the repo.
	result.Ownership.Codeowners = Codeowners{Present: len(in.Codeowners) > 0, Path: in.CodeownersPath}
	rules := parseCodeowners(in.Codeowners)
	result.Ownership.Codeowners.Rules = len(rules)

	if !in.CommitsAvailable {
		return result
	}

	cutoff := now.AddDate(0, 0, -cfg.WindowDays)
	commits := make([]gitlog.Commit, 0, len(in.Commits))
	for _, c := range in.Commits {
		if !c.AuthoredAt.Before(cutoff) {
			commits = append(commits, c)
		}
	}

	people := newDirectory(commits)

	if len(commits) > 0 && len(commits) >= cfg.MinCommits {
		result.BusFactor = busFactor(commits, people, cfg.TopAuthors)
		result.Ownership.Status = StatusOK
		result.Ownership.Areas = ownershipAreas(commits, people, areaOf, cfg.TopAreas)
		result.Ownership.Subareas = ownershipAreas(commits, people, subareaOf, cfg.TopSubareas)
		result.Ownership.Files = ownershipFiles(commits, people, cfg.TopFiles)
		if result.Ownership.Codeowners.Present {
			result.Ownership.Codeowners.CoveredShare = coveredShare(commits, rules)
		}
	}

	return result
}

// directory maps an author (by lower-cased email, else name) to the name shown for them: the name
// they most often commit under. Emails are never exposed.
type directory struct {
	display map[string]string
}

func authorKey(c gitlog.Commit) string {
	if email := strings.ToLower(strings.TrimSpace(c.AuthorEmail)); email != "" {
		return email
	}
	return strings.ToLower(strings.TrimSpace(c.AuthorName))
}

func newDirectory(commits []gitlog.Commit) directory {
	names := map[string]map[string]int{}
	for _, c := range commits {
		key := authorKey(c)
		if names[key] == nil {
			names[key] = map[string]int{}
		}
		names[key][strings.TrimSpace(c.AuthorName)]++
	}

	display := make(map[string]string, len(names))
	for key, counts := range names {
		best, bestCount := "", -1
		for name, n := range counts {
			if n > bestCount || (n == bestCount && name < best) {
				best, bestCount = name, n
			}
		}
		if best == "" {
			best = "unknown"
		}
		display[key] = best
	}

	return directory{display: display}
}

func (d directory) name(key string) string { return d.display[key] }

func busFactor(commits []gitlog.Commit, people directory, topN int) BusFactor {
	counts := map[string]int{}
	for _, c := range commits {
		counts[authorKey(c)]++
	}

	type entry struct {
		key   string
		count int
	}
	entries := make([]entry, 0, len(counts))
	for key, n := range counts {
		entries = append(entries, entry{key, n})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		return entries[i].key < entries[j].key
	})

	total := len(commits)
	value, running := 0, 0
	for _, e := range entries {
		running += e.count
		value++
		if float64(running) >= float64(total)/2 {
			break
		}
	}

	top := make([]AuthorShare, 0, topN)
	for i, e := range entries {
		if i >= topN {
			break
		}
		top = append(top, AuthorShare{Name: people.name(e.key), Commits: e.count, Share: round2(float64(e.count) / float64(total))})
	}

	return BusFactor{Status: StatusOK, Value: value, Commits: total, Authors: len(entries), TopAuthors: top}
}

type areaStats struct {
	files map[string]struct{}
	lines map[string]int // author key -> changed lines
	total int
}

// ownershipFiles lists the topN most changed files with their main owner. Unlike the directory
// views it needs no grouping: every file is its own area.
func ownershipFiles(commits []gitlog.Commit, people directory, topN int) []FileOwner {
	areas := ownershipAreas(commits, people, func(p string) string { return p }, topN)
	files := make([]FileOwner, 0, len(areas))

	for _, a := range areas {
		files = append(files, FileOwner{Path: a.Path, Lines: a.Lines, TopOwner: a.TopOwner, TopOwnerShare: a.TopOwnerShare, Owners: a.Owners})
	}

	return files
}

// ownershipAreas groups the changed files with group (an empty name skips the file) and, per group,
// finds who changed it most. The top topN groups by changed lines are returned.
func ownershipAreas(commits []gitlog.Commit, people directory, group func(path string) string, topN int) []Area {
	stats := map[string]*areaStats{}
	for _, c := range commits {
		key := authorKey(c)
		for _, f := range c.FilesChanged {
			p := normalizePath(f.Path)
			if p == "" {
				continue
			}
			name := group(p)
			if name == "" {
				continue
			}
			s := stats[name]
			if s == nil {
				s = &areaStats{files: map[string]struct{}{}, lines: map[string]int{}}
				stats[name] = s
			}
			lines := f.Added + f.Deleted
			if f.Binary || f.Added < 0 || f.Deleted < 0 {
				lines = 1 // a binary change has no line count but is still a change
			}
			s.files[p] = struct{}{}
			s.lines[key] += lines
			s.total += lines
		}
	}

	areas := make([]Area, 0, len(stats))
	for name, s := range stats {
		if s.total == 0 {
			continue
		}
		topKey, topLines, owners := "", -1, 0
		for key, lines := range s.lines {
			if lines > topLines || (lines == topLines && key < topKey) {
				topKey, topLines = key, lines
			}
			if float64(lines)/float64(s.total) >= ownerShareFloor {
				owners++
			}
		}
		areas = append(areas, Area{
			Path:          name,
			Files:         len(s.files),
			Lines:         s.total,
			TopOwner:      people.name(topKey),
			TopOwnerShare: round2(float64(topLines) / float64(s.total)),
			Owners:        owners,
		})
	}

	sort.Slice(areas, func(i, j int) bool {
		if areas[i].Lines != areas[j].Lines {
			return areas[i].Lines > areas[j].Lines
		}
		return areas[i].Path < areas[j].Path
	})
	if len(areas) > topN {
		areas = areas[:topN]
	}

	return areas
}

var renameBraces = regexp.MustCompile(`\{[^{}]*? => ([^{}]*)\}`)

// normalizePath turns a numstat rename ("a/{b => c}/d" or "a => b") into the path after the rename.
func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	if strings.Contains(p, "{") && strings.Contains(p, " => ") {
		p = renameBraces.ReplaceAllString(p, "$1")
		p = strings.ReplaceAll(p, "//", "/")
	} else if i := strings.Index(p, " => "); i >= 0 {
		p = p[i+len(" => "):]
	}

	return strings.TrimPrefix(path.Clean("/"+p), "/")
}

func areaOf(p string) string {
	if i := strings.Index(p, "/"); i > 0 {
		return p[:i] + "/"
	}
	return RootArea
}

// subareaOf is the directory two levels deep ("src/api/" for "src/api/x.go"); a file directly in a
// top-level directory or in the root has none and is only counted in the top-level view.
func subareaOf(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) < 3 {
		return ""
	}

	return parts[0] + "/" + parts[1] + "/"
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

type codeownersRule struct {
	re     *regexp.Regexp
	owners []string
}

// parseCodeowners reads CODEOWNERS rules in order. A rule without owners is kept: it makes the
// files it matches deliberately unowned, which the last-match-wins evaluation must see.
func parseCodeowners(content []byte) []codeownersRule {
	var rules []codeownersRule
	for _, line := range strings.Split(string(content), "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		re := patternRegexp(fields[0])
		if re == nil {
			continue
		}
		rules = append(rules, codeownersRule{re: re, owners: fields[1:]})
	}

	return rules
}

// patternRegexp converts a gitignore-style CODEOWNERS pattern into a regexp over repo-relative
// paths: "/x" anchors at the root, a pattern with an inner "/" is anchored too, a bare name matches
// at any depth, a trailing "/" means a directory, "*" stays inside one segment and "**" crosses them.
func patternRegexp(pattern string) *regexp.Regexp {
	anchored := strings.HasPrefix(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "/")
	pattern = strings.TrimSuffix(pattern, "/")
	if pattern == "" {
		return nil
	}
	if strings.Contains(pattern, "/") {
		anchored = true
	}

	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		switch ch := pattern[i]; {
		case ch == '*' && i+1 < len(pattern) && pattern[i+1] == '*':
			b.WriteString(".*")
			i++
		case ch == '*':
			b.WriteString("[^/]*")
		case ch == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}

	prefix := "^(.*/)?"
	if anchored {
		prefix = "^"
	}
	re, err := regexp.Compile(prefix + b.String() + "(/.*)?$")
	if err != nil {
		return nil
	}

	return re
}

// coveredShare is the share of distinct changed files whose last matching rule names an owner.
func coveredShare(commits []gitlog.Commit, rules []codeownersRule) *float64 {
	files := map[string]struct{}{}
	for _, c := range commits {
		for _, f := range c.FilesChanged {
			if p := normalizePath(f.Path); p != "" {
				files[p] = struct{}{}
			}
		}
	}
	if len(files) == 0 {
		return nil
	}

	covered := 0
	for p := range files {
		for i := len(rules) - 1; i >= 0; i-- {
			if rules[i].re.MatchString(p) {
				if len(rules[i].owners) > 0 {
					covered++
				}
				break
			}
		}
	}

	share := round2(float64(covered) / float64(len(files)))
	return &share
}
