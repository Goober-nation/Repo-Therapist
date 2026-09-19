# Kafka message contracts

Status: **TBD — not yet designed.** This is a team decision (Repository service, Grade service,
Security wrapper, and whoever owns the Aggregator side all need to agree), not something to fill in
unilaterally.

Scope, once written: the message shapes published/consumed on the two Kafka links in the
architecture diagram —

- Aggregator <-> Grade service
- Grade service <-> Repository service / Security wrapper

For each message: producer, consumer(s), topic name, payload schema, and what triggers it (e.g.
"Repository service publishes `repo.synced` once a repo's content lands in S3, consumed by Grade
service to start scoring").
