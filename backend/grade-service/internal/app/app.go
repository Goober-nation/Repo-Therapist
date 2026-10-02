// Package app holds grade-service's core orchestration: given a sourcecraft_uuid, pull that
// repo's S3 _meta/*.json exports and compute its Activity category score. Used by every mode
// (one-shot, --listen) in cmd/grade-service.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"time"

	"repo-therapist/grade-service/internal/appsecwrapper"
	"repo-therapist/grade-service/internal/config"
	"repo-therapist/grade-service/internal/contracts"
	"repo-therapist/grade-service/internal/gitlog"
	"repo-therapist/grade-service/internal/scoring"
	"repo-therapist/grade-service/internal/storage"
)

// publicCategoryIDs / privateCategoryIDs are the Aggregator's public/private split
// (docs/grading/00-overview.md): public = Activity+Documentation+Issues+Code health (65%,
// renormalized via scoring.CombineCategories); private = all six, already summing to 100%.
var (
	publicCategoryIDs  = []string{"activity", "documentation", "issues", "code_health"}
	privateCategoryIDs = []string{"activity", "documentation", "issues", "code_health", "security", "ci_cd"}
)

// GradeOutput is Grade's typed result — a repoID plus every computed category, kept separate from
// contracts.GradingResultEvent (the Aggregator-facing shape) since not every caller (the one-shot
// CLI path) needs the public/private split, just the raw category breakdown.
type GradeOutput struct {
	RepoID     string                            `json:"sourcecraft_uuid"`
	Categories map[string]scoring.CategoryResult `json:"categories"`
	// DocsAdvice is the LLM service's Documentation advice (repo.docs.graded), set only when
	// that reply succeeded. The last item is the "graded without LLM" notice when the grade was
	// algorithmic; it is forwarded like any other item.
	DocsAdvice []contracts.DocsAdvice `json:"docs_advice,omitempty"`
}

// CategoryIDs returns the categories graded for a visibility: the private view is a superset
// that adds the private-only Security and CI/CD categories.
func CategoryIDs(visibility string) []string {
	if visibility == contracts.VisibilityPrivate {
		return privateCategoryIDs
	}
	return publicCategoryIDs
}

