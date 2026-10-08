-- Admin batch regrade (docs/admin.md): a batch is a list of public repositories regraded on an admin's
-- request. Written only by the Aggregator. Private repositories never appear here: a batch selects
-- from public repos only, so slugs are safe to keep. The actor is an HMAC, never a raw Yandex ID.
CREATE TABLE regrade_batches (
  id           BIGSERIAL PRIMARY KEY,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at  TIMESTAMPTZ,

  status       TEXT NOT NULL DEFAULT 'running'
               CHECK (status IN ('running', 'paused', 'completed', 'cancelled')),
  actor_hash   TEXT NOT NULL DEFAULT '',
  -- The filter the admin chose (languages, score range, age, hand-picked list); no secrets.
  filter       JSONB NOT NULL DEFAULT '{}'
);

-- At most one unfinished batch at a time.
CREATE UNIQUE INDEX regrade_batches_one_active ON regrade_batches ((true))
  WHERE status IN ('running', 'paused');

CREATE TABLE regrade_batch_items (
  batch_id     BIGINT NOT NULL REFERENCES regrade_batches(id) ON DELETE CASCADE,
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

CREATE INDEX ON regrade_batch_items (batch_id, status);
