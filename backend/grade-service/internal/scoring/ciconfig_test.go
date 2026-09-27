package scoring

import "testing"

func TestIsNoOpCIConfig(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want bool
	}{
		{"echo only", `
workflows:
  verify:
    tasks:
      - name: build
        cubes:
          - name: fake
            script:
              - echo "building..."
              - echo done && exit 0
`, true},
		{"multi-line string script, sleep and comments", `
workflows:
  w:
    tasks:
      - cubes:
          - script: |
              # pretend to test
              sleep 30
              true
`, true},
		{"real build step", `
workflows:
  verify:
    tasks:
      - cubes:
          - script:
              - echo start
              - ./gradlew test
`, false},
		{"trivial command chained with real one", `
workflows:
  w:
    tasks:
      - cubes:
          - script: ["echo hi; go test ./..."]
`, false},
		{"non-zero exit is not a no-op", `
workflows:
  w:
    tasks:
      - cubes:
          - script: ["exit 1"]
`, false},
		{"image-only cube counts as real work", `
workflows:
  w:
    tasks:
      - cubes:
          - script: ["echo hi"]
          - name: deploy
            image: some/deployer:latest
`, false},
		{"task without cubes: no verdict", `
workflows:
  w:
    tasks:
      - name: templated
`, false},
		{"no workflows: no verdict", `on: {}`, false},
		{"invalid yaml: no verdict", "workflows: [unclosed", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsNoOpCIConfig([]byte(tc.yaml)); got != tc.want {
				t.Errorf("IsNoOpCIConfig = %v, want %v", got, tc.want)
			}
		})
	}
}

const noOpYAML = "workflows:\n  w:\n    tasks:\n      - cubes:\n          - script: [\"echo ok\"]\n"

// A no-op config zeroes the whole category even with a perfect run history, and surfaces the
// ci_noop_pipeline marker metric (which drives its recommendation).
func TestComputeCICD_NoOpPipelineZeroesCategory(t *testing.T) {
	in := loadCIFixture(t, "green")
	in.ConfigContents = map[string][]byte{".sourcecraft/ci.yaml": []byte(noOpYAML)}
	got := ComputeCICD(ciCfg(t), in)
	if got.NoData || got.Score != 0 {
		t.Fatalf("got score %v (no_data %v), want 0", got.Score, got.NoData)
	}
	if len(got.Metrics) != 1 || got.Metrics[0].ID != noOpMetricID || got.Metrics[0].Score != 0 {
		t.Errorf("metrics = %+v, want single %s at score 0", got.Metrics, noOpMetricID)
	}
	recs := BuildRecommendations(map[string]CategoryResult{"ci_cd": got}, []string{"ci_cd"},
		map[string]string{noOpMetricID: "fix it"})
	if len(recs) != 1 || recs[0].MetricID != noOpMetricID {
		t.Errorf("recommendations = %+v, want one for %s", recs, noOpMetricID)
	}
}

// Unreadable config content (not in ConfigContents) skips the check: normal scoring applies.
func TestComputeCICD_UnreadConfigSkipsNoOpCheck(t *testing.T) {
	got := ComputeCICD(ciCfg(t), loadCIFixture(t, "green"))
	if got.NoData || got.Score == 0 {
		t.Fatalf("got score %v (no_data %v), want normal non-zero score", got.Score, got.NoData)
	}
}

// Runs unreadable still means NoData: the no-op gate only applies where CI/CD is graded at all.
func TestComputeCICD_NoOpDoesNotOverrideRunsDenied(t *testing.T) {
	in := loadCIFixture(t, "runs_denied")
	in.ConfigContents = map[string][]byte{".sourcecraft/ci.yaml": []byte(noOpYAML)}
	if got := ComputeCICD(ciCfg(t), in); !got.NoData {
		t.Errorf("got %+v, want NoData", got)
	}
}
