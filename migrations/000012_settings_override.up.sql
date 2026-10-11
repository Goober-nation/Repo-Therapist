-- Admin overrides of config/app.yaml settings (docs/config.md). Only the Aggregator writes here; it
-- republishes every row to the compacted Kafka topic config.updated, which the services read.
-- A row exists only while a key is overridden: resetting a key to its app.yaml value deletes it.
CREATE TABLE settings_override (
  key        TEXT PRIMARY KEY,           -- app.yaml path, e.g. gateway.rate_limit
  value      TEXT NOT NULL,
  updated_by TEXT NOT NULL DEFAULT '',   -- audit actor hash of the admin, never a raw Yandex ID
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
