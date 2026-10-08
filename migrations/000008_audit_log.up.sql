-- Audit log: an append-only record of security-relevant actions of admins and users (docs/admin.md).
-- Written only by the Aggregator. Holds no tokens, no raw Yandex IDs and no slugs of private
-- repositories: actor_hash is an HMAC of the Yandex ID, and a private target is a hash or a label.
-- Rows older than AUDIT_RETENTION_DAYS (default 30) are deleted by the Aggregator.
CREATE TABLE audit_log (
  id           BIGSERIAL PRIMARY KEY,
  occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

  actor_kind   TEXT NOT NULL CHECK (actor_kind IN ('admin', 'user', 'anonymous', 'system')),
  actor_hash   TEXT NOT NULL DEFAULT '',

  action       TEXT NOT NULL,
  target_kind  TEXT NOT NULL DEFAULT '',
  target       TEXT NOT NULL DEFAULT '',
  outcome      TEXT NOT NULL CHECK (outcome IN ('ok', 'denied', 'error')),
  details      JSONB NOT NULL DEFAULT '{}'
);

CREATE INDEX ON audit_log (occurred_at);
CREATE INDEX ON audit_log (action, id DESC);
