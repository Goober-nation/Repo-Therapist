-- Low-confidence flag of a public grade (grading.results "confidence", docs/kafka-contracts.md).
-- Public grades only, written by the Aggregator right after the public grade; no row means the
-- flag is unknown (grade stored before the flag existed), which is never treated as low.

CREATE TABLE repo_confidence_public (
  sourcecraft_uuid   UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),

  low_confidence     BOOLEAN NOT NULL,
  reasons            JSONB NOT NULL DEFAULT '[]',

  graded_at          TIMESTAMPTZ
);

CREATE INDEX ON repo_confidence_public (low_confidence);
