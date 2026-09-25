# LLM Documentation result

The active Python Yandex path checks JSON structure, IDs, types and score ranges only.
It does not reject model claims for mismatched deduction arithmetic, evidence quotes,
line numbers, context hashes or applicability. This remains the user-approved behavior.

`YandexModel.grade(snapshot, context)` and the local CLI now return exactly the same
three-field payload sent in `documentation.analysis.completed` v2:

- `grade`: overall Documentation score, 0–100.
- `subcategories`: eight `{id, grade}` objects; a wholly inapplicable group has `grade: null`.
- `facts`: flat list of `{id, subcategory, code, facts, evidence}` objects. Fact details
  are objects; evidence is an array and can be empty. No advice or numeric penalties.

The format-only model response still contains the 21 semantic/hybrid checks used by the
scoring implementation. Python adds four objective file checks and calculates the weighted
overall score and section scores from `points_awarded`. These internal records and their
component values are not exported in the completed Kafka payload or CLI output.
All deduction facts are projected without semantic filtering or fabricated score impacts.
Fact IDs are deterministic within a result (`DOC-0001`, etc.) and must be preserved on retry.

`documentation_result.documentation_payload` handles projection.
`contracts.validate_documentation_payload` checks the wire shape.
`contracts.build_documentation_completed(request, payload)` creates the v2 Kafka envelope,
copying request/analysis/repository/scope/commit correlation. It does not publish to a broker.
`parse_documentation_result` validates the serialized completed event.

Report request v2 includes this unchanged payload as `documentation` (or null on an
unavailable category); `problems` describes the other five categories. Reports reference
Documentation fact IDs via `source_type: documentation_fact`. Legacy report request v1
remains readable with its previous problem-impact rules.

The legacy strict scoring/evidence validator is not in the active Yandex call path.
One additional model request is allowed for a malformed response, never for semantic
inconsistency. Kafka transport consumers/producers remain to be implemented.

From `backend/llm-service`:

```bash
PYTHONPATH=src python3 -m llm_service --repo ../..
PYTHONPATH=src python3 -m llm_service --repo ../.. --dry-run
PYTHONPATH=src python3 -m unittest discover -s tests -q
```

The normal command prints the compact payload as JSON to stdout and progress/errors to
stderr. Dry-run prepares context without network or credentials. Live generation uses
`YANDEX_AI_API_KEY`, `YANDEX_FOLDER_ID`, and optional `YANDEX_AI_MODEL` from the environment.
