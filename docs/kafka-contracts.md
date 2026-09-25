# Kafka message contracts

Scope: the message shapes published/consumed on the Kafka links in the architecture diagram —

- Aggregator <-> Grade service — **still TBD**, a team decision (needs whoever owns the
  Aggregator), not filled in here.
- Grade service <-> Security wrapper — **still TBD**, same reasoning.
- Grade service <-> Repository service — **defined below**. This exchange is entirely within one
  person's scope (Repository + Grade service), so it didn't need to wait on wider team sign-off —
  see `backlog/phase-3-kafka-integration.md` for the implementation notes.
- Grade service <-> LLM service — **defined below (request v1, completed v2)** for documentation analysis.
- Aggregator <-> LLM service — **defined below (request/completed v2)** for final report generation.

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

# LLM service contracts (versioned)

These contracts add two independent asynchronous workflows. They do not change the existing
`repo.grade.requested` or `repo.ready` messages.

1. Grade service sends the repository's S3 prefix and receives the final normalized Documentation
   score, scores for its eight sections, and machine-readable facts about deficiencies. LLM
   service owns the Documentation grading method; Grade service owns the cross-category aggregation
   and final Repo Health Score.
2. Aggregator requests a Russian-language report after all scores and analyzer facts are final.
   LLM service does not recalculate or change those scores.

Repository contents are never placed in Kafka messages. Documentation requests contain the S3
prefix from `repo.ready`, not repository files. Report requests contain only final scores and
machine-readable deficiencies; report generation does not re-read repository files.

## LLM service boundary

