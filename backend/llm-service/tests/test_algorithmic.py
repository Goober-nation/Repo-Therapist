import contextlib
import io
import json
import unittest
from unittest.mock import patch

from llm_service.__main__ import main
from llm_service.algorithmic import algorithmic_grade
from llm_service.context import build_model_context
from llm_service.grade_wire import build_grade_result, parse_grade_request
from llm_service.kafka_worker import (documentation_mode, documentation_model,
                                      process_documentation_request)
from llm_service.model_response import InvalidModelResponse
from llm_service.recommendations import ALGORITHMIC_NOTICE, RECOMMENDATIONS
from llm_service.snapshot import Snapshot
from llm_service.yandex_model import (ModelContentFiltered, ModelDeadlineExceeded,
                                      ModelInputTooLarge, ModelProviderError, ModelRateLimited)
from test_kafka_worker import FakeModel, FakeStore, request

REQUIRED = {"LICENSE": "MIT License\n", "CONTRIBUTING.md": "Open a PR.\n",
            "CODEOWNERS": "* @team\n"}
GOOD_README = """# Demo

Demo converts CSV files to JSON.

## Installation

```bash
pip install -e .
```

## Usage

Run `demo input.csv`.

## Testing

```bash
pytest
```

## Project structure

```
src/   application code
```
"""


def grade(files, extra=()):
    snapshot = Snapshot(frozenset(set(files) | set(extra)), files, {})
    return algorithmic_grade(snapshot, build_model_context(snapshot))


def subgrades(payload):
    return {item["id"]: item["grade"] for item in payload["subcategories"]}


class AlgorithmicGradeTests(unittest.TestCase):
    def test_present_sections_earn_80_percent_and_consistency_is_skipped(self):
        payload = grade({"README.md": GOOD_README, **REQUIRED},
                        extra=["pyproject.toml", "tests/test_app.py", "src/app.py"])
        grades = subgrades(payload)
        self.assertEqual(grades["required_files"], 100)
        for section in ("purpose", "local_run", "testing", "usage", "structure"):
            self.assertEqual(grades[section], 80, section)
        self.assertIsNone(grades["consistency"])
        self.assertEqual(payload["facts"], [])
        # 20 required + 80% of 15 + 20 + 10 + 10 + 3 section points, out of 78.
        self.assertEqual(payload["grade"], 85.13)

    def test_good_documentation_gets_only_the_notice_as_advice(self):
        payload = grade({"README.md": GOOD_README, **REQUIRED},
                        extra=["pyproject.toml", "tests/test_app.py", "src/app.py"])
        event = build_grade_result(parse_grade_request(request()), payload, llm_used=False)
        self.assertEqual(event["advice"], [ALGORITHMIC_NOTICE])
        self.assertEqual(event["method"], "algorithmic")

    def test_one_line_readme_loses_missing_sections(self):
        payload = grade({"README.md": "# Demo\n\nA tool.\n", **REQUIRED},
                        extra=["main.go", "main_test.go", "cmd/app/main.go"])
        grades = subgrades(payload)
        self.assertEqual((grades["required_files"], grades["purpose"]), (100, 80))
        for section in ("local_run", "build", "testing", "usage"):
            self.assertEqual(grades[section], 0, section)
        codes = {fact["code"] for fact in payload["facts"]}
        self.assertIn("DOC_LOCAL_RUN_MISSING", codes)
        self.assertTrue(codes <= set(RECOMMENDATIONS))
        self.assertLess(payload["grade"], 50)

    def test_sections_without_artifacts_are_not_applicable(self):
        payload = grade({"README.md": "# Notes\n\nPersonal notes.\n", **REQUIRED})
        grades = subgrades(payload)
        self.assertIsNone(grades["build"])
        self.assertIsNone(grades["testing"])
        self.assertNotIn("DOC_TESTING_INSTRUCTIONS_MISSING",
                         {fact["code"] for fact in payload["facts"]})

    def test_commands_in_code_count_under_unusual_headings(self):
        readme = "# Demo\n\nA tool.\n\n## Как начать\n\n```\ndocker compose up\ngo test ./...\n```\n"
        grades = subgrades(grade({"README.md": readme, **REQUIRED},
                                 extra=["main.go", "main_test.go"]))
        self.assertEqual((grades["local_run"], grades["testing"]), (80, 80))

    def test_linked_document_and_file_name_count(self):
        files = {"README.md": "# Demo\n\nA tool. See [setup](docs/setup.md).\n",
                 "docs/setup.md": "Steps for your machine.\n", **REQUIRED}
        self.assertEqual(subgrades(grade(files))["local_run"], 80)


