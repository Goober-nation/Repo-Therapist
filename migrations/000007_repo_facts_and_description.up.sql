-- Repository facts the public ranking filters on (docs/ranking.md) and the repository description
-- for the topic search. Facts are written by the Aggregator right after a public grade; NULL members
-- mean "unknown" and are never matched by a filter, no row means no facts yet.

ALTER TABLE repos ADD COLUMN description TEXT;

CREATE TABLE repo_facts_public (
  sourcecraft_uuid     UUID PRIMARY KEY REFERENCES repos(sourcecraft_uuid),

  files                INTEGER,
  loc                  INTEGER,
  has_ci               BOOLEAN,
  has_security_policy  BOOLEAN,

  graded_at            TIMESTAMPTZ
);
