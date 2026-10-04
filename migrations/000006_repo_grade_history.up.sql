-- Score history of public grades (docs/ranking.md): one row per public grading, never updated or
-- deleted, so past scores survive each regrade. Written by the Aggregator right after the public
-- grade is saved; the composite key makes a redelivered grading.results message a no-op. Private
-- grades are not stored here. low_confidence is NULL when the flag was unknown for that grade.

CREATE TABLE repo_grade_history (
  sourcecraft_uuid   UUID NOT NULL REFERENCES repos(sourcecraft_uuid),
  graded_at          TIMESTAMPTZ NOT NULL,

  score              SMALLINT NOT NULL CHECK (score BETWEEN 0 AND 100),
  category_scores    JSONB NOT NULL DEFAULT '{}',
  low_confidence     BOOLEAN,

  PRIMARY KEY (sourcecraft_uuid, graded_at)
);

CREATE INDEX ON repo_grade_history (graded_at);

-- The current public grades become each repo's first history point.
INSERT INTO repo_grade_history (sourcecraft_uuid, graded_at, score, category_scores, low_confidence)
SELECT g.sourcecraft_uuid, g.graded_at, g.score, g.category_scores, c.low_confidence
FROM repo_grades_public g
LEFT JOIN repo_confidence_public c ON c.sourcecraft_uuid = g.sourcecraft_uuid
WHERE g.score IS NOT NULL AND g.graded_at IS NOT NULL
ON CONFLICT DO NOTHING;
