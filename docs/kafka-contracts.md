# Kafka message contracts

Scope: the message shapes published/consumed on the Kafka links in the architecture diagram —

- Aggregator <-> Grade service — **still TBD**, a team decision (needs whoever owns the
  Aggregator), not filled in here.
- Grade service <-> Security wrapper — **still TBD**, same reasoning.
- Grade service <-> Repository service — **defined below**. This exchange is entirely within one
  person's scope (Repository + Grade service), so it didn't need to wait on wider team sign-off —
  see `backlog/phase-3-kafka-integration.md` for the implementation notes.
- Grade service <-> LLM service — **defined below (v1)** for semantic documentation analysis.
- Aggregator <-> LLM service — **defined below (v1)** for final report generation.

## `repo.grade.requested`

Producer: currently only a manual trigger (`grade-service --request-org=X --request-repo=Y`);
eventually the Aggregator. Consumer: Repository service.

```json
{
  "org_slug": "examples",
  "repo_slug": "vulnerable-repo",
  "requested_at": "2026-09-19T23:49:12+03:00"
}
```

## `repo.ready`

Producer: Repository service, once it's finished (successfully or not) handling a
`repo.grade.requested` message. Consumer: Grade service.

```json
{
  "org_slug": "examples",
  "repo_slug": "vulnerable-repo",
  "sourcecraft_uuid": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "s3_prefix": "repos/019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "status": "ready",
  "commit_count": 3,
  "pull_count": 2,
  "release_count": 0,
  "ready_at": "2026-09-19T23:49:15+03:00"
}
```

`status` is `"ready"` or `"failed"` — on failure, `error` carries the message and the count fields
are omitted. Grade service logs and skips `"failed"` messages rather than trying to grade them.

Go types: `internal/contracts/repo_grading.go`. Verified end-to-end (2026-09-19): manual trigger ->
Repository service -> `repo.ready` -> Grade service -> printed Activity score, matching the same
result independently verified by hand earlier in phase 2.

---

# LLM service contracts (v1)

These contracts add two independent asynchronous workflows. They do not change the existing
`repo.grade.requested` or `repo.ready` messages.

1. Grade service requests a Documentation assessment and receives the final normalized score for
   that category. LLM service owns the Documentation grading method; Grade service owns the
   cross-category aggregation and final Repo Health Score.
2. Aggregator requests a Russian-language report after all scores and verified problems are final.
   LLM service does not recalculate or change those scores.

Repository contents are never placed in Kafka messages. Documentation requests contain a reference
to an immutable, completed S3 snapshot. Report requests contain only final scores and verified
machine-readable deductions; report generation does not read the repository snapshot.

## LLM service boundary

Documentation and best practices is the only analysis category assessed by LLM service. For this
category, normal code performs objective checks and the model performs semantic checks. LLM service
combines both parts using a versioned grading method and returns the final normalized Documentation
score, deduction codes, and validated evidence. Grade service consumes that category score without
recalculating its internal checks.

Activity, Security, CI/CD, Issues, and Code Health are assessed algorithmically outside LLM service.
LLM service does not read their raw inputs, run their analyzers, or calculate their category scores.
It sees their final scores and verified problems only in `report.generation.requested`, where they
are immutable source material for human-readable synthesis.

## Topics and ownership

| Topic | Producer | Consumer | Kafka message key |
|---|---|---|---|
| `llm.documentation.requests` | Grade service | LLM service | `analysis_id` |
| `llm.documentation.results` | LLM service | Grade service | `analysis_id` |
| `llm.report.requests` | Aggregator | LLM service | `analysis_id` |
| `llm.report.results` | LLM service | Aggregator | `analysis_id` |

Each LLM service instance that handles documentation uses the same consumer group. Report workers
likewise share a separate consumer group. Suggested names are `llm-service-documentation-v1` and
`llm-service-report-v1`. Different logical consumers must not share a consumer group: members of
one group divide messages instead of each receiving a copy.

Producers must use a key-aware partitioner so every message with the same `analysis_id` maps to the
same partition within a topic. Kafka only guarantees order inside one topic partition; no workflow
may rely on ordering between different topics.

## Common event envelope

