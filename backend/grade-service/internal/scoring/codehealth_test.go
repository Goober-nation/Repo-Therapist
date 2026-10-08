package scoring

import (
	"strings"
	"testing"

	"repo-therapist/grade-service/internal/config"
)

func codeHealthCfg() config.CategoryConfig {
	return config.CategoryConfig{
		Status: config.StatusCalculated,
		Metrics: []config.MetricConfig{
			{
				ID: "median_marker_age", Status: config.StatusCalculated, Weight: 0.25,
				Aggregation: config.AggregationConfig{Type: "median_marker_age", MinSampleSize: 5},
				Formula:     config.FormulaConfig{Type: "linear_clamp", Direction: "-", Critical: 293, Healthy: 183},
			},
			{
				ID: "density_per_kloc", Status: config.StatusCalculated, Weight: 0.30,
				Aggregation: config.AggregationConfig{Type: "density_per_kloc"},
				Formula:     config.FormulaConfig{Type: "linear_clamp", Direction: "-", Critical: 20, Healthy: 1},
			},
			{
				ID: "share_stale_markers", Status: config.StatusCalculated, Weight: 0.25,
				Aggregation: config.AggregationConfig{Type: "share_stale_markers", MinSampleSize: 5, StalenessDays: 293},
				Formula:     config.FormulaConfig{Type: "linear_clamp", Direction: "-", Critical: 0.5, Healthy: 0.05},
			},
			{
				ID: "worst_file_concentration", Status: config.StatusCalculated, Weight: 0.20,
				Aggregation: config.AggregationConfig{Type: "worst_file_concentration", MinSampleSize: 5},
				Formula:     config.FormulaConfig{Type: "linear_clamp", Direction: "-", Critical: 0.6, Healthy: 0.15},
			},
		},
	}
}

func TestComputeCodeHealth_Unavailable(t *testing.T) {
	result := ComputeCodeHealth(codeHealthCfg(), CodeHealthInputs{Available: false})
	if !result.NoData {
		t.Fatal("expected NoData when todo-markers.json wasn't available")
	}
}

func TestComputeCodeHealth_StatusNotCalculated(t *testing.T) {
	cfg := codeHealthCfg()
	cfg.Status = config.StatusNotCalculated
	result := ComputeCodeHealth(cfg, CodeHealthInputs{Available: true, TotalLOC: 1000})
	if !result.NoData {
		t.Fatal("expected NoData when category status is not_calculated")
	}
}

// TestComputeCodeHealth_BelowSampleFloor exercises a repo with real content (density stays
// scoreable) but too few markers (3, below min_sample_size 5) for the other three metrics —
// they must all go NoData, and the category score must renormalize around density alone.
func TestComputeCodeHealth_BelowSampleFloor(t *testing.T) {
	inputs := CodeHealthInputs{
		Available: true,
		TotalLOC:  10000,
		Markers: []Marker{
			{File: "a.go", AgeDays: 10},
			{File: "a.go", AgeDays: 20},
			{File: "b.go", AgeDays: 30},
		},
	}
	result := ComputeCodeHealth(codeHealthCfg(), inputs)
	if result.NoData {
		t.Fatal("category should not be NoData: density_per_kloc has no sample floor")
	}

	byID := make(map[string]MetricResult)
	for _, m := range result.Metrics {
		byID[m.ID] = m
	}
	if !byID["median_marker_age"].NoData {
		t.Error("median_marker_age should be NoData below min_sample_size")
	}
	if !byID["share_stale_markers"].NoData {
		t.Error("share_stale_markers should be NoData below min_sample_size")
	}
	if !byID["worst_file_concentration"].NoData {
		t.Error("worst_file_concentration should be NoData below min_sample_size")
	}
	density := byID["density_per_kloc"]
	if density.NoData {
		t.Fatal("density_per_kloc should be computed even below the marker-count floor")
	}
	wantDensity := 3.0 / (10000.0 / 1000.0) // 0.3 markers/kLOC
	if density.RawValue != wantDensity {
		t.Errorf("density_per_kloc raw = %v, want %v", density.RawValue, wantDensity)
	}
	// Only density has weight in the renormalized score, so its score alone (as a fraction, not
	// its raw value) determines the category score.
	if result.Score != density.Score*100 {
		t.Errorf("category score = %v, want %v (density alone, renormalized)", result.Score, density.Score*100)
	}
}