// Grade does the actual work: pull this repo's S3 exports, compute every category's score.
// token is the user's SourceCraft PAT for private grading; empty uses the service PAT. Public
// grading skips the private-only categories entirely (no AppSec call, no CI/CD read).
// prefix is the job's S3 export (repo.ready s3_prefix, no trailing slash). tokenDone, when not
// nil, is called once the token is no longer needed — right after the Security call, or before
// reading S3 when there is none.
func Grade(ctx context.Context, repoID, prefix string, cfg *config.Config, token, visibility string, tokenDone func()) (*GradeOutput, error) {
	if tokenDone == nil {
		tokenDone = func() {}
	}
	private := visibility == contracts.VisibilityPrivate
	s3Client, err := newS3Client(ctx)
	if err != nil {
		return nil, fmt.Errorf("connecting to S3: %w", err)
	}

	inputs := scoring.ActivityInputs{}

	if data, err := s3Client.GetObject(ctx, prefix+"/_meta/commits.json"); err != nil {
		log.Printf("commits.json unavailable for %s: %v (Activity's commit-sourced metrics will be no-data)", repoID, err)
	} else {
		var commits []gitlog.Commit
		if err := json.Unmarshal(data, &commits); err != nil {
			return nil, fmt.Errorf("decoding commits.json: %w", err)
		}
		inputs.Commits, inputs.CommitsAvailable = commits, true
	}

	// Issues/milestones are read back from the sourcecraft-wrapper-shaped JSON that
	// repository-service wrote to S3 via its scwrapper client.
	if data, err := s3Client.GetObject(ctx, prefix+"/_meta/issues.json"); err != nil {
		log.Printf("issues.json unavailable for %s: %v (Activity's issue-rate metric will be no-data)", repoID, err)
	} else {
		var issues []scoring.Issue
		if err := json.Unmarshal(data, &issues); err != nil {
			return nil, fmt.Errorf("decoding issues.json: %w", err)
		}
		inputs.Issues, inputs.IssuesAvailable = issues, true
	}

	if data, err := s3Client.GetObject(ctx, prefix+"/_meta/milestones.json"); err != nil {
		log.Printf("milestones.json unavailable for %s: %v (Activity's milestone metrics will be no-data)", repoID, err)
	} else {
		var milestones []scoring.Milestone
		if err := json.Unmarshal(data, &milestones); err != nil {
			return nil, fmt.Errorf("decoding milestones.json: %w", err)
		}
		inputs.Milestones, inputs.MilestonesAvailable = milestones, true
	}

	activityCfg, ok := cfg.Categories["activity"]
	if !ok {
		return nil, fmt.Errorf("config has no \"activity\" category")
	}
	activityResult := scoring.ComputeCategory("activity", activityCfg, inputs, time.Now())

	issuesResult := scoring.CategoryResult{ID: "issues", NoData: true}
	if issuesCfg, ok := cfg.Categories["issues"]; ok && issuesCfg.Status == config.StatusCalculated {
		issuesResult = scoring.ComputeCategory("issues", issuesCfg, inputs, time.Now())
	}

	securityResult := scoring.CategoryResult{ID: "security", NoData: true}
	if securityCfg, ok := cfg.Categories["security"]; private && ok && securityCfg.Status == config.StatusCalculated {
		securityResult = gradeSecurity(ctx, repoID, token)
	}
	tokenDone()

	codeHealthInputs := scoring.CodeHealthInputs{}
	if data, err := s3Client.GetObject(ctx, prefix+"/_meta/todo-markers.json"); err != nil {
		log.Printf("todo-markers.json unavailable for %s: %v (Code health category will be no-data)", repoID, err)
	} else {
		var export struct {
			Markers  []scoring.Marker `json:"markers"`
			TotalLOC int              `json:"total_loc"`
		}
		if err := json.Unmarshal(data, &export); err != nil {
			return nil, fmt.Errorf("decoding todo-markers.json: %w", err)
		}
		codeHealthInputs = scoring.CodeHealthInputs{Markers: export.Markers, TotalLOC: export.TotalLOC, Available: true}
	}

	codeHealthResult := scoring.CategoryResult{ID: "code_health", NoData: true}
	if codeHealthCfg, ok := cfg.Categories["code_health"]; ok && codeHealthCfg.Status == config.StatusCalculated {
		codeHealthResult = scoring.ComputeCodeHealth(codeHealthCfg, codeHealthInputs)
	}

	categories := map[string]scoring.CategoryResult{
		"activity":    activityResult,
		"issues":      issuesResult,
		"code_health": codeHealthResult,
	}
	if !private {
		return &GradeOutput{RepoID: repoID, Categories: categories}, nil
	}

	ciInputs := scoring.CICDInputs{}
	if data, err := s3Client.GetObject(ctx, prefix+"/_meta/ci-runs.json"); err != nil {
		log.Printf("ci-runs.json unavailable for %s: %v (CI/CD category will be no-data)", repoID, err)
	} else {
		if err := json.Unmarshal(data, &ciInputs.Export); err != nil {
			return nil, fmt.Errorf("decoding ci-runs.json: %w", err)
		}
		ciInputs.Available = true
		ciInputs.ConfigContents = make(map[string][]byte, len(ciInputs.Export.ConfigFiles))
		for _, path := range ciInputs.Export.ConfigFiles {
			data, err := s3Client.GetObject(ctx, prefix+"/"+path)
			if err != nil {
				log.Printf("CI config %s unavailable for %s: %v (no-op pipeline check skipped)", path, repoID, err)
				continue
			}
			ciInputs.ConfigContents[path] = data
		}
	}

	ciResult := scoring.CategoryResult{ID: "ci_cd", NoData: true}
	if ciCfg, ok := cfg.Categories["ci_cd"]; ok && ciCfg.Status == config.StatusCalculated {
		ciResult = scoring.ComputeCICD(ciCfg, ciInputs)
	}

	categories["security"] = securityResult
	categories["ci_cd"] = ciResult
	return &GradeOutput{RepoID: repoID, Categories: categories}, nil
}

func newS3Client(ctx context.Context) (*storage.Client, error) {
	return storage.NewClient(ctx, storage.Config{
		Endpoint:  EnvOrDefault("S3_ENDPOINT", "localhost:9000"),
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
		Bucket:    EnvOrDefault("S3_BUCKET", "repo-content"),
		UseSSL:    false,
	})
}

// DeleteRepoData removes one job's S3 export (prefix, e.g. repos/<uuid>/<job_id>) once nothing
// reads it any more, and returns how many objects were removed.
func DeleteRepoData(ctx context.Context, prefix string) (int, error) {
	if !strings.HasPrefix(prefix, "repos/") || strings.Count(strings.Trim(prefix, "/"), "/") < 1 {
		return 0, fmt.Errorf("refusing to delete unexpected S3 prefix %q", prefix)
	}
	s3Client, err := newS3Client(ctx)
	if err != nil {
		return 0, fmt.Errorf("connecting to S3: %w", err)
	}
	return s3Client.DeletePrefix(ctx, strings.TrimSuffix(prefix, "/")+"/")
}

