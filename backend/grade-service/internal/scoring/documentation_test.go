package scoring

import (
	"testing"

	"repo-therapist/grade-service/internal/contracts"
)

func TestComputeDocumentationNilIsNoData(t *testing.T) {
	if r := ComputeDocumentation(nil); !r.NoData || r.ID != "documentation" {
		t.Fatalf("got %+v, want documentation no-data", r)
	}
}

func TestComputeDocumentationTakesLLMGrade(t *testing.T) {
	r := ComputeDocumentation(&contracts.DocsGraded{Grade: 84, Subgrades: map[string]int{"required_files": 25, "purpose": 100}})
	if r.NoData || r.Score != 84 {
		t.Fatalf("got %+v, want score 84", r)
	}
	if len(r.Metrics) != 2 || r.Metrics[0].ID != "purpose" || r.Metrics[1].Score != 0.25 {
		t.Fatalf("unexpected metrics %+v", r.Metrics)
	}
}
