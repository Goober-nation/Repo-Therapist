package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCatalog(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "recommendations.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadRecommendationsReadsPriorityAndText(t *testing.T) {
	catalog, err := LoadRecommendations(writeCatalog(t, "m1: {priority: critical, text: \"  Исправьте.  \"}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := catalog["m1"]; got != (RecommendationEntry{Priority: "critical", Text: "Исправьте."}) {
		t.Fatalf("m1 = %+v", got)
	}
}

func TestLoadRecommendationsRejectsBadEntries(t *testing.T) {
	cases := map[string]string{
		"unknown priority": "m1: {priority: urgent, text: x}\n",
		"no priority":      "m1: {text: x}\n",
		"no text":          "m1: {priority: low}\n",
		"old tagged form":  "m1: \"[low] x\"\n",
	}
	for name, body := range cases {
		if _, err := LoadRecommendations(writeCatalog(t, body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestLoadRecommendationsMissingFileIsEmpty(t *testing.T) {
	catalog, err := LoadRecommendations(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil || len(catalog) != 0 {
		t.Fatalf("catalog=%v err=%v, want empty and no error", catalog, err)
	}
}
