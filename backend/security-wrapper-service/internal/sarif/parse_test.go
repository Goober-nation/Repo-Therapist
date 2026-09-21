package sarif

import (
	"repo-therapist/security-wrapper-service/internal/domain"
	"testing"
)

const sampleSARIF = `{
  "runs": [
    {
      "tool": {
        "driver": {
          "rules": [
            {
              "id": "go/sql-injection",
              "name": "SQL Injection",
              "shortDescription": {"text": "Unsanitized input reaches a SQL query"},
              "defaultConfiguration": {"level": "error"},
              "properties": {"security-severity": "9.8"}
            },
            {
              "id": "go/unused-import",
              "name": "Unused import",
              "defaultConfiguration": {"level": "note"}
            }
          ]
        }
      },
      "results": [
        {
          "ruleId": "go/sql-injection",
          "ruleIndex": 0,
          "message": {"text": "user input flows into db.Query"},
          "locations": [
            {
              "physicalLocation": {
                "artifactLocation": {"uri": "internal/db/query.go"},
                "region": {"startLine": 42, "endLine": 44}
              }
            }
          ]
        },
        {
          "ruleId": "go/unused-import",
          "ruleIndex": 1,
          "message": {"text": "\"fmt\" imported and not used"},
          "locations": [
            {
              "physicalLocation": {
                "artifactLocation": {"uri": "cmd/main.go"},
                "region": {"startLine": 3, "endLine": 3}
              }
            }
          ]
        }
      ]
    }
  ]
}`

func TestParse(t *testing.T) {
	summaries, err := Parse([]byte(sampleSARIF))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}

	sqlInjection := summaries[0]
	if sqlInjection.RuleName != "SQL Injection" {
		t.Errorf("RuleName = %q, want %q", sqlInjection.RuleName, "SQL Injection")
	}
	if sqlInjection.Severity != domain.SeverityCritical {
		t.Errorf("Severity = %q, want %q (security-severity 9.8 -> CRITICAL)", sqlInjection.Severity, domain.SeverityCritical)
	}
	if sqlInjection.File != "internal/db/query.go" || sqlInjection.StartLine != 42 || sqlInjection.EndLine != 44 {
		t.Errorf("unexpected location: %+v", sqlInjection)
	}

	unusedImport := summaries[1]
	if unusedImport.Severity != domain.SeverityLow {
		t.Errorf("Severity = %q, want %q (level note -> LOW, no security-severity property)", unusedImport.Severity, domain.SeverityLow)
	}
}

func TestParse_EmptyRuns(t *testing.T) {
	summaries, err := Parse([]byte(`{"runs":[]}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("expected 0 summaries, got %d", len(summaries))
	}
}

func TestParse_InvalidJSON(t *testing.T) {
	if _, err := Parse([]byte("not json")); err == nil {
		t.Fatal("expected an error for invalid JSON, got nil")
	}
}