Every LLM event is UTF-8 JSON with `Content-Type: application/json` semantics and this envelope:

```json
{
  "event_id": "018f47d2-6d5a-7b72-82fd-1e9834d57a91",
  "event_type": "documentation.analysis.requested",
  "event_version": 1,
  "analysis_id": "018f47d1-e33b-7af3-a642-229ac22a9e82",
  "repository_id": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "commit_sha": "47c673f70ddf4c3e5b8768df35d688f43c6f5f52",
  "data_scope": "public",
  "created_at": "2026-09-21T10:15:30Z",
  "payload": {}
}
```

Required fields:

| Field | Meaning |
|---|---|
| `event_id` | UUID identifying this Kafka event. A retry of the same logical event keeps this ID. |
| `event_type` | One of the six event types defined below. |
| `event_version` | Contract version. Version 1 consumers reject other versions explicitly. |
| `analysis_id` | UUID of one complete repository analysis run. Used as the Kafka key. |
| `repository_id` | SourceCraft repository UUID. Slugs are display metadata, not identity. |
| `commit_sha` | Full Git commit SHA for the repository state being assessed. |
| `data_scope` | `public` for public-only data or `owner` for data available to an authorized owner. |
| `created_at` | RFC 3339 UTC timestamp. Producers emit the `Z` form. |
| `payload` | Event-specific object. |

Result and failure events additionally require `request_event_id`, equal to the `event_id` of the
request being answered. They copy `analysis_id`, `repository_id`, `commit_sha`, and `data_scope`
unchanged from that request.

Aggregator creates `analysis_id` when the complete analysis run starts and every service propagates
it unchanged. The producer of each event creates its `event_id`; therefore LLM service creates a new
`event_id` for its terminal response while preserving the request's ID in `request_event_id`.

No event may contain credentials, SourceCraft/Yandex tokens, S3 secret keys, full prompts, or whole
repository files. A serialized event should remain below 512 KiB so it stays comfortably below the
usual Kafka 1 MiB record limit.

## Documentation analysis

### `documentation.analysis.requested`

Topic: `llm.documentation.requests`.

The request is published only after the snapshot is complete and its manifest is readable. S3
endpoint and credentials are runtime configuration of LLM service and are not included in Kafka.
In contract v1, `output_language` is required and its only supported value is `ru`.

```json
{
  "event_id": "018f47d2-6d5a-7b72-82fd-1e9834d57a91",
  "event_type": "documentation.analysis.requested",
  "event_version": 1,
  "analysis_id": "018f47d1-e33b-7af3-a642-229ac22a9e82",
  "repository_id": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "commit_sha": "47c673f70ddf4c3e5b8768df35d688f43c6f5f52",
  "data_scope": "public",
  "created_at": "2026-09-21T10:15:30Z",
  "payload": {
    "snapshot": {
      "bucket": "repo-content",
      "prefix": "snapshots/018f47d1-e33b-7af3-a642-229ac22a9e82/",
      "manifest_key": "snapshots/018f47d1-e33b-7af3-a642-229ac22a9e82/manifest.json"
    },
    "output_language": "ru"
  }
}
```

Snapshot requirements:

- `prefix` is unique to this snapshot and ends with `/`;
- objects under the prefix are immutable after the request is published;
- `manifest_key` is inside that prefix and is written last;
- the manifest identifies `repository_id` and `commit_sha`, records its completion time, and lists
  every content object (the manifest itself is excluded) with its key, byte size, and SHA-256
  digest;
- LLM service fails the job if manifest identity differs from the event or a selected object does
  not match the manifest;
- absence may be reported as `file_missing` only when the complete manifest was successfully read.
  An inaccessible or incomplete snapshot produces `NO_DATA` or a failure, never a false absence.

Manifest v1 shape:

```json
{
  "manifest_version": 1,
  "repository_id": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "commit_sha": "47c673f70ddf4c3e5b8768df35d688f43c6f5f52",
  "completed_at": "2026-09-21T10:15:25Z",
  "objects": [
    {
      "key": "snapshots/018f47d1-e33b-7af3-a642-229ac22a9e82/README.md",
      "size_bytes": 4821,
      "sha256": "8ecb8d39de30c96e1cc3a1d76a3c75fca3b579648983b216d9e095173c42f699"
    }
  ]
}
```

