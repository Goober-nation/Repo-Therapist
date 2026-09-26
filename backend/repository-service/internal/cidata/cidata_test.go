package cidata

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"repo-therapist/repository-service/internal/scwrapper"
)

func TestDetectConfigs(t *testing.T) {
	dir := t.TempDir()
	if got := DetectConfigs(dir); len(got) != 0 {
		t.Fatalf("empty repo: got %v, want none", got)
	}

	// A directory at the config path is not a config file.
	if err := os.MkdirAll(filepath.Join(dir, ".sourcecraft", "ci.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DetectConfigs(dir); len(got) != 0 {
		t.Fatalf("directory at config path: got %v, want none", got)
	}

	dir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".sourcecraft"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".sourcecraft", "ci.yaml"), []byte("workflows: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := DetectConfigs(dir)
	if len(got) != 1 || got[0] != ".sourcecraft/ci.yaml" {
		t.Fatalf("got %v, want [.sourcecraft/ci.yaml]", got)
	}
}

func TestSelectRuns(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	at := func(daysAgo int) string { return now.AddDate(0, 0, -daysAgo).Format(time.RFC3339Nano) }

	runs := []scwrapper.CIRun{
		{Slug: "old", CreatedAt: at(200)},
		{Slug: "b", CreatedAt: at(10)},
		{Slug: "a", CreatedAt: at(1)},
		{Slug: "nodate", CreatedAt: ""},
		{Slug: "c", CreatedAt: at(179)},
	}

	got := SelectRuns(runs, now, 180, 25)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %d runs, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Slug != w {
			t.Errorf("run %d: got %q, want %q (newest first, outside-window and undated dropped)", i, got[i].Slug, w)
		}
	}

	capped := SelectRuns(runs, now, 180, 2)
	if len(capped) != 2 || capped[0].Slug != "a" || capped[1].Slug != "b" {
		t.Fatalf("cap 2: got %+v, want newest two [a b]", capped)
	}
}
