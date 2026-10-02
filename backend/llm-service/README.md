# LLM service (Python)

This directory contains the first implementation slice of the Python LLM service.
`llm_service.grading` implements the `documentation-grading-v1` check registry,
objective file checks, validation of integer model component scores and evidence,
score normalization, eight subcategory scores, and machine-readable problems.
`llm_service.snapshot` reads selected text from the Repository service's S3 prefix
and detects a changed listing or selected content during the read.
`llm_service.contracts` validates incoming v1 Documentation and Russian report
request envelopes, category scores, and the arithmetic of supplied report problems.
`llm_service.context` ranks at most 12 documentation candidates and eight supporting
artifacts, in addition to required-file checks. It scans headings algorithmically and
sends at most eight excerpts of up to 30 lines / 3000 UTF-8 bytes each. Topics cover
purpose, local run, build, tests, structure and usage, with diverse topics preferred.
The default model context cap is 32000 bytes. This is a sampled estimate, not a full
audit. Omitted text is not evidence of missing documentation. Candidate scanning
reads complete UTF-8 files locally (up to 2 MB each / 8 MB total); only excerpts are
sent to the model. Large scans still fail explicitly at those limits.

The active Yandex adapter validates **output format only** using
`llm_service.formatted_response`. It checks JSON structure, types, check IDs,
integer score ranges and evidence object shapes. Quotes, line numbers, facts,
applicability judgments, and consistency between component/score/deduction totals
are not verified and do not block a correctly formatted response.

`grade(snapshot, context)` returns exactly `grade`, `subcategories`, and `facts`.
The eight section grades are normalized to 0–100; an inapplicable section uses null.
The flat fact list includes objective-file and model findings, each with an ID,
section, code, fact details and evidence. No internal checks, component values,
severity, numeric penalties, context or provider metadata are exported.
The same object is the payload of `documentation.analysis.completed` v2.
`contracts.build_documentation_completed` builds the correlation envelope but does not
publish it to Kafka. Report request v2 accepts this payload in `documentation`;
legacy report request v1 remains readable.

Internally, the adapter scores 21 model checks plus four objective checks and derives
summary scores from `points_awarded`. The legacy strict validator remains separate
and is not called by the Yandex adapter.

One correction request is made only when output format is invalid, so a grading
call can incur up to two generation charges. Transport errors are reported directly.
Input above the local UTF-8 size cap fails explicitly; this cap is not an exact
measure of model tokens. No files are silently truncated.

Run against a local repository from this directory:

```bash
PYTHONPATH=src python3 -m llm_service --repo ../..
```

The entire result is printed as JSON. Add `--dry-run` to inspect the selected file
list and context size without calling the model. See
[model output boundary](../../docs/llm-model-output.md) for the result semantics.

Set these environment variables locally (do not commit the API key):

```text
YANDEX_AI_API_KEY=<secret key from AI Studio>
YANDEX_FOLDER_ID=<folder ID>
YANDEX_AI_MODEL=aliceai-llm
```

`YANDEX_AI_MODEL=aliceai-llm-flash` is also supported for comparison. With the
variables exported, verify access using a short paid API request from this directory:

```bash
PYTHONPATH=src python3 -c 'from llm_service.yandex_model import YandexModel, YandexModelConfig; print(YandexModel(YandexModelConfig.from_env()).complete("Ответь одним словом: готово", max_output_tokens=32).decode())'
```