func TestComputeCodeHealth_ZeroMarkersRealZeroDensity(t *testing.T) {
	inputs := CodeHealthInputs{Available: true, TotalLOC: 5000, Markers: nil}
	result := ComputeCodeHealth(codeHealthCfg(), inputs)
	if result.NoData {
		t.Fatal("0 markers over real LOC should be a real, scoreable result (density=0), not NoData")
	}
	byID := make(map[string]MetricResult)
	for _, m := range result.Metrics {
		byID[m.ID] = m
	}
	density := byID["density_per_kloc"]
	if density.NoData || density.RawValue != 0 {
		t.Errorf("density_per_kloc with 0 markers = %+v, want raw 0, not NoData", density)
	}
	if density.Score != 1.0 {
		t.Errorf("density_per_kloc score with 0 markers/kLOC = %v, want 1.0 (healthy)", density.Score)
	}
}

func TestComputeCodeHealth_ZeroTotalLOC(t *testing.T) {
	inputs := CodeHealthInputs{Available: true, TotalLOC: 0, Markers: nil}
	result := ComputeCodeHealth(codeHealthCfg(), inputs)
	if !result.NoData {
		t.Fatal("expected whole category NoData: TotalLOC=0 means every metric is undefined (0/4 have data)")
	}
}

// TestComputeCodeHealth_SizeInvariance is the actual regression test for the bug the user
// flagged: cloning share_stale_markers/density's ratio design should score two repos with
// identical TODO *rates* roughly the same regardless of how many total markers each has, unlike
// a raw max ("oldest marker age") would.
func TestComputeCodeHealth_SizeInvariance(t *testing.T) {
	// Small repo: 6 markers, 1 stale (~16.7%).
	small := CodeHealthInputs{Available: true, TotalLOC: 6000, Markers: repeatMarkers(5, 10, 1, 400)}
	// Large repo: 60 markers, same 1-in-6 stale ratio (10 stale), same density per kLOC.
	large := CodeHealthInputs{Available: true, TotalLOC: 60000, Markers: repeatMarkers(50, 10, 10, 400)}

	smallResult := ComputeCodeHealth(codeHealthCfg(), small)
	largeResult := ComputeCodeHealth(codeHealthCfg(), large)

	if smallResult.NoData || largeResult.NoData {
		t.Fatalf("expected both scoreable: small.NoData=%v large.NoData=%v", smallResult.NoData, largeResult.NoData)
	}
	diff := smallResult.Score - largeResult.Score
	if diff < 0 {
		diff = -diff
	}
	if diff > 1.0 {
		t.Errorf("small (%v) and large (%v) repo scores differ by %v for the same underlying rates — share_stale_markers/density should be size-invariant", smallResult.Score, largeResult.Score, diff)
	}
}

