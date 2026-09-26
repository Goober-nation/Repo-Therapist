-- SourceCraft Repo Health — shared database schema (v2)
-- Owner: Aggregator (core) service — sole read-write access, per the architecture diagram
-- (API Gateway -> Aggregator -> DB is the only RW path; Rating service is read-only; every
-- other service reports results back to Aggregator over Kafka/RPC rather than touching this DB).
-- See db-design.md for rationale behind every non-obvious choice below.
-- Supersedes ../schema.sql — see CHANGELOG.md for what changed and why.

-- ============================================================================
-- 1. repos — identity + metadata
-- ============================================================================
CREATE TABLE repos (
  sourcecraft_uuid   UUID PRIMARY KEY,            -- SourceCraft's own repo UUID; our only key now
  org_slug           TEXT NOT NULL,
  repo_slug          TEXT NOT NULL,
  UNIQUE (org_slug, repo_slug),

  visibility         TEXT NOT NULL CHECK (visibility IN ('public','internal','private')),

  last_activity_at   TIMESTAMPTZ                   -- calculated: broadest signal, commits+issues+PRs+releases
);

CREATE INDEX ON repos (visibility);
CREATE INDEX ON repos (last_activity_at DESC);

-- ============================================================================
-- 2. repo_lock — single category-agnostic mutex, one row per repo
-- ============================================================================
-- Replaces a per-table processing_status column. Only the Aggregator ever sets/clears this —
-- it's the sole DB writer, so there's exactly one place in the system that needs a mutex view,
-- not one per worker table. 'busy' means some job (any category, any service) is in flight for
-- this repo; everything else must leave the repo alone until it flips back to 'free'.
CREATE TABLE repo_lock (
  sourcecraft_uuid   UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),

  status             TEXT NOT NULL DEFAULT 'free' CHECK (status IN ('free','busy')),
  status_updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Claim:
-- UPDATE repo_lock SET status = 'busy', status_updated_at = now()
-- WHERE sourcecraft_uuid = $1 AND status = 'free' RETURNING sourcecraft_uuid;
--
-- Release:
-- UPDATE repo_lock SET status = 'free', status_updated_at = now() WHERE sourcecraft_uuid = $1;
--
-- Stale-lock sweep (recover from a crashed worker):
-- UPDATE repo_lock SET status = 'free', status_updated_at = now()
-- WHERE status = 'busy' AND status_updated_at < now() - interval '15 minutes';

-- ============================================================================
-- 3. repo_grades — aggregate health score only (public + private side by side)
-- ============================================================================
CREATE TABLE repo_grades (
  sourcecraft_uuid       UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),

  public_score           SMALLINT CHECK (public_score BETWEEN 0 AND 100),
  public_category_scores JSONB NOT NULL DEFAULT '{}',  -- {"activity":88,"docs":70,...} — rescaled /100
  public_recommendations JSONB NOT NULL DEFAULT '[]',

  private_score           SMALLINT CHECK (private_score BETWEEN 0 AND 100),
  private_category_scores JSONB NOT NULL DEFAULT '{}', -- superset: public categories + ci + appsec
  private_recommendations JSONB NOT NULL DEFAULT '[]', -- superset: public recs + ci/appsec-derived

  missing_data           TEXT[] NOT NULL DEFAULT '{}', -- categories marked "нет данных"

  graded_at              TIMESTAMPTZ
);

CREATE INDEX ON repo_grades (public_score DESC);

-- ============================================================================
-- 4. Per-category fact tables — one per scoring category, same shape throughout
-- ============================================================================
-- Every category below (public-eligible or private-only) gets its own table: raw facts backing
-- the score, plus a `score` column for that category's own contribution. No processing_status
-- here — see repo_lock above, the Aggregator holds the mutex for the whole repo, not per table.
--
-- Public-eligible categories (e.g. activity, docs, community — category list not finalized yet,
-- fill in per your finalized rubric) follow this shape:
--
-- CREATE TABLE repo_activity (
--   sourcecraft_uuid    UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),
--   <category-specific fact columns>,
--   score               SMALLINT,                 -- this category's own raw point total
--   checked_at          TIMESTAMPTZ
-- );
--
-- repo_grades.public_category_scores / private_category_scores read `score` from each category
-- table and roll it up; public_score rescales the sum of public-eligible categories' scores to a
-- /100 scale, private_score sums all categories' scores directly (already designed to add to 100).

-- ----------------------------------------------------------------------------
-- 4a. repo_appsec — AppSec API scan results (private-only category)
-- ----------------------------------------------------------------------------
CREATE TABLE repo_appsec (
  sourcecraft_uuid       UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),

  data_tier              TEXT NOT NULL DEFAULT 'public' CHECK (data_tier IN ('public','owner')),

  scan_uuid              TEXT,
  scan_status            TEXT,                     -- resolved string, not the API's raw int32
  scan_type              TEXT,                      -- SCAN_TYPE_DEFAULT | _AUDIT | _CLOUD_DAST
  commit_hash            TEXT,
  time_started           TIMESTAMPTZ,
  time_finished           TIMESTAMPTZ,

  total_defect_groups    INT,
  found_defect_groups    INT,
  open_defect_groups     INT,

  open_critical_count    INT,
  open_high_count        INT,
  open_medium_count      INT,
  open_low_count         INT,

  defect_groups          JSONB,                     -- defect group summaries, for recommendations
                                                      -- parsed from the raw SARIF/PDF report, which
                                                      -- is discarded after parsing — no S3 pointer,
                                                      -- see db-design.md
  score                  SMALLINT,                   -- this category's contribution, private grade only

  checked_at             TIMESTAMPTZ
);

-- ----------------------------------------------------------------------------
-- 4b. repo_ci — CI/CD signal (private-only category)
-- ----------------------------------------------------------------------------
CREATE TABLE repo_ci (
  sourcecraft_uuid      UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),

  data_tier             TEXT NOT NULL DEFAULT 'public' CHECK (data_tier IN ('public','owner')),
  data_source           TEXT NOT NULL CHECK (data_source IN ('pr_merge_checks','cicd_runs_api','none')),

  has_ci                BOOLEAN,
  sampled_pr_count      INT,                         -- PRs sampled for pr_merge_checks tier
  success_count         INT,
  failure_count         INT,
  workflows             JSONB,                        -- [{workflow_slug, status, created_at, ...}]

  avg_duration_seconds  DOUBLE PRECISION,             -- only populated on 'owner' data_tier
  score                 SMALLINT,                     -- this category's contribution, private grade only

  checked_at            TIMESTAMPTZ
);

-- ============================================================================
-- 5. repo_llm_responses_public / repo_llm_responses_private — LLM evaluation
-- ============================================================================
-- Split in two because the two grades are evaluated with different prompts against different
-- allowed input (private includes CI+AppSec context, public must not).

CREATE TABLE repo_llm_responses_public (
  sourcecraft_uuid   UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),

  raw_response       JSONB,                        -- full LLM output, unparsed
  model_used         TEXT,
  evaluated_at       TIMESTAMPTZ
);

CREATE TABLE repo_llm_responses_private (
  sourcecraft_uuid   UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),

  raw_response       JSONB,
  model_used         TEXT,
  evaluated_at       TIMESTAMPTZ
);


CREATE TABLE users (
    id UUID PRIMARY KEY,
    yandex_id VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

