//go:build live

package scoring

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestComputeCICD_LiveMockRepos scores the real mock repos listed in docs/ci-mock-repos.json
// against the real SourceCraft API. Run with:
//
//	SOURCECRAFT_PAT=... go test -tags live -run LiveMockRepos ./internal/scoring/
//
// It builds the ci-runs.json export straight from the REST API (trees for the config file,
// cicd/runs for runs) rather than through sourcecraft-wrapper + repository-service, so it checks
// scoring against real payloads, not the whole pipeline — see docs/testing.md for the end-to-end
// run. Mirrors repository-service's cidata export rules: newest 25 runs within 180 days, and
// SourceCraft's 1970-01-01 placeholder dates blanked.
func TestComputeCICD_LiveMockRepos(t *testing.T) {
	pat := os.Getenv("SOURCECRAFT_PAT")
	if pat == "" {
		t.Skip("SOURCECRAFT_PAT not set")
	}

	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "docs", "ci-mock-repos.json"))
	if err != nil {
		t.Fatal(err)
	}
	var registry struct {
		Org   string `json:"org"`
		Repos []struct {
			Repo     string `json:"repo"`
			UUID     string `json:"sourcecraft_uuid"`
			Expected struct {
				NoData bool    `json:"no_data"`
				Score  float64 `json:"score"`
			} `json:"expected"`
		} `json:"repos"`
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		t.Fatal(err)
	}

	cfg := ciCfg(t)
	for _, r := range registry.Repos {
		t.Run(r.Repo, func(t *testing.T) {
			ex := liveCIExport(t, pat, registry.Org, r.Repo, r.UUID)
			got := ComputeCICD(cfg, CICDInputs{Export: ex, Available: true})
			t.Logf("config=%v runs=%d score=%.2f no_data=%v", ex.ConfigFiles, len(ex.Runs), got.Score, got.NoData)
			if got.NoData != r.Expected.NoData {
				t.Fatalf("NoData = %v, want %v (%+v)", got.NoData, r.Expected.NoData, got)
			}
			if !got.NoData && math.Abs(got.Score-r.Expected.Score) > 0.5 {
				t.Errorf("Score = %.2f, want %.0f (%+v)", got.Score, r.Expected.Score, got.Metrics)
			}
		})
	}
}

func liveCIExport(t *testing.T, pat, org, repo, uuid string) CIExport {
	t.Helper()
	ex := CIExport{ConfigFiles: []string{}, RunsAvailable: true}

	status := liveGet(t, pat, fmt.Sprintf("https://api.sourcecraft.tech/repos/%s/%s/trees?path=.sourcecraft", org, repo), nil)
	if status == http.StatusOK {
		var tree struct {
			Trees []struct {
				Path string `json:"path"`
				Type string `json:"type"`
			} `json:"trees"`
		}
		liveGet(t, pat, fmt.Sprintf("https://api.sourcecraft.tech/repos/%s/%s/trees?path=.sourcecraft", org, repo), &tree)
		for _, e := range tree.Trees {
			if e.Path == ".sourcecraft/ci.yaml" && e.Type == "file" {
				ex.ConfigFiles = append(ex.ConfigFiles, e.Path)
			}
		}
	}

	var runs struct {
		Runs []struct {
			Slug      string `json:"slug"`
			Status    string `json:"status"`
			EventType string `json:"event_type"`
			Dates     struct {
				CreatedAt  time.Time `json:"created_at"`
				StartedAt  time.Time `json:"started_at"`
				FinishedAt time.Time `json:"finished_at"`
			} `json:"dates"`
		} `json:"runs"`
	}
	if s := liveGet(t, pat, "https://api.sourcecraft.tech/repos/id:"+uuid+"/cicd/runs", &runs); s != http.StatusOK {
		ex.RunsAvailable = false
		return ex
	}
	ts := func(v time.Time) string {
		if v.Unix() <= 0 {
			return ""
		}
		return v.Format(time.RFC3339Nano)
	}
	cutoff := time.Now().AddDate(0, 0, -180)
	for _, r := range runs.Runs { // API order is newest first
		if r.Dates.CreatedAt.Before(cutoff) || len(ex.Runs) == 25 {
			continue
		}
		switch r.Status {
		case "created", "prepared", "processing":
			t.Fatalf("run %s is still %s — wait for queued runs to finish and re-run", r.Slug, r.Status)
		}
		ex.Runs = append(ex.Runs, CIRun{
			Slug: r.Slug, Status: r.Status, EventType: r.EventType,
			CreatedAt: ts(r.Dates.CreatedAt), StartedAt: ts(r.Dates.StartedAt), FinishedAt: ts(r.Dates.FinishedAt),
		})
	}
	return ex
}

// liveGet GETs url with the PAT and, on 200 with a non-nil out, decodes the body into it.
func liveGet(t *testing.T, pat, url string, out any) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK && out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode
}
