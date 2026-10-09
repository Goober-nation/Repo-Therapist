CREATE TABLE repos (
  sourcecraft_uuid   UUID PRIMARY KEY,
  org_slug           TEXT NOT NULL,
  repo_slug          TEXT NOT NULL,
  UNIQUE (org_slug, repo_slug),

  visibility         TEXT NOT NULL CHECK (visibility IN ('public','internal','private')),

  last_activity_at   TIMESTAMPTZ,

  main_language      TEXT,
  language_color     TEXT,
  sourcecraft_rating DOUBLE PRECISION,
  web_url            TEXT,
  -- Repository description from SourceCraft, searched by the ranking's topic filter.
  description        TEXT
);

CREATE INDEX ON repos (visibility);
CREATE INDEX ON repos (last_activity_at DESC);

CREATE TABLE repo_lock (
  sourcecraft_uuid   UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),

  status             TEXT NOT NULL DEFAULT 'free' CHECK (status IN ('free','busy')),
  status_updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE repo_grades_public (
  sourcecraft_uuid   UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),

  score              SMALLINT CHECK (score BETWEEN 0 AND 100),
  category_scores    JSONB NOT NULL DEFAULT '{}',
  advice             JSONB NOT NULL DEFAULT '[]',

  missing_data       TEXT[] NOT NULL DEFAULT '{}',
  graded_at          TIMESTAMPTZ
);

CREATE INDEX ON repo_grades_public (score DESC);

CREATE TABLE repo_grades_private (
  sourcecraft_uuid   UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),

  score              SMALLINT CHECK (score BETWEEN 0 AND 100),
  category_scores    JSONB NOT NULL DEFAULT '{}',
  advice             JSONB NOT NULL DEFAULT '[]',

  missing_data       TEXT[] NOT NULL DEFAULT '{}',
  graded_at          TIMESTAMPTZ
);

CREATE INDEX ON repo_grades_private (score DESC);

-- Low-confidence flag of a public grade. Written by the Aggregator right after the public grade;
-- no row = unknown (never treated as low). reasons: [{code, text}] generic pre-written texts.
CREATE TABLE repo_confidence_public (
  sourcecraft_uuid   UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),

  low_confidence     BOOLEAN NOT NULL,
  reasons            JSONB NOT NULL DEFAULT '[]',

  graded_at          TIMESTAMPTZ
);

CREATE INDEX ON repo_confidence_public (low_confidence);

-- Informational analytics (bus factor, ownership map) of a grade, one row per repo and grade mode.
-- Written by the Aggregator right after the grade; no row = no analytics yet.
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

CREATE TABLE repo_llm_responses_public (
  sourcecraft_uuid   UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),

  raw_response       JSONB,
  model_used         TEXT,
  evaluated_at       TIMESTAMPTZ,
  graded_at          TIMESTAMPTZ
);

CREATE TABLE repo_llm_responses_private (
  sourcecraft_uuid   UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),

  raw_response       JSONB,
  model_used         TEXT,
  evaluated_at       TIMESTAMPTZ,
  graded_at          TIMESTAMPTZ
);


-- Repository facts the public ranking filters on, written by the Aggregator right after a public
-- grade. NULL member = unknown (never matched by a filter); no row = no facts yet.
CREATE TABLE repo_facts_public (
  sourcecraft_uuid     UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),

  files                INTEGER,
  loc                  INTEGER,
  has_ci               BOOLEAN,
  has_security_policy  BOOLEAN,

  graded_at            TIMESTAMPTZ
);

-- Score history of public grades: one row per public grading, append-only (written by the Aggregator
-- right after the public grade; the composite key makes a redelivered message a no-op). Private
-- grades are not stored. low_confidence NULL = the flag was unknown for that grade.
CREATE TABLE repo_grade_history (
  sourcecraft_uuid   UUID NOT NULL REFERENCES repos(sourcecraft_uuid),
  graded_at          TIMESTAMPTZ NOT NULL,

  score              SMALLINT NOT NULL CHECK (score BETWEEN 0 AND 100),
  category_scores    JSONB NOT NULL DEFAULT '{}',
  low_confidence     BOOLEAN,

  PRIMARY KEY (sourcecraft_uuid, graded_at)
);

CREATE INDEX ON repo_grade_history (graded_at);

CREATE TABLE users (
    id UUID PRIMARY KEY,
    yandex_id VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);


-- Audit log of admin and user actions (docs/admin.md). Written only by the Aggregator; no tokens, no
-- raw Yandex IDs, no slugs of private repositories. Rows older than AUDIT_RETENTION_DAYS are deleted.
CREATE TABLE audit_log (
  id           BIGSERIAL PRIMARY KEY,
  occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

  actor_kind   TEXT NOT NULL CHECK (actor_kind IN ('admin', 'user', 'anonymous', 'system')),
  -- HMAC of the Yandex ID, truncated; empty for anonymous and system actors.
  actor_hash   TEXT NOT NULL DEFAULT '',

  -- Dotted lowercase name, e.g. admin.denied, regrade.batch.start.
  action       TEXT NOT NULL,
  -- '' | repo | batch | route; a private repo is a hash, never its slug.
  target_kind  TEXT NOT NULL DEFAULT '',
  target       TEXT NOT NULL DEFAULT '',
  outcome      TEXT NOT NULL CHECK (outcome IN ('ok', 'denied', 'error')),
  details      JSONB NOT NULL DEFAULT '{}'
);

