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
fails explicitly; no document is silently truncated. `llm_service.model_response`
accepts only the 21 semantic/hybrid check objects, validates their deduction
codes and typed facts against the closed registry, verifies evidence against
the stable snapshot, and lets ordinary code calculate the final score.
`llm_service.detectors` reads filenames and selected manifests without running
repository code. Its verified facts are included in the hashed model context
and inserted into deductions by Python after the model responds. A model
response containing its own `detected_*` values is rejected.

Model output is one JSON object with a `checks` array and no prose. Each check
contains `check_id`, `check_type`, `outcome`, `points_awarded`, `max_points`,
`components`, and `deductions`. The provider adapter will request this shape;
unknown fields, duplicate IDs, unverified detector facts, and fabricated file
quotes are rejected. `NO_DATA` and `NOT_APPLICABLE` require an ordinary-code
decision before the model may return them.

The service is **not ready to consume or publish Kafka events yet**. Model choice
was intentionally deferred. The next implementation slice needs a model adapter,
expanded detectors for repository-specific inputs that static inspection cannot
yet verify, an S3 client adapter, Kafka consumers/producers, and Russian report
generation. A real S3
service and model provider have not been used yet. The grading result must not
be published until those integrations and their failure paths are tested.

Run the current tests from this directory:

```bash
PYTHONPATH=src python3 -m unittest discover -s tests -v
python3 -m compileall -q src tests
```

Python 3.11 or newer is required. These tests use only the standard library and
do not contact Kafka, S3, or a model provider.