// BuildGradingResult turns a GradeOutput into the Aggregator-facing contracts.GradingResultEvent
// for one visibility: that side's score, category scores, every recommendation (in category
// order, Documentation last) and missing_data; the other side stays null. catalog is the recommendation
// text lookup (config.LoadRecommendations); an empty catalog just means every metric that would
// otherwise get a recommendation is silently skipped (see scoring.BuildRecommendations), not an
// error.
func BuildGradingResult(out *GradeOutput, cfg *config.Config, catalog map[string]config.RecommendationEntry, visibility string, now time.Time) (contracts.GradingResultEvent, error) {
	weights := make(map[string]float64, len(cfg.Categories))
	for id, cat := range cfg.Categories {
		weights[id] = cat.Weight
	}

	// KNOWN LIMITATION (2026-09-25): category_scores' JSON shape is not uniform. A category Grade
	// attempted and failed (e.g. Security with no PAT/connection) gets an explicit
	// {"no_data": true} entry; a not_calculated category (Documentation today) never gets
	// an entry in out.Categories at all, so it's simply absent from the JSON — no placeholder.
	// Deliberately not "fixed" by injecting placeholders for every category: an extra
	// {no_data: true} object per absent category was tried and rejected — more objects with the
	// same shape as a real category is a bigger risk to a consumer that isn't defensive about it
	// than a key just being missing. MissingData below is the authoritative "what's not computed"
	// signal regardless of what category_scores does or doesn't contain.
	ids := CategoryIDs(visibility)
	score, noData := scoring.CombineCategories(out.Categories, ids, weights)
	if limit, ok := scoring.SecurityCap(out.Categories, ids); ok && !noData {
		score = math.Min(score, limit)
	}

	categoryJSON, err := json.Marshal(scoring.SubsetCategories(out.Categories, ids))
	if err != nil {
		return contracts.GradingResultEvent{}, fmt.Errorf("marshaling category scores: %w", err)
	}

	pool := scoring.BuildRecommendations(out.Categories, ids, catalog)
	pool = append(pool, scoring.DocsRecommendations(out.Categories, ids, out.DocsAdvice)...)
	recsJSON, err := json.Marshal(toContractRecommendations(pool))
	if err != nil {
		return contracts.GradingResultEvent{}, fmt.Errorf("marshaling recommendations: %w", err)
	}

	// math.Round, not a bare int16(x) truncation — a bare cast rounds every score down (never up),
	// silently biasing every headline score by up to a full point (found reviewing a real pipeline
	// run, 2026-09-25).
	var scorePtr *int16
	if !noData {
		v := int16(math.Round(score))
		scorePtr = &v
	}

	result := contracts.GradingResultEvent{
		SourcecraftUUID: out.RepoID,
		GradeMode:       visibility,
		Score:           scorePtr,
		CategoryScores:  categoryJSON,
		Advice:          recsJSON,
		MissingData:     scoring.MissingCategories(out.Categories, ids),
		GradedAt:        now,
	}
	return result, nil
}

// GradingFailure is the result published when a repo couldn't be graded, so the Aggregator can
// release its lock instead of waiting on a result that will never come.
func GradingFailure(sourcecraftUUID, visibility, code string, now time.Time) contracts.GradingResultEvent {
	return contracts.GradingResultEvent{SourcecraftUUID: sourcecraftUUID, GradeMode: visibility, GradedAt: now, Error: code}
}

func toContractRecommendations(recs []scoring.Recommendation) []contracts.Recommendation {
	out := make([]contracts.Recommendation, len(recs))
	for i, r := range recs {
		out[i] = contracts.Recommendation{CategoryID: r.CategoryID, MetricID: r.MetricID, Priority: r.Priority, Text: r.Text}
	}
	return out
}

// gradeSecurity calls security-wrapper-service for repoID's AppSec data and scores it. Any
// failure to reach the wrapper is logged and treated as no-data, same as a missing S3 export —
// one category's failure shouldn't fail the whole grading run.
func gradeSecurity(ctx context.Context, repoID, token string) scoring.CategoryResult {
	pat := token
	if pat == "" {
		pat = os.Getenv("SOURCECRAFT_PAT")
	}
	if pat == "" {
		log.Printf("no request token and SOURCECRAFT_PAT not set: Security category will be no-data for %s", repoID)
		return scoring.CategoryResult{ID: "security", NoData: true}
	}

	client, err := appsecwrapper.Dial(EnvOrDefault("SECURITY_WRAPPER_ADDR", "localhost:8085"), pat)
	if err != nil {
		log.Printf("dialing security-wrapper-service for %s: %v (Security category will be no-data)", repoID, err)
		return scoring.CategoryResult{ID: "security", NoData: true}
	}
	defer client.Close()

	result, err := client.GetAppsecResult(ctx, repoID)
	if err != nil {
		log.Printf("GetAppsecResult failed for %s: %v (Security category will be no-data)", repoID, err)
		return scoring.CategoryResult{ID: "security", NoData: true}
	}

	return scoring.ComputeSecurity(result, time.Now())
}

func PrintResult(out *GradeOutput) {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		log.Printf("marshaling result: %v", err)
		return
	}
	fmt.Println(string(data))
}

func EnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