Documentation and best practices is the only analysis category assessed by LLM service. For this
category, normal code performs objective checks and the model performs semantic checks. LLM service
combines both parts using a versioned grading method and returns the final normalized Documentation
score, eight section grades, and a flat fact list. Grade service consumes that category score without
recalculating its internal checks. It also receives subcategory scores and facts about every
deduction. The documentation response contains no written advice; Russian explanations and
recommendations are generated only after a separate request from Aggregator.

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
  "data_scope": "public",
  "created_at": "2026-09-21T10:15:30Z",
  "payload": {}
}
```

Envelope fields:

| Field | Meaning |
|---|---|
| `event_id` | UUID identifying this Kafka event. A retry of the same logical event keeps this ID. |
| `event_type` | One of the six event types defined below. |
| `event_version` | Contract version for this event type; unsupported versions are rejected explicitly. |
| `analysis_id` | UUID of one complete repository analysis run. Used as the Kafka key. |
| `repository_id` | SourceCraft repository UUID. Slugs are display metadata, not identity. |
| `commit_sha` | Full Git commit SHA when the producer knows it. Repository/Grade's current `repo.ready` exchange does not provide it, so it is optional for Documentation events. |
| `data_scope` | `public` for public-only data or `owner` for data available to an authorized owner. |
| `created_at` | RFC 3339 UTC timestamp. Producers emit the `Z` form. |
| `payload` | Event-specific object. |

Current versions: Documentation request/failure = 1; Documentation completed = 2;
report request/completed = 2; report failure = 1 (unchanged error envelope). The Python
request parser also accepts legacy report request v1 during migration. Report v1 keeps its
legacy problem-impact rules; new producers use v2 for compact Documentation facts.

Result and failure events additionally require `request_event_id`, equal to the `event_id` of the
request being answered. They copy `analysis_id`, `repository_id`, `data_scope`, and `commit_sha`
(when present) unchanged from that request.

Aggregator creates `analysis_id` when the complete analysis run starts and every service propagates
it unchanged. The producer of each event creates its `event_id`; therefore LLM service creates a new
`event_id` for its terminal response while preserving the request's ID in `request_event_id`.

No event may contain credentials, SourceCraft/Yandex tokens, S3 secret keys, full prompts, or whole
repository files. A serialized event should remain below 512 KiB so it stays comfortably below the
usual Kafka 1 MiB record limit.

## Documentation analysis

### `documentation.analysis.requested`

Topic: `llm.documentation.requests`.

The request is published after Grade service consumes `repo.ready` with `status: ready`. It forwards
that event's `s3_prefix` unchanged. The S3 bucket, endpoint, and credentials are runtime
configuration of LLM service and are not included in Kafka. Documentation grading does not generate
prose, so it needs no `output_language` field.

```json
{
  "event_id": "018f47d2-6d5a-7b72-82fd-1e9834d57a91",
  "event_type": "documentation.analysis.requested",
  "event_version": 1,
  "analysis_id": "018f47d1-e33b-7af3-a642-229ac22a9e82",
  "repository_id": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "data_scope": "public",
  "created_at": "2026-09-21T10:15:30Z",
  "payload": {
    "s3_prefix": "repos/019b2102-fa21-7ab7-8ce1-116af406fb5e"
  }
}
```

S3 input rules:

- `s3_prefix` is exactly the value from `repo.ready`; object keys are read below
  `s3_prefix + "/"`, so similarly named repository prefixes cannot overlap;
- the repository content is read from that prefix; `_meta/` exports are not part of the
  documentation being graded;
- LLM service builds an in-memory manifest from the complete object listing and records hashes of
  selected file contents used for the analysis; this manifest is not another S3 object;
- Repository service currently reuses `repos/<repository_id>` and does not publish a manifest or
  commit SHA. LLM service must detect a changed object list or changed selected contents while
  reading and fail the request instead of publishing a mixed-state result;
- `file_missing` is allowed only after a complete, successful prefix listing. An inaccessible or
  unstable prefix produces a failed event, never a false absence.

### `documentation.analysis.completed`

Topic: `llm.documentation.results`. Event version: **2**.

The payload contains exactly three fields: the overall Documentation grade, eight section grades,
and a flat list of facts describing deficiencies. Correlation IDs remain in the event envelope.

```json
{
  "event_id": "018f47d8-e274-7aa8-ac4f-e85ee5f01818",
  "event_type": "documentation.analysis.completed",
  "event_version": 2,
  "request_event_id": "018f47d2-6d5a-7b72-82fd-1e9834d57a91",
  "analysis_id": "018f47d1-e33b-7af3-a642-229ac22a9e82",
  "repository_id": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "data_scope": "public",
  "created_at": "2026-09-21T10:15:44Z",
  "payload": {
    "grade": 98,
    "subcategories": [
      {
        "id": "required_files",
        "grade": 100
      },
      {
        "id": "purpose",
        "grade": 100
      },
      {
        "id": "local_run",
        "grade": 90
      },
      {
        "id": "build",
        "grade": 100
      },
      {
        "id": "testing",
        "grade": 100
      },
      {
        "id": "usage",
        "grade": 100
      },
      {
        "id": "structure",
        "grade": 100
      },
      {
        "id": "consistency",
        "grade": 100
      }
    ],
    "facts": [
      {
        "id": "DOC-0001",
        "subcategory": "local_run",
        "code": "DOC_LOCAL_RUN_CONFIGURATION_INCOMPLETE",
        "facts": {
          "missing_aspects": [
            "required_parameters"
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
  }
}
```

Payload rules:

- `grade`: number from 0 to 100, rounded to two decimals. Grade service uses this value directly.
- `subcategories`: exactly eight objects with only `id` and `grade`. IDs are `required_files`,
  `purpose`, `local_run`, `build`, `testing`, `usage`, `structure`, `consistency`.
- Each section grade is normalized to 0–100 within that section. `null` means every check in
  the section is not applicable; it is not zero and must not reduce the overall grade.
- Overall grade uses the existing check weights and evaluated maxima, not the arithmetic mean
  of the eight section grades. Scoring guidance remains in `documentation-grading.md`.
- `facts`: an array of objects containing `id`, `subcategory`, `code`, `facts`, and `evidence`.
  `facts` within an item is an object of structured deficiency details; `evidence` is an array
  of objects and can be empty. `code` labels the kind of deficiency. There is no advice text.
- Fact IDs are unique within a result (for example `DOC-0001`). They are assigned in internal
  check/deduction order and preserved when the same logical event is retried or forwarded.
  They are not cross-analysis identities. Facts retain their section association.
- Both objective-file and model-produced facts are included, even minor ones. All supplied
  model findings are retained without testing their arithmetic against the section score.
- Validation is format-only. Model facts, quotes, line numbers and semantic claims are not
  certified by the service; evidence and numeric deduction consistency do not block delivery.
- The payload does **not** contain `checks`, `components`, `deductions`, `problems`, `severity`,
  numeric score impacts, coverage, prompt/model metadata, context hashes or validation flags
  at its top level. Evidence objects may retain their original context hash or file excerpt.
- Recommendations and human-readable explanations are produced only in the later report flow.

The 25 internal checks and component scores remain implementation details. Removing them from
Kafka does not change the scoring weights. Grade service forwards this complete compact payload
to Aggregator, which supplies it as `payload.documentation` in report request v2. Neither service
needs to synthesize severity, numerical per-fact penalties or verified problem records.

Version 2 replaces the former extended v1 completed payload. Consumers must explicitly migrate;
there is no silent interpretation of v1 `score/checks/problems` as v2 `grade/subcategories/facts`.
Documentation requests and documentation failure events retain version 1.

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
- `S3_PREFIX_NOT_FOUND`
- `S3_CONTENT_CHANGED`
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
results. Documentation facts are forwarded unchanged in `payload.documentation`; they are
format-checked model claims, not verified problems. `problems` holds other categories only. The request contains no S3
snapshot because report generation must use final facts, not re-read repository source code.
In report request v2, `output_language` is required and its only supported value is `ru`.

```json
{
  "event_id": "018f47e0-66ad-76fa-8c50-d162b7264a47",
  "event_type": "report.generation.requested",
  "event_version": 2,
  "analysis_id": "018f47d1-e33b-7af3-a642-229ac22a9e82",
  "repository_id": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "commit_sha": "47c673f70ddf4c3e5b8768df35d688f43c6f5f52",
  "data_scope": "public",
  "created_at": "2026-09-21T10:16:30Z",
  "payload": {
    "output_language": "ru",
    "scores": {
      "overall": {
        "status": "CALCULATED",
        "score": 91
      },
      "categories": [
        {
          "category": "security",
          "status": "NO_DATA",
          "score": null
        },
        {
          "category": "activity",
          "status": "CALCULATED",
          "score": 91
        },
        {
          "category": "documentation",
          "status": "CALCULATED",
          "score": 98,
          "coverage_percent": 100
        },
        {
          "category": "ci_cd",
          "status": "NO_DATA",
          "score": null
        },
        {
          "category": "issues",
          "status": "CALCULATED",
          "score": 100
        },
        {
          "category": "code_health",
          "status": "CALCULATED",
          "score": 100
        }
      ]
    },
    "problems": [
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
    ],
    "documentation": {
      "grade": 98,
      "subcategories": [
        {
          "id": "required_files",
          "grade": 100
        },
        {
          "id": "purpose",
          "grade": 100
        },
        {
          "id": "local_run",
          "grade": 90
        },
        {
          "id": "build",
          "grade": 100
        },
        {
          "id": "testing",
          "grade": 100
        },
        {
          "id": "usage",
          "grade": 100
        },
        {
          "id": "structure",
          "grade": 100
        },
        {
          "id": "consistency",
          "grade": 100
        }
      ],
      "facts": [
        {
          "id": "DOC-0001",
          "subcategory": "local_run",
          "code": "DOC_LOCAL_RUN_CONFIGURATION_INCOMPLETE",
          "facts": {
            "missing_aspects": [
              "required_parameters"
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
    }
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

`documentation` is required: the exact three-field completed payload when Documentation is
scored, or `null` if the category failed/is unavailable. Preserve its grade, section grades and
all fact IDs/content. The category score supplied in `scores.categories` comes from its `grade`.
Documentation facts need no severity, score impact, or mandatory verified evidence.

Problem and deduction rules for the other five categories:

- `id` is unique within the analysis and stable for the supplied result;
- `category` is one of `security`, `activity`, `ci_cd`, `issues`, `code_health`; Documentation
  entries belong only in `documentation.facts`;
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
  "event_version": 2,
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
        {"source_type": "documentation_fact", "source_id": "DOC-0001"}
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
        "text": "Документация оценена в 98 баллов из 100 из-за неполной инструкции локального запуска.",
        "sources": [
          {"source_type": "category_score", "source_id": "documentation"},
          {"source_type": "documentation_fact", "source_id": "DOC-0001"}
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
          {"source_type": "documentation_fact", "source_id": "DOC-0001"}
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
          {"source_type": "documentation_fact", "source_id": "DOC-0001"}
        ]
      }
    ]
  }
}
```

Every generated statement has at least one source. A source is one of:

- `{"source_type":"problem","source_id":"<existing problem id>"}`;
- `{"source_type":"documentation_fact","source_id":"<documentation.facts item id>"}`;
- `{"source_type":"category_score","source_id":"<existing category id>"}`;
- `{"source_type":"overall_score","source_id":"overall"}`.

LLM service validates every reference against the request. Unknown IDs make the model response
invalid. Every input problem ID and Documentation fact ID must appear in at least one item of the output `problems` array, so
even a minor deduction remains visible. Recommendation sources must include at least one problem or Documentation fact.
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