### `documentation.analysis.completed`

Topic: `llm.documentation.results`.

```json
{
  "event_id": "018f47d8-e274-7aa8-ac4f-e85ee5f01818",
  "event_type": "documentation.analysis.completed",
  "event_version": 1,
  "request_event_id": "018f47d2-6d5a-7b72-82fd-1e9834d57a91",
  "analysis_id": "018f47d1-e33b-7af3-a642-229ac22a9e82",
  "repository_id": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "commit_sha": "47c673f70ddf4c3e5b8768df35d688f43c6f5f52",
  "data_scope": "public",
  "created_at": "2026-09-21T10:15:44Z",
  "payload": {
    "category": "documentation",
    "grading_method_version": "documentation-grading-v1",
    "prompt_version": "documentation-prompt-v1",
    "context": {
      "sha256": "d5b389a2248c8a7f7054f4b22a70f56fcab12c56108a5f4cc1a9bcb870cc5462",
      "selected_file_count": 7,
      "total_file_count": 83,
      "truncated": false
    },
    "model": {
      "provider": "yandex-cloud",
      "name": "model-name-set-by-deployment"
    },
    "score": {
      "status": "CALCULATED",
      "value": 98,
      "scale": 100
    },
    "coverage": {
      "applicable_points": 100,
      "evaluated_points": 100,
      "percent": 100
    },
    "checks": [
      {
        "check_id": "local_run.configuration",
        "check_type": "SEMANTIC",
        "outcome": "EVALUATED",
        "points_awarded": 2,
        "max_points": 4,
        "deductions": [
          {
            "deduction_id": "local_run.configuration:environment",
            "code": "DOC_LOCAL_RUN_CONFIGURATION_INCOMPLETE",
            "raw_points_lost": 2,
            "facts": {
              "missing_aspects": [
                "environment_variables"
              ]
            },
            "evidence": [
              {
                "type": "file_content",
                "path": "README.md",
                "start_line": 30,
                "end_line": 38,
                "quote": "POSTGRES_USER=..."
              },
              {
                "type": "file_missing",
                "path": ".env.example"
              }
            ]
          }
        ]
      },
      {
        "check_id": "license.exists",
        "check_type": "DETERMINISTIC",
        "outcome": "EVALUATED",
        "points_awarded": 5,
        "max_points": 5,
        "deductions": []
      }
    ],
    "problems": [
      {
        "id": "DOC-LOCAL-RUN-CONFIGURATION",
        "category": "documentation",
        "code": "DOC_LOCAL_RUN_CONFIGURATION_INCOMPLETE",
        "severity": "LOW",
        "score_impact": {
          "category_points_lost": 2
        },
        "facts": {
          "missing_aspects": [
            "environment_variables"
          ]
        },
        "evidence": [
          {
            "type": "file_content",
            "path": "README.md",
            "start_line": 30,
            "end_line": 38,
            "quote": "POSTGRES_USER=..."
          },
          {
            "type": "file_missing",
            "path": ".env.example"
          }
        ],
        "source_checks": [
          "local_run.configuration"
        ],
        "source_deductions": [
          "local_run.configuration:environment"
        ]
      }
    ],
    "warnings": []
  }
}
```

The example abbreviates `checks`; a production event contains every check defined by
`grading_method_version`. The omitted checks in this example are fully evaluated and receive their
maximum points. `check_id` values and their contribution to the category score belong to the named
grading method version. Extending the method does not require a Kafka contract version change.
Grade service does not interpret individual checks; it consumes `score`. Checks and problems are
included for auditing and as validated machine input for the final report.

Score rules:

- `category` is always `documentation`;
- `scale` is always `100` in contract v1;
- `status` is `CALCULATED` when every applicable check was evaluated and `CALCULATED_PARTIAL` when
  one or more applicable checks are `NO_DATA`;
- both statuses carry a numeric `value` from 0 to 100;
- LLM service calculates `value` from deterministic and semantic checks using the exact method
  identified by `grading_method_version`;
