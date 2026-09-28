import contextlib
import io
import json
import unittest
from unittest.mock import patch

from llm_service.__main__ import main
from test_yandex_model import SNAPSHOT, full_checks, successful_response


class ModelCliTests(unittest.TestCase):
    def test_cli_returns_complete_json_without_semantic_rejection(self):
        checks = full_checks()
        checks[0]["deductions"] = [{"deduction_id": "example", "code": "DOC_PURPOSE_UNCLEAR",
                                   "raw_points_lost": 3, "facts": {}, "evidence": []}]
        stdout, stderr = io.StringIO(), io.StringIO()
        with patch("llm_service.__main__.local_snapshot", return_value=SNAPSHOT), \
                patch.dict("os.environ", {"YANDEX_AI_API_KEY": "test-key",
                                          "YANDEX_FOLDER_ID": "test-folder"}), \
                patch("llm_service.yandex_model.urlopen", return_value=successful_response(
                    json.dumps({"checks": checks}))) as send, \
                contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            status = main(["--repo", "."])
        self.assertEqual(status, 0)
        self.assertEqual(send.call_count, 1)
        result = json.loads(stdout.getvalue())
        self.assertEqual(set(result), {"grade", "subcategories", "facts"})
        self.assertEqual(len(result["subcategories"]), 8)
        self.assertEqual(result["facts"][0]["code"], "DOC_PURPOSE_UNCLEAR")
        self.assertNotIn("test-key", stdout.getvalue() + stderr.getvalue())

    def test_dry_run_does_not_require_credentials_or_call_model(self):
        stdout = io.StringIO()
        with patch("llm_service.__main__.local_snapshot", return_value=SNAPSHOT), \
                patch("llm_service.yandex_model.urlopen") as send, \
                contextlib.redirect_stdout(stdout):
            self.assertEqual(main(["--dry-run"]), 0)
        send.assert_not_called()
        self.assertIn("README.md", json.loads(stdout.getvalue())["selected_files"])

    def test_cli_handles_format_failure_without_traceback_or_partial_stdout(self):
        stdout, stderr = io.StringIO(), io.StringIO()
        with patch("llm_service.__main__.local_snapshot", return_value=SNAPSHOT), \
                patch.dict("os.environ", {"YANDEX_AI_API_KEY": "test-key",
                                          "YANDEX_FOLDER_ID": "test-folder",
                                          "LLM_DOCS_MODE": "llm"}), \
                patch("llm_service.yandex_model.urlopen", side_effect=[
                    successful_response("not JSON"), successful_response("not JSON")]), \
                contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            self.assertEqual(main([]), 1)
        self.assertEqual(stdout.getvalue(), "")
        self.assertIn("Ошибка:", stderr.getvalue())
        self.assertNotIn("Traceback", stderr.getvalue())
