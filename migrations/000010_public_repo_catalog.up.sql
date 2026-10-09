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
