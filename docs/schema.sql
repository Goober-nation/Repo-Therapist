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
  web_url            TEXT
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


CREATE TABLE users (
    id UUID PRIMARY KEY,
    yandex_id VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