// TestComputeCodeHealth_ExcludesCappedMarkers is the regression test for the real-world bug found
// testing against postgres/postgres, divkit/divkit, and akuleshov7/diktat (2026-09-25): a marker
// whose git-blame walk hit the shallow-clone boundary gets Marker.AgeCapped=true and AgeDays set
// to the clone's history window, not a real age. On those repos 90-100% of markers were capped,
// which silently turned "we don't know this marker's age" into "this marker is very old" for
// median_marker_age and share_stale_markers — exactly the no-data-treated-as-bad failure this
// grading policy exists to avoid. Capped markers must be excluded from both metrics.
func TestComputeCodeHealth_ExcludesCappedMarkers(t *testing.T) {
	markers := []Marker{
		// 5 capped markers, all reporting the same artificial 365-day age — if these leaked into
		// the stats, median_marker_age would read 365 (critical) and share_stale_markers 100%.
		{File: "old.go", AgeDays: 365, AgeCapped: true},
		{File: "old.go", AgeDays: 365, AgeCapped: true},
		{File: "old.go", AgeDays: 365, AgeCapped: true},
		{File: "old.go", AgeDays: 365, AgeCapped: true},
		{File: "old.go", AgeDays: 365, AgeCapped: true},
		// 5 uncapped markers with real, known, fresh ages — these are the only data that should
		// feed median_marker_age/share_stale_markers.
		{File: "new.go", AgeDays: 5, AgeCapped: false},
		{File: "new.go", AgeDays: 5, AgeCapped: false},
		{File: "new.go", AgeDays: 5, AgeCapped: false},
		{File: "new.go", AgeDays: 5, AgeCapped: false},
		{File: "new.go", AgeDays: 5, AgeCapped: false},
	}
	inputs := CodeHealthInputs{Available: true, TotalLOC: 10000, Markers: markers}
	result := ComputeCodeHealth(codeHealthCfg(), inputs)

	byID := make(map[string]MetricResult)
	for _, m := range result.Metrics {
		byID[m.ID] = m
	}

	medianAge := byID["median_marker_age"]
	if medianAge.NoData {
		t.Fatal("median_marker_age should be computed: 5 uncapped markers clears min_sample_size")
	}
	if medianAge.RawValue != 5 {
		t.Errorf("median_marker_age raw = %v, want 5 (capped markers must not pollute the median)", medianAge.RawValue)
	}

	shareStale := byID["share_stale_markers"]
	if shareStale.NoData {
		t.Fatal("share_stale_markers should be computed: 5 uncapped markers clears min_sample_size")
	}
	if shareStale.RawValue != 0 {
		t.Errorf("share_stale_markers raw = %v, want 0 (all 5 uncapped markers are fresh; capped ones must not count as stale)", shareStale.RawValue)
	}

	// worst_file_concentration and density_per_kloc are age-independent and must still see all
	// 10 markers (2 files x 5 markers each -> 50% concentration), capped or not.
	worstFile := byID["worst_file_concentration"]
	if worstFile.NoData || worstFile.RawValue != 0.5 {
		t.Errorf("worst_file_concentration = %+v, want raw 0.5 over all 10 markers regardless of capping", worstFile)
	}
}

// TestComputeCodeHealth_AllMarkersCappedGoesNoData verifies the fallback: if every marker is
// capped, there are zero usable data points for the age-sensitive metrics, not "10 markers worth
// of data" — they must go NoData rather than silently scoring off fabricated ages.
func TestComputeCodeHealth_AllMarkersCappedGoesNoData(t *testing.T) {
	markers := make([]Marker, 10)
	for i := range markers {
		markers[i] = Marker{File: "f.go", AgeDays: 365, AgeCapped: true}
	}
	inputs := CodeHealthInputs{Available: true, TotalLOC: 10000, Markers: markers}
	result := ComputeCodeHealth(codeHealthCfg(), inputs)

	byID := make(map[string]MetricResult)
	for _, m := range result.Metrics {
		byID[m.ID] = m
	}
	if !byID["median_marker_age"].NoData {
		t.Error("median_marker_age should be NoData when every marker is capped (0 usable data points)")
	}
	if !byID["share_stale_markers"].NoData {
		t.Error("share_stale_markers should be NoData when every marker is capped (0 usable data points)")
	}
	// worst_file_concentration is unaffected by capping.
	if byID["worst_file_concentration"].NoData {
		t.Error("worst_file_concentration should still compute: capping doesn't affect it")
	}
}

func repeatMarkers(freshCount int, freshAge float64, staleCount int, staleAge float64) []Marker {
	markers := make([]Marker, 0, freshCount+staleCount)
	for i := 0; i < freshCount; i++ {
		markers = append(markers, Marker{File: "f.go", AgeDays: freshAge})
	}
	for i := 0; i < staleCount; i++ {
		markers = append(markers, Marker{File: "f.go", AgeDays: staleAge})
	}
	return markers
}