- the model does not freely invent the final category score: normal service code validates model
  output and applies the versioned method.

Normalization rules:

```text
applicable_max = sum(max_points where outcome != NOT_APPLICABLE)
evaluated_max  = sum(max_points where outcome == EVALUATED)
earned         = sum(points_awarded where outcome == EVALUATED)
coverage       = evaluated_max / applicable_max * 100
score          = earned / evaluated_max * 100
```

Missing data never counts as zero. If no applicable check can be evaluated, LLM service publishes a
failed event instead of fabricating a completed result. Exact weights and applicability rules are
defined by `documentation-grading-v1` in `docs/documentation-grading.md`.

Check rules:

| Field | Values / meaning |
|---|---|
| `check_type` | `DETERMINISTIC`, `SEMANTIC`, or `HYBRID` |
| `outcome` | `EVALUATED`, `NO_DATA`, or `NOT_APPLICABLE` |
| `points_awarded` | Integer from 0 through the method-defined `max_points` for `EVALUATED`; otherwise `null` |
| `deductions` | Machine-readable deduction codes, typed facts, and validated evidence; never user-facing prose |

For every evaluated check, `raw_points_lost` values sum to
`max_points - points_awarded`. `raw_points_lost` is an integer on that check's method-defined scale;
after missing-data normalization it may differ from the deduction's contribution on the final
category scale. A fully satisfied check has an empty `deductions` array. `NO_DATA` is distinct from
a zero-point evaluated check: it means the available snapshot/context is insufficient for a
reliable result. `NOT_APPLICABLE` means the check does not apply to this repository. Internal
scales, weights, formulas, and normalization belong to `documentation-grading-v1`, not the Kafka
schema.

The documentation grading workflow emits no user-facing reason, title, or recommendation. Those
are generated later, and only once, by the report workflow. Deduction `code`, typed `facts`, and
validated evidence carry the machine-readable reason between the two workflows. The deduction
code selects the allowed facts schema; arbitrary model-generated fact fields are rejected. Every
deduction has a `deduction_id` that is stable and unique within the result. Documentation codes and
their facts/evidence schemas are defined in `docs/documentation-codes.md`.

Problem rules:

- normal service code groups deductions by a shared root cause; the model does not perform this
  grouping;
- every deduction appears in exactly one problem's `source_deductions`;
- `source_checks` lists all contributing checks;
- `score_impact.category_points_lost` is the grouped raw loss normalized by `evaluated_points`;
- problem impacts sum exactly to `100 - score.value`, with the final problem absorbing any rounding
  remainder;
- severity is computed after grouping: up to 10 lost points is `LOW`, more than 10 through 25 is
  `MEDIUM`, more than 25 through 50 is `HIGH`, and more than 50 is `CRITICAL`;
- a code-specific minimum severity may raise, but never lower, impact severity.

Allowed evidence variants:

```jsonl
{"type":"file_content","path":"README.md","start_line":10,"end_line":12,"quote":"..."}
{"type":"file_exists","path":"Dockerfile"}
{"type":"file_missing","path":".env.example"}
{"type":"document_review","paths":["README.md","docs/overview.md"],"context_sha256":"d5b389a2248c8a7f7054f4b22a70f56fcab12c56108a5f4cc1a9bcb870cc5462"}
```

Paths are UTF-8, relative to the snapshot root, use `/`, and cannot contain `..`. Lines are
one-based and inclusive. LLM service validates file existence, line ranges, and quotes against the
snapshot before publishing a completed event. For `document_review`, every listed file must have
been included completely in the hashed model context; it supports absence of semantic content in
existing documents. Invalid model evidence is never forwarded.

`warnings` reports non-fatal context limitations such as intentionally truncated documentation.
When truncation prevents a reliable decision, the affected check is `NO_DATA`, coverage decreases,
and the result becomes `CALCULATED_PARTIAL`. Missing-data warnings do not create deductions and do
not reduce the normalized score.

### `documentation.analysis.failed`

Topic: `llm.documentation.results`.

