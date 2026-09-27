-- Gateway users (docs/schema.sql). One row per Yandex ID account.

CREATE TABLE users (
  id          UUID PRIMARY KEY,
  yandex_id   VARCHAR(255) NOT NULL UNIQUE,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
