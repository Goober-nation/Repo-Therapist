# LLM service (Python)

This directory contains the first implementation slice of the Python LLM service.
`llm_service.grading` implements the `documentation-grading-v1` check registry,
objective file checks, validation of integer model component scores and evidence,
score normalization, eight subcategory scores, and machine-readable problems.
`llm_service.snapshot` reads selected text from the Repository service's S3 prefix
and detects a changed listing or selected content during the read.
`llm_service.contracts` validates incoming v1 Documentation and Russian report
request envelopes, category scores, and the arithmetic of supplied report problems.
`llm_service.context` chooses complete user documents and selected build/test
manifests from the full S3 listing. It excludes source files and `.env`, then
hashes the exact UTF-8 context that will be sent to the model. Oversized input
fails explicitly; no document is silently truncated.

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

The service is **not ready to consume or publish Kafka events yet**. The next
implementation slices still need expanded detectors for repository-specific
inputs that static inspection cannot verify, an S3 client adapter, Kafka
consumers/producers, and Russian report generation. Real Yandex calls have returned responses during manual testing; the current
format-only path has been tested locally with simulated provider responses. Real
S3 integration and end-to-end Kafka delivery remain unverified. The grading result must not be published until
those integrations and their failure paths are tested.

Run the current tests from this directory:

```bash
PYTHONPATH=src python3 -m unittest discover -s tests -v
python3 -m compileall -q src tests
```

Python 3.11 or newer is required. These tests use only the standard library and
do not contact Kafka, S3, or a model provider.