```json
{
  "event_id": "018f47d8-e274-7aa8-ac4f-e85ee5f01819",
  "event_type": "documentation.analysis.failed",
  "event_version": 1,
  "request_event_id": "018f47d2-6d5a-7b72-82fd-1e9834d57a91",
  "analysis_id": "018f47d1-e33b-7af3-a642-229ac22a9e82",
  "repository_id": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "commit_sha": "47c673f70ddf4c3e5b8768df35d688f43c6f5f52",
  "data_scope": "public",
  "created_at": "2026-09-21T10:15:44Z",
  "payload": {
    "error": {
      "code": "LLM_PROVIDER_UNAVAILABLE",
      "retryable": true,
      "message": "Провайдер модели временно недоступен"
    }
  }
}
```

Documentation error codes:

- `INVALID_EVENT`
- `UNSUPPORTED_EVENT_VERSION`
- `SNAPSHOT_NOT_FOUND`
- `SNAPSHOT_IDENTITY_MISMATCH`
- `SNAPSHOT_INTEGRITY_FAILED`
- `S3_UNAVAILABLE`
- `CONTEXT_BUILD_FAILED`
- `LLM_PROVIDER_UNAVAILABLE`
- `LLM_RATE_LIMITED`
- `LLM_INVALID_RESPONSE`
- `EVIDENCE_VALIDATION_FAILED`
- `INTERNAL_ERROR`

## Report generation

### `report.generation.requested`

Topic: `llm.report.requests`.

Aggregator publishes this event only after Grade service and all analyzers have produced final
results. Problems are already verified before they reach LLM service. The request contains no S3
snapshot because report generation must use final facts, not re-read repository source code.
In contract v1, `output_language` is required and its only supported value is `ru`.

```json
{
  "event_id": "018f47e0-66ad-76fa-8c50-d162b7264a47",
  "event_type": "report.generation.requested",
  "event_version": 1,
  "analysis_id": "018f47d1-e33b-7af3-a642-229ac22a9e82",
  "repository_id": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "commit_sha": "47c673f70ddf4c3e5b8768df35d688f43c6f5f52",
  "data_scope": "public",
  "created_at": "2026-09-21T10:16:30Z",
  "payload": {
    "output_language": "ru",
    "scores": {
      "overall": {"status": "CALCULATED", "score": 91},
      "categories": [
        {"category": "security", "status": "NO_DATA", "score": null},
        {"category": "activity", "status": "CALCULATED", "score": 91},
        {"category": "documentation", "status": "CALCULATED", "score": 68, "coverage_percent": 100},
        {"category": "ci_cd", "status": "NO_DATA", "score": null},
        {"category": "issues", "status": "CALCULATED", "score": 100},
        {"category": "code_health", "status": "CALCULATED", "score": 100}
      ]
    },
    "problems": [
      {
        "id": "DOC-LOCAL-RUN",
        "category": "documentation",
        "code": "DOC_LOCAL_RUN_INCOMPLETE",
        "severity": "HIGH",
        "score_impact": {
          "category_points_lost": 32
        },
        "facts": {
          "missing_aspects": [
            "environment_variables",
            "expected_result"
          ]
        },
        "evidence": [
          {
            "type": "file_content",
            "summary": "В README указана команда запуска",
            "path": "README.md",
            "start_line": 42,
            "end_line": 42
          }
        ]
      },
      {
        "id": "ACT-RECENCY",
        "category": "activity",
        "code": "LAST_ACTIVITY_TOO_OLD",
        "severity": "LOW",
        "score_impact": {
          "category_points_lost": 9
        },
        "facts": {
          "days_since_last_activity": 7,
          "healthy_threshold_days": 3
        },
        "evidence": [
          {
            "type": "metric",
            "summary": "Дней с последней активности",
            "metric_id": "days_since_last_activity",
            "raw_value": 7,
            "unit": "days"
          }
        ]
      }
    ]
  }
}
```

Score rules:

- status is one of `CALCULATED`, `CALCULATED_PARTIAL`, `NO_DATA`, `NOT_CALCULATED`, or `FAILED`;
- `score` is a number from 0 to 100 for `CALCULATED` and `CALCULATED_PARTIAL`; otherwise it is
  `null`;
- a partially calculated category includes `coverage_percent`; full results may include it for
  consistency;