func unusedShareCfg() config.CategoryConfig {
	return config.CategoryConfig{
		Status: config.StatusCalculated,
		Metrics: []config.MetricConfig{{
			ID: "unused_private_share", Status: config.StatusCalculated, Weight: 0.40,
			Aggregation: config.AggregationConfig{Type: "unused_private_share", MinSampleSize: 10},
			Formula:     config.FormulaConfig{Type: "linear_clamp", Direction: "-", Critical: 0.08, Healthy: 0.005},
		}},
	}
}

func TestUnusedPrivateShare(t *testing.T) {
	dead := &DeadCodeExport{PrivateTotal: 100, UnusedTotal: 4, Unused: []DeadFunction{
		{File: "a.go", Name: "x"}, {File: "b.go", Name: "y"}, {File: "b.go", Name: "z"}, {File: "c.go", Name: "w"},
	}}
	m := ComputeCodeHealth(unusedShareCfg(), CodeHealthInputs{Available: true, DeadCode: dead}).Metrics[0]
	if m.NoData || m.RawValue != 0.04 {
		t.Fatalf("got %+v, want raw 0.04", m)
	}
	want := "Найдено 4 неиспользуемые приватные функции в файлах b.go, a.go, c.go. " +
		"Удалите мёртвый код или используйте его — он усложняет чтение и сопровождение."
	if m.Detail != want {
		t.Fatalf("detail = %q", m.Detail)
	}
}

func TestUnusedPrivateShare_NoData(t *testing.T) {
	for name, dead := range map[string]*DeadCodeExport{
		"missing export": nil,
		"below sample":   {PrivateTotal: 9},
	} {
		m := ComputeCodeHealth(unusedShareCfg(), CodeHealthInputs{Available: true, DeadCode: dead}).Metrics[0]
		if !m.NoData {
			t.Errorf("%s: expected NoData", name)
		}
	}
	m := ComputeCodeHealth(unusedShareCfg(), CodeHealthInputs{Available: true, DeadCode: &DeadCodeExport{PrivateTotal: 50}}).Metrics[0]
	if m.NoData || m.Score != 1 || m.Detail != "" {
		t.Fatalf("clean repo: got %+v, want perfect score, no detail", m)
	}
}

func TestDeadCodeAdvice(t *testing.T) {
	one := deadCodeAdvice(&DeadCodeExport{UnusedTotal: 1, Unused: []DeadFunction{{File: "x.rs"}}})
	if !strings.HasPrefix(one, "Найдено 1 неиспользуемая приватная функция в файле x.rs. ") {
		t.Errorf("one: %q", one)
	}
	var many []DeadFunction
	for _, f := range []string{"a", "b", "c", "d"} {
		many = append(many, DeadFunction{File: f})
	}
	got := deadCodeAdvice(&DeadCodeExport{UnusedTotal: 11, Unused: many})
	if !strings.HasPrefix(got, "Найдено 11 неиспользуемых приватных функций в файлах a, b, c и других. ") {
		t.Errorf("many: %q", got)
	}
	for n, want := range map[int]string{1: "1", 2: "2", 5: "5", 21: "1", 22: "2", 111: "5", 112: "5"} {
		if g := ruPlural(n, "1", "2", "5"); g != want {
			t.Errorf("ruPlural(%d) = %s, want %s", n, g, want)
		}
	}
}

func TestRecommendationUsesMetricDetail(t *testing.T) {
	cats := map[string]CategoryResult{"code_health": {ID: "code_health", Metrics: []MetricResult{
		{ID: "unused_private_share", Score: 0.5, Detail: "конкретный текст"},
		{ID: "density_per_kloc", Score: 0.5},
	}}}
	catalog := map[string]config.RecommendationEntry{
		"unused_private_share": {Priority: "medium", Text: "общий текст"},
		"density_per_kloc":     {Priority: "medium", Text: "плотность"},
	}
	recs := BuildRecommendations(cats, []string{"code_health"}, catalog)
	if len(recs) != 2 || recs[0].Text != "конкретный текст" || recs[1].Text != "плотность" {
		t.Fatalf("recs = %+v", recs)
	}
}