class DocumentationModeTests(unittest.TestCase):
    def test_auto_falls_back_on_every_model_failure(self):
        failures = [ModelDeadlineExceeded("slow"), ModelRateLimited("429"),
                    ModelProviderError("402"), ModelContentFiltered("refused"),
                    ModelInputTooLarge("big"), InvalidModelResponse("bad")]
        for error in failures:
            with self.subTest(error=type(error).__name__):
                event = process_documentation_request(request(), FakeStore(),
                                                      FakeModel(error), "auto")
                self.assertEqual(event["error"], "")
                self.assertIsInstance(event["grade"], int)
                self.assertEqual(event["advice"][-1], ALGORITHMIC_NOTICE)

    def test_auto_falls_back_when_the_model_run_fails_midway(self):
        class BadPayloadModel(FakeModel):
            def grade(self, snapshot, context):
                payload = super().grade(snapshot, context)
                payload["grade"] = "not a number"
                return payload

        for model in (BadPayloadModel(), FakeModel(KeyError("checks")), FakeModel(RuntimeError("bug"))):
            with self.subTest(model=type(model).__name__, error=model.error):
                event = process_documentation_request(request(), FakeStore(), model, "auto")
                self.assertEqual(event["error"], "")
                self.assertIsInstance(event["grade"], int)
                self.assertEqual(event["advice"][-1], ALGORITHMIC_NOTICE)

    def test_llm_mode_keeps_midway_failures_visible(self):
        event = process_documentation_request(request(), FakeStore(),
                                              FakeModel(RuntimeError("bug")), "llm")
        self.assertEqual(event["error"], "INTERNAL_ERROR")
        self.assertIsNone(event["grade"])

    def test_repository_failures_are_not_masked(self):
        event = process_documentation_request(request(), FakeStore(missing=True),
                                              FakeModel(), "auto")
        self.assertEqual(event["error"], "S3_PREFIX_NOT_FOUND")
        self.assertIsNone(event["grade"])

    def test_llm_mode_keeps_errors_and_success_has_no_notice(self):
        failed = process_documentation_request(request(), FakeStore(),
                                               FakeModel(ModelRateLimited("429")), "llm")
        self.assertEqual(failed["error"], "LLM_RATE_LIMITED")
        success = process_documentation_request(request(), FakeStore(), FakeModel(), "auto")
        self.assertNotIn(ALGORITHMIC_NOTICE, success["advice"])
        self.assertEqual(success["method"], "llm")

    def test_algorithmic_mode_never_calls_model(self):
        model = FakeModel()
        event = process_documentation_request(request(), FakeStore(), model, "algorithmic")
        self.assertEqual(model.calls, 0)
        self.assertEqual(event["advice"][-1], ALGORITHMIC_NOTICE)

    def test_mode_setting_and_missing_credentials(self):
        with patch.dict("os.environ", {}, clear=True):
            self.assertEqual(documentation_mode(), "auto")
            self.assertIsNone(documentation_model("auto"))
            with self.assertRaises(ValueError):
                documentation_model("llm")
        with patch.dict("os.environ", {"LLM_DOCS_MODE": "llmm"}), self.assertRaises(ValueError):
            documentation_mode()

    def test_cli_grades_algorithmically_without_credentials(self):
        files = {"README.md": GOOD_README, **REQUIRED}
        snapshot = Snapshot(frozenset(files), files, {})
        stdout, stderr = io.StringIO(), io.StringIO()
        with patch("llm_service.__main__.local_snapshot", return_value=snapshot), \
                patch.dict("os.environ", {"LLM_DOCS_MODE": "algorithmic"}, clear=True), \
                contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            self.assertEqual(main([]), 0)
        self.assertEqual(set(json.loads(stdout.getvalue())), {"grade", "subcategories", "facts"})
        self.assertIn("без LLM", stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