- LLM service preserves every supplied score exactly and never derives a replacement;
- all six categories are present, including unavailable ones, so missing data cannot disappear
  from the report;
- `data_scope` decides which problems Aggregator may include. LLM service does not combine public
  and owner-only inputs.

Problem and deduction rules:

- `id` is unique within the analysis and stable for the supplied result;
- `category` is one of `security`, `activity`, `documentation`, `ci_cd`, `issues`, `code_health`;
- `severity` is `CRITICAL`, `HIGH`, `MEDIUM`, or `LOW`; minor deductions are `LOW`, not omitted;
- `score_impact.category_points_lost` is a positive number on that category's 0-100 scale;
- for every `CALCULATED` or `CALCULATED_PARTIAL` category below 100, at least one scoring problem
  for that category is required;
- for each `CALCULATED` or `CALCULATED_PARTIAL` category, its problem deductions sum to
  `100 - score` (within 0.01 for decimal rounding). A category at 100 has no scoring problems;
- `code` and `facts` are machine-readable inputs. Producers do not pre-generate a title,
  explanation, or recommendation;
- `code` selects a versioned, category-specific facts schema; unknown codes are invalid input;
- evidence is supplied and verified by analyzers rather than invented by the report model;
- evidence may reference a file, metric, AppSec problem, CI run, issue, commit, merge request, or
  release. Each evidence item has a short verified `summary` plus identifiers needed by the UI;
- the complete request must remain below 512 KiB. Producers aggregate repeated raw occurrences into
  one problem with counts/evidence when necessary, but no reason for a point deduction may be
  omitted.

### `report.generation.completed`

Topic: `llm.report.results`.

```json
{
  "event_id": "018f47e5-414e-77ad-80f2-061f91e79f89",
  "event_type": "report.generation.completed",
  "event_version": 1,
  "request_event_id": "018f47e0-66ad-76fa-8c50-d162b7264a47",
  "analysis_id": "018f47d1-e33b-7af3-a642-229ac22a9e82",
  "repository_id": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "commit_sha": "47c673f70ddf4c3e5b8768df35d688f43c6f5f52",
  "data_scope": "public",
  "created_at": "2026-09-21T10:16:39Z",
  "payload": {
    "report_version": "health-report-v1",
    "prompt_version": "report-prompt-v1",
    "model": {
      "provider": "yandex-cloud",
      "name": "model-name-set-by-deployment"
    },
    "summary": {
      "text": "Репозиторий получил 91 балл из 100. Основные улучшения связаны с документацией.",
      "sources": [
        {"source_type": "overall_score", "source_id": "overall"},
        {"source_type": "problem", "source_id": "DOC-LOCAL-RUN"}
      ]
    },
    "category_assessments": [
      {
        "category": "activity",
        "text": "Активность оценена в 91 балл из 100; небольшое снижение связано с давностью последнего изменения.",
        "sources": [
          {"source_type": "category_score", "source_id": "activity"},
          {"source_type": "problem", "source_id": "ACT-RECENCY"}
        ]
      },
      {
        "category": "documentation",
        "text": "Документация оценена в 68 баллов из 100 из-за неполной инструкции локального запуска.",
        "sources": [
          {"source_type": "category_score", "source_id": "documentation"},
          {"source_type": "problem", "source_id": "DOC-LOCAL-RUN"}
        ]
      },
      {
        "category": "issues",
        "text": "Категория Issues получила 100 баллов из 100.",
        "sources": [
          {"source_type": "category_score", "source_id": "issues"}
        ]
      },
      {
        "category": "code_health",
        "text": "Категория Code Health получила 100 баллов из 100.",
        "sources": [
          {"source_type": "category_score", "source_id": "code_health"}
        ]
      }
    ],
    "problems": [
      {
        "text": "В документации не указаны обязательные переменные окружения.",
        "sources": [
          {"source_type": "problem", "source_id": "DOC-LOCAL-RUN"}
        ]
      },
      {
        "text": "Давность последней активности не позволила категории Activity получить 100 баллов.",
        "sources": [
          {"source_type": "problem", "source_id": "ACT-RECENCY"}
        ]
      }
    ],
    "missing_data": [
      {
        "category": "security",
        "text": "Данные Security для этого анализа недоступны.",
        "sources": [
          {"source_type": "category_score", "source_id": "security"}
        ]
      },
      {
        "category": "ci_cd",
        "text": "Данные CI/CD для этого анализа недоступны.",
        "sources": [
          {"source_type": "category_score", "source_id": "ci_cd"}
        ]
      }
    ],
    "recommendations": [
      {
        "priority": "HIGH",
        "title": "Дополнить инструкцию локального запуска",
        "reason": "Без описания конфигурации запуск нельзя надёжно воспроизвести.",
        "action": "Перечислить обязательные переменные и добавить файл-пример.",
        "expected_effect": "Локальный запуск станет воспроизводимым для новых участников.",
        "sources": [
          {"source_type": "problem", "source_id": "DOC-LOCAL-RUN"}
        ]
      }
    ]
  }
}
```

