-- Informational analytics of a grade (grading.results "analytics", docs/grading/analytics.md):
-- bus factor and the ownership map. One row per repo and grade mode, written by the Aggregator
-- right after the grade; no row means there are no analytics yet (never an error).

CREATE TABLE repo_analytics_public (
  sourcecraft_uuid   UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),

  analytics          JSONB NOT NULL,
  graded_at          TIMESTAMPTZ
);

CREATE TABLE repo_analytics_private (
  sourcecraft_uuid   UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),

  analytics          JSONB NOT NULL,
  graded_at          TIMESTAMPTZ
);