The Documentation worker consumes `repo.docs.grade.requested` with
`{sourcecraft_uuid, s3_prefix, requested_at}` and publishes `repo.docs.graded`
messages with `{sourcecraft_uuid, grade, subgrades, advice, error, evaluated_at}`.
The key is
`sourcecraft_uuid`. `subgrades` maps section IDs to integer percentages, omitting
unreviewed/inapplicable sections. `advice` contains only Russian improvement recommendations selected deterministically
by deficiency code (or section fallback). Reasons, file references and raw evidence
are not sent in this field. No additional model call is made. Internal local CLI output retains `subcategories` and `facts`.
See [the active contract](../../docs/kafka-contracts.md#оценка-документации-активный-контракт-grade--llm).
On success `error` is empty. On evaluation failure `error` contains a short code,
`grade` is null, `subgrades` is empty, and `advice` is empty. The worker continues
with later requests. The input offset is committed only after Kafka acknowledges
either result. The Go Grade consumer still needs to handle nonempty `error` as
Documentation `NO_DATA` instead of using the grade fields.
The Yandex adapter has a 60-second model-call budget per Documentation evaluation,
shared by the initial request and its optional format retry. An exhausted budget
produces `LLM_TIMEOUT`; the Grade service retains its 70-second fallback timeout.
A request whose `requested_at` is older than `LLM_DOCS_REQUEST_MAX_AGE_SECONDS`
(default 70) is skipped without reading S3 or calling the model: nothing is
published and the offset is committed, since Grade no longer waits for it.
Redelivery can repeat results; there is no request ID in this contract to
distinguish simultaneous grading runs for the same repository.

Install the Python service and start the worker from this directory:

```bash
python3 -m pip install -e .
PYTHONPATH=src python3 -m llm_service --listen
```

Alongside the Yandex settings above, export `S3_ENDPOINT`, `S3_BUCKET`,
`S3_ACCESS_KEY`, `S3_SECRET_KEY`, and `KAFKA_BROKERS`. For the repository's
local Docker Compose setup they are `localhost:8054`, `repo-content`, the
S3 credentials from the local environment, and `localhost:8053`. Optional
`S3_USE_SSL=true` switches to HTTPS; optional `LLM_KAFKA_GROUP_ID` overrides
the default `llm-service-documentation-v1` group. Credentials are required;
the LLM service never creates buckets or writes S3 objects. Kafka topics must
exist before starting the consumer; this worker does not provision them.

Real Yandex calls have returned responses during manual testing. The Kafka
previous envelope request/result round trip was verified against the local Kafka broker with a
simulated repository and model. S3 list/get and context preparation were
verified over HTTP against a local S3-compatible test server. Local S3 is now RustFS (the MinIO image can no longer be pulled);
a full compose grading run read repository data from it successfully. The legacy report envelope
(`llm.report.*`) is superseded by the summary worker below.

## Repository summary and recommendations

The summary worker consumes `llm.recommendations.requested` from the Aggregator and
publishes `llm.recommendations.received`; see
[the contract](../../docs/kafka-contracts.md#сводка-и-рекомендации-по-репозиторию-активный-контракт-aggregator--llm-2026-09-27).
Input is the final grade only (overall score, category scores, recommendations, missing
data); repository files are not read. The model writes a Russian `summary` and up to eight
recommendation texts. Code derives each recommendation's `priority` from the
scores. Output format and category IDs are validated; the wording is not. Public requests never expose Security or
CI/CD to the model. One format retry fits in a 60-second budget; failures produce
`raw_response: null` and an error code.

```bash
# Kafka worker (needs the Yandex variables and KAFKA_BROKERS; S3 is not used)
PYTHONPATH=src python3 -m llm_service --listen-summary

# One request from a JSON file, printed as the result event (paid model call)
PYTHONPATH=src python3 -m llm_service --summary-file request.json
```

Optional: `LLM_SUMMARY_KAFKA_GROUP_ID` (default `llm-service-recommendations-v1`),
`LLM_SUMMARY_REQUEST_TOPIC`, `LLM_SUMMARY_RESULT_TOPIC`, `LLM_PRIORITY_THRESHOLD` (integer
0-100, default 75: the `MEDIUM`/`LOW` recommendation priority boundary and Security's `HIGH`
boundary; the old name `LLM_STRENGTH_THRESHOLD` is read when the new one is unset).

Documentation grading mode, `LLM_DOCS_MODE`: `auto` (default) grades with the model and falls
back to the algorithmic grade on any failure of the model path — the call or building its
result; repository read failures still return an error. `llm` reports every failure,
`algorithmic` never calls the model.

Documentation advice texts (per deduction code, per-section fallbacks, the algorithmic-grade
notice) live in `config/advice.yaml`; `LLM_ADVICE_PATH` overrides the path. The catalog is
validated at startup: every deduction code and section needs text, unknown ones are rejected.

Run the current tests from this directory:

```bash
PYTHONPATH=src python3 -m unittest discover -s tests -v
python3 -m compileall -q src tests
```

Python 3.11 or newer is required. These tests use simulated clients and do not
contact Kafka, S3, or a model provider.


Sampling refinement (2026-09-26): the selector prioritizes individual rubric checks,
reserves README introduction, and follows up to eight existing repository-local
README document links (one hop; inline and reference links). External links are
never fetched. Excerpts have a shared 24000-byte content budget; the entire JSON
context is capped at 32000 bytes. Candidate scanning remains bounded separately.
Each model check is marked `sampled` or `not_reviewed` in the internal context.
An unreviewed check contributes neither earned nor maximum points and cannot
produce deficiency facts. The total is 100 * earned / evaluated maximum. A section
with no evaluated checks has `grade: null`; this now means either not applicable
or not reviewed. No estimated score is substituted for omitted material. This
supersedes earlier wording that required all section grades to be estimated.
The Kafka object shape is unchanged; consumers must display null as unavailable,
not as zero. Deterministic required-file checks retain their existing rules.

The current flat Kafka contract passed an integration check with a real local Kafka
broker, an HTTP S3 emulator and an HTTP Responses API stub (2026-09-26). The actual
HTTP request contained the model URI and JSON Schema; the result matched the flat
Kafka contract, and the offset was committed after publication. Temporary test
topics were removed. Real MinIO + Yandex end-to-end operation remains unverified.

Audit fixes: ignore S3 directory markers; discover named root documentation such
as INSTALL/BUILD/TESTING; enforce the context budget after JSON escaping and
recompute coverage if excerpts are dropped; suppress facts from inapplicable
checks; include the schema in the serialized request byte guard; bound S3 network
timeouts/retries and report truncated model HTTP responses as provider errors.

The model produces internal `checks`, not Kafka events. The local validator checks
structure/types, scoring code normalizes evaluated checks, and `grade_wire.py`
converts the summary to the agreed flat Kafka result. Model factual accuracy is
not mechanically verified. Schema mode uses `strict: false`; malformed output is
rejected locally with at most one format-correction retry. The provider schema
fields follow the [Responses API reference](https://aistudio.yandex.ru/en/docs/ai-studio/api/Responses/createResponse).

Excluded checks (NOT_APPLICABLE or algorithmically not_reviewed) are normalized
to null scores and empty deductions before validating their unused values.
Evaluated checks retain score type/range validation.