Every generated statement has at least one source. A source is one of:

- `{"source_type":"problem","source_id":"<existing problem id>"}`;
- `{"source_type":"category_score","source_id":"<existing category id>"}`;
- `{"source_type":"overall_score","source_id":"overall"}`.

LLM service validates every reference against the request. Unknown IDs make the model response
invalid. Every input problem ID must appear in at least one item of the output `problems` array, so
even a minor deduction remains visible. Recommendation sources must include at least one problem.
Exact projected overall score changes are not generated because only Grade service can calculate
cross-category and final score changes. LLM service owns only the Documentation category
calculation.
`category_assessments` contains every category with `CALCULATED` or `CALCULATED_PARTIAL` status
exactly once, including categories scored at 100. A 100-point assessment may state only the score;
it must not invent a detailed strength that was absent from the request.
`missing_data` contains every category whose input status is `NO_DATA`, `NOT_CALCULATED`, or
`FAILED`, plus a coverage note for every `CALCULATED_PARTIAL` category. It contains no fully
`CALCULATED` category.

The contract intentionally returns structured report content rather than Markdown or PDF. A
presentation layer can render the same validated content into web, Markdown, or PDF without another
model call.

### `report.generation.failed`

Topic: `llm.report.results`. It uses the same failure shape as
`documentation.analysis.failed`.

Report error codes:

- `INVALID_EVENT`
- `UNSUPPORTED_EVENT_VERSION`
- `INVALID_SCORE_INPUT`
- `INVALID_PROBLEM_INPUT`
- `LLM_PROVIDER_UNAVAILABLE`
- `LLM_RATE_LIMITED`
- `LLM_INVALID_RESPONSE`
- `REPORT_SOURCE_VALIDATION_FAILED`
- `INTERNAL_ERROR`

## Delivery, retries, and idempotency

Kafka delivery is at-least-once. The contracts do not claim exactly-once processing.

- A producer retry of the same logical event reuses `event_id`. Creating a new `event_id` means a
  new logical event.
- LLM service publishes one terminal `completed` or `failed` event for a request and commits the
  input offset only after Kafka acknowledges that publication.
- A crash between publishing and committing can publish the same terminal result again. Result
  consumers therefore deduplicate by `request_event_id` and accept only the first terminal result.
- `retryable` tells the workflow owner whether a new request may be scheduled. It does not cause
  LLM service to loop forever. Internal schema/evidence correction attempts are bounded separately.
- A duplicate request may cause repeated computation if the worker has no durable deduplication
  store. This is acceptable for v1; the stable IDs make the duplicate safe for downstream state.
- A fully malformed message that lacks trustworthy correlation fields cannot produce a valid
  failure event. The consumer logs only non-sensitive metadata, records a metric, and commits it so
  it cannot block its partition. A future dead-letter topic can be added if operations require it.

## Compatibility rules

- Adding an optional field is backward-compatible within `event_version: 1`.
- Removing a field, changing its meaning/type, adding a required field, or changing enum semantics
  requires `event_version: 2` and a migration period.
- Event consumers reject unsupported versions; they do not guess or partially process them.
- Grading method, prompt, model, and report template versions are result provenance, not Kafka
  schema versions.
- Timestamps, UUIDs, category IDs, and enum spelling are case-sensitive as shown above.
