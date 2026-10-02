package scoring

import (
	"encoding/json"
	"testing"

	"repo-therapist/grade-service/internal/contracts"
)

func TestComputeDocumentationNilIsNoData(t *testing.T) {
	if r := ComputeDocumentation(nil); !r.NoData || r.ID != "documentation" {
		t.Fatalf("got %+v, want documentation no-data", r)
	}
}

func TestComputeDocumentationTakesLLMGrade(t *testing.T) {
	r := ComputeDocumentation(&contracts.DocsGraded{Grade: intPtr(84), Subgrades: map[string]int{"required_files": 25, "purpose": 100}})
	if r.NoData || r.Score != 84 {
		t.Fatalf("got %+v, want score 84", r)
	}
	if len(r.Metrics) != 2 || r.Metrics[0].ID != "purpose" || r.Metrics[1].Score != 0.25 {
		t.Fatalf("unexpected metrics %+v", r.Metrics)
	}
}

func TestComputeDocumentationErrorReplyIsNoData(t *testing.T) {
	r := ComputeDocumentation(&contracts.DocsGraded{Grade: intPtr(84), Subgrades: map[string]int{"purpose": 100}, Error: "CONTENT_FILTER"})
	if !r.NoData || len(r.Metrics) != 0 {
		t.Fatalf("got %+v, want whole section no-data", r)
	}
}

func TestComputeDocumentationNullGradeIsNoData(t *testing.T) {
	var reply contracts.DocsGraded
	raw := `{"sourcecraft_uuid":"u1","grade":null,"subgrades":{},"advice":[],"error":"LLM_TIMEOUT","evaluated_at":"2026-09-26T12:00:10Z"}`
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Grade != nil || reply.Error != "LLM_TIMEOUT" {
		t.Fatalf("decoded %+v", reply)
	}
	reply.Error = "" // a null grade alone must still be no-data, never 0
	if r := ComputeDocumentation(&reply); !r.NoData {
		t.Fatalf("got %+v, want no-data", r)
	}
}

func TestComputeDocumentationKeepsKnownMethod(t *testing.T) {
	cases := map[string]struct {
		raw  string
		want string
	}{
		"llm":         {`{"grade":80,"subgrades":{},"advice":[],"method":"llm","error":""}`, "llm"},
		"algorithmic": {`{"grade":50,"subgrades":{},"advice":[],"method":"algorithmic","error":""}`, "algorithmic"},
		"absent":      {`{"grade":50,"subgrades":{},"advice":[],"error":""}`, ""},
		"unknown":     {`{"grade":50,"subgrades":{},"advice":[],"method":"magic","error":""}`, ""},
	}
	for name, tc := range cases {
		var reply contracts.DocsGraded
		if err := json.Unmarshal([]byte(tc.raw), &reply); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		r := ComputeDocumentation(&reply)
		if r.Method != tc.want {
			t.Errorf("%s: method = %q, want %q", name, r.Method, tc.want)
		}
		out, _ := json.Marshal(r)
		var wire map[string]any
		_ = json.Unmarshal(out, &wire)
		if _, present := wire["method"]; present != (tc.want != "") {
			t.Errorf("%s: method in JSON = %v, want present only when known: %s", name, present, out)
		}
	}
}

func TestDocsGradedDecodesStructuredAdvice(t *testing.T) {
	var reply contracts.DocsGraded
	raw := `{"sourcecraft_uuid":"u1","grade":85,"subgrades":{"purpose":80},"advice":[{"code":"DOC_PURPOSE_UNCLEAR","priority":"high","text":"T"}],"method":"llm","error":"","evaluated_at":"2026-09-26T12:00:10Z"}`
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		t.Fatal(err)
	}
	if len(reply.Advice) != 1 || reply.Advice[0] != (contracts.DocsAdvice{Code: "DOC_PURPOSE_UNCLEAR", Priority: "high", Text: "T"}) {
		t.Fatalf("advice = %+v", reply.Advice)
	}
}

func intPtr(v int) *int { return &v }