CREATE INDEX ON audit_log (occurred_at);
CREATE INDEX ON audit_log (action, id DESC);


-- Admin batch grading (docs/admin.md; created as regrade_* by migration 000009, renamed by 000011).
-- Written only by the Aggregator. A batch selects public
-- repositories only; the actor is an HMAC of the Yandex ID. At most one batch is unfinished at a time.
CREATE TABLE grading_batches (
  id           BIGSERIAL PRIMARY KEY,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at  TIMESTAMPTZ,

  status       TEXT NOT NULL DEFAULT 'running'
               CHECK (status IN ('running', 'paused', 'completed', 'cancelled')),
  actor_hash   TEXT NOT NULL DEFAULT '',
  -- The filter the admin chose (languages, score range, age, hand-picked list); no secrets.
  filter       JSONB NOT NULL DEFAULT '{}'
);

CREATE UNIQUE INDEX grading_batches_one_active ON grading_batches ((true))
  WHERE status IN ('running', 'paused');

CREATE TABLE grading_batch_items (
  batch_id     BIGINT NOT NULL REFERENCES grading_batches(id) ON DELETE CASCADE,
  repo_id      UUID NOT NULL REFERENCES repos(sourcecraft_uuid),
  org_slug     TEXT NOT NULL,
  repo_slug    TEXT NOT NULL,
  PRIMARY KEY (batch_id, repo_id),

  status       TEXT NOT NULL DEFAULT 'pending'
               CHECK (status IN ('pending', 'running', 'ok', 'failed', 'skipped')),
  -- A short fixed code (grading_failed, timeout, ...), never free text from the graders.
  error        TEXT NOT NULL DEFAULT '',
  started_at   TIMESTAMPTZ,
  finished_at  TIMESTAMPTZ
);

CREATE INDEX ON grading_batch_items (batch_id, status);

-- Public repo catalog (docs/admin.md): the list of every public SourceCraft repository, fetched page
-- by page on an admin's request and used by the scheduled regrade and batch selection until the next
-- refresh. Written only by the Aggregator. A refresh fills the staging table and replaces the catalog
-- in one transaction only when the whole listing succeeded, so readers never see a partial list.
CREATE TABLE public_repo_catalog (
  sourcecraft_uuid   UUID PRIMARY KEY,
  org_slug           TEXT NOT NULL,
  repo_slug          TEXT NOT NULL,
  main_language      TEXT,
  language_color     TEXT,
  sourcecraft_rating DOUBLE PRECISION NOT NULL DEFAULT 0,
  last_activity_at   TIMESTAMPTZ,
  web_url            TEXT,
  description        TEXT,
  fetched_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ON public_repo_catalog (sourcecraft_rating DESC);
CREATE INDEX ON public_repo_catalog (lower(main_language));
CREATE INDEX ON public_repo_catalog (lower(org_slug || '/' || repo_slug));

-- Same shape, no constraints beyond the columns: pages may repeat a repo while the listing shifts.
CREATE UNLOGGED TABLE public_repo_catalog_staging (
  sourcecraft_uuid   UUID NOT NULL,
  org_slug           TEXT NOT NULL,
  repo_slug          TEXT NOT NULL,
  main_language      TEXT,
  language_color     TEXT,
  sourcecraft_rating DOUBLE PRECISION NOT NULL DEFAULT 0,
  last_activity_at   TIMESTAMPTZ,
  web_url            TEXT,
  description        TEXT,
  fetched_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per refresh. The actor is an HMAC, never a raw Yandex ID; empty for the automatic first
-- refresh of an empty catalog.
CREATE TABLE catalog_refreshes (
  id           BIGSERIAL PRIMARY KEY,
  started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at  TIMESTAMPTZ,

  status       TEXT NOT NULL DEFAULT 'running'
               CHECK (status IN ('running', 'completed', 'failed', 'cancelled')),
  trigger      TEXT NOT NULL CHECK (trigger IN ('admin', 'startup')),
  actor_hash   TEXT NOT NULL DEFAULT '',
  pages        INT NOT NULL DEFAULT 0,
  -- Repos fetched so far; on completion, public repos in the new catalog.
  repos        INT NOT NULL DEFAULT 0,
  -- A short fixed code (wrapper_error, too_many_pages, interrupted, ...), never free text.
  error        TEXT NOT NULL DEFAULT ''
);

-- At most one refresh running at a time.
CREATE UNIQUE INDEX catalog_refreshes_one_running ON catalog_refreshes ((true))
  WHERE status = 'running';
