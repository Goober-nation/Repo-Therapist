-- Generated from docs/schema.sql (v2). Keep in sync manually — schema.sql is the source of
-- truth for rationale (see docs/db-design.md, currently in tmp/ pending final review); this
-- file is just the golang-migrate-runnable form of it.

CREATE TABLE repos (
  sourcecraft_uuid   UUID PRIMARY KEY,
  org_slug           TEXT NOT NULL,
  repo_slug          TEXT NOT NULL,
  UNIQUE (org_slug, repo_slug),

  name               TEXT NOT NULL,
  description        TEXT,
  web_url            TEXT NOT NULL,
  default_branch     TEXT NOT NULL,

  visibility         TEXT NOT NULL CHECK (visibility IN ('public','internal','private')),

  primary_language   TEXT,

  rating_value       DOUBLE PRECISION,
  rating_percentile  DOUBLE PRECISION,
  rating_reactions   JSONB,

  counters           JSONB,

  last_commit_at     TIMESTAMPTZ,
  last_activity_at   TIMESTAMPTZ
);

CREATE INDEX ON repos (visibility);
CREATE INDEX ON repos (last_activity_at DESC);

CREATE TABLE repo_lock (
  sourcecraft_uuid   UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),
  status             TEXT NOT NULL DEFAULT 'free' CHECK (status IN ('free','busy')),
  status_updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE repo_grades (
  sourcecraft_uuid       UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),

  public_score           SMALLINT CHECK (public_score BETWEEN 0 AND 100),
  public_category_scores JSONB NOT NULL DEFAULT '{}',
  public_recommendations JSONB NOT NULL DEFAULT '[]',

  private_score           SMALLINT CHECK (private_score BETWEEN 0 AND 100),
  private_category_scores JSONB NOT NULL DEFAULT '{}',
  private_recommendations JSONB NOT NULL DEFAULT '[]',

  missing_data           TEXT[] NOT NULL DEFAULT '{}',

  graded_at              TIMESTAMPTZ
);

CREATE INDEX ON repo_grades (public_score DESC);

CREATE TABLE repo_appsec (
  sourcecraft_uuid       UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),

  data_tier              TEXT NOT NULL DEFAULT 'public' CHECK (data_tier IN ('public','owner')),

  scan_uuid              TEXT,
  scan_status            TEXT,
  scan_type              TEXT,
  commit_hash            TEXT,
  time_started           TIMESTAMPTZ,
  time_finished          TIMESTAMPTZ,

  total_defect_groups    INT,
  found_defect_groups    INT,
  open_defect_groups     INT,

  open_critical_count    INT,
  open_high_count        INT,
  open_medium_count      INT,
  open_low_count         INT,

  defect_groups          JSONB,
  score                  SMALLINT,

  checked_at             TIMESTAMPTZ
);

CREATE TABLE repo_ci (
  sourcecraft_uuid      UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),

  data_tier             TEXT NOT NULL DEFAULT 'public' CHECK (data_tier IN ('public','owner')),
  data_source           TEXT NOT NULL CHECK (data_source IN ('pr_merge_checks','cicd_runs_api','none')),

  has_ci                BOOLEAN,
  sampled_pr_count      INT,
  success_count         INT,
  failure_count         INT,
  workflows             JSONB,

  avg_duration_seconds  DOUBLE PRECISION,
  score                 SMALLINT,

  checked_at            TIMESTAMPTZ
);

CREATE TABLE repo_llm_responses_public (
  sourcecraft_uuid   UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),
  raw_response       JSONB,
  model_used         TEXT,
  evaluated_at       TIMESTAMPTZ
);

CREATE TABLE repo_llm_responses_private (
  sourcecraft_uuid   UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),
  raw_response       JSONB,
  model_used         TEXT,
  evaluated_at       TIMESTAMPTZ
);
