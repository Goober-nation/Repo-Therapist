import io
import json
import os
import ssl
import unittest
from unittest.mock import patch
from urllib.error import HTTPError, URLError

from llm_service.context import build_model_context
from llm_service.grading import CHECKS, COMPONENTS
from llm_service.model_response import InvalidModelResponse
from llm_service.snapshot import Snapshot
from llm_service.yandex_model import (API_URL, ModelProviderError, YandexModel,
                                      YandexModelConfig, GRADING_RESPONSE_SCHEMA,
                                      build_grading_prompt)


FILES = {"README.md": "# Demo\n\nA useful project.\n",
         "LICENSE": "MIT License\nPermission granted.\n",
         "CONTRIBUTING.md": "# Contributing\n\nOpen a PR.\n",
         "CODEOWNERS": "* @team\n"}
SNAPSHOT = Snapshot(frozenset(FILES), FILES, {})
CONTEXT = build_model_context(SNAPSHOT)


def successful_response(text):
    return io.BytesIO(json.dumps({
        "status": "completed",
        "output": [{"type": "message", "content": [{"type": "output_text", "text": text}]}],
    }).encode())


def full_checks():
    return [{"check_id": check_id, "check_type": spec.kind, "outcome": "EVALUATED",
             "points_awarded": spec.maximum, "max_points": spec.maximum,
             "components": dict(COMPONENTS[check_id]), "deductions": []}
            for check_id, spec in CHECKS.items() if check_id in COMPONENTS]


class YandexModelTests(unittest.TestCase):
    def setUp(self):
        self.model = YandexModel(YandexModelConfig("private-key", "folder-123"))

    def test_reads_configuration_without_exposing_key(self):
        with patch.dict(os.environ, {"YANDEX_AI_API_KEY": "private-key",
                                  "YANDEX_FOLDER_ID": "folder-123"}):
            config = YandexModelConfig.from_env()
        self.assertEqual(config.model, "aliceai-llm")
        self.assertNotIn("private-key", repr(config))
        with self.assertRaises(ValueError):
            YandexModelConfig("", "folder-123")

    def test_request_uses_yandex_uri_and_extracts_only_completed_text(self):
        with patch("llm_service.yandex_model.urlopen", return_value=successful_response("готово")) as send:
            self.assertEqual(self.model.complete("Ответь: готово", max_output_tokens=100),
                             "готово".encode())
        request = send.call_args.args[0]
        self.assertEqual(request.full_url, API_URL)
        self.assertEqual(request.get_header("Authorization"), "Api-Key private-key")
        self.assertEqual(send.call_args.kwargs["timeout"], 120)
        body = json.loads(request.data)
        self.assertEqual(body["model"], "gpt://folder-123/aliceai-llm")
        self.assertEqual(body["temperature"], 0)
        self.assertEqual(body["max_output_tokens"], 100)
        self.assertNotIn("text", body)

    def test_grading_checks_format_and_returns_summary(self):
        raw = json.dumps({"checks": full_checks()})
        with patch("llm_service.yandex_model.urlopen", return_value=successful_response(raw)) as send:
            result = self.model.grade(SNAPSHOT, CONTEXT)
        self.assertEqual(result["grade"], 100)
        self.assertEqual(set(result), {"grade", "subcategories", "facts"})
        prompt = json.loads(send.call_args.args[0].data)["input"]
        self.assertIn(CONTEXT.text, prompt)
        self.assertIn(CONTEXT.sha256, prompt)
        self.assertIn("DOC_PURPOSE_MISSING", prompt)
        self.assertNotIn("private-key", prompt)
        response_format = json.loads(send.call_args.args[0].data)["text"]["format"]
        self.assertEqual(response_format["type"], "json_schema")
        self.assertEqual(response_format["name"], "documentation_grading")
        self.assertEqual(response_format["schema"], GRADING_RESPONSE_SCHEMA)
        self.assertEqual(response_format["schema"]["required"], ["checks"])
        self.assertFalse(response_format["schema"]["additionalProperties"])
        self.assertEqual(response_format["schema"]["properties"]["checks"]["minItems"],
                         len(COMPONENTS))
        self.assertEqual(response_format["schema"]["properties"]["checks"]["maxItems"],
                         len(COMPONENTS))
        with patch("llm_service.yandex_model.urlopen", side_effect=[
                successful_response('{"checks":[]}'),
                successful_response('{"checks":[]}'),
        ]):
            with self.assertRaises(InvalidModelResponse):
                self.model.grade(SNAPSHOT, CONTEXT)

    def test_retries_once_with_validator_feedback(self):
        invalid = successful_response('{"checks":[]}')
        valid = successful_response(json.dumps({"checks": full_checks()}))
        with patch("llm_service.yandex_model.urlopen", side_effect=[invalid, valid]) as send:
            result = self.model.grade(SNAPSHOT, CONTEXT)
        self.assertEqual(result["grade"], 100)
        self.assertEqual(send.call_count, 2)
        retry_prompt = json.loads(send.call_args_list[1].args[0].data)["input"]
        self.assertIn("VALIDATION_FEEDBACK", retry_prompt)
        self.assertIn("missing model checks", retry_prompt)

    def test_semantic_inconsistencies_return_unchanged_without_retry(self):
        checks = full_checks()
        checks[0]["points_awarded"] = 6
        checks[0]["deductions"] = [{
            "deduction_id": "model-chosen-id", "code": "MODEL_CODE", "raw_points_lost": 3,
            "facts": {"description": "Недочёт по мнению модели"},
            "evidence": [{"type": "file_content", "path": "README.md",
                          "start_line": 999, "end_line": 1, "quote": "Not in the file"},
                         {"type": "document_review", "paths": ["missing.md"],
                          "context_sha256": "not-the-context-hash"}],
        }]
        with patch("llm_service.yandex_model.urlopen", return_value=successful_response(
                json.dumps({"checks": checks}))) as send:
            result = self.model.grade(SNAPSHOT, CONTEXT)
        self.assertEqual(send.call_count, 1)
        self.assertEqual(result["grade"], 93.55)  # 29 / 31 evaluated points
        self.assertEqual(result["facts"][0]["facts"], checks[0]["deductions"][0]["facts"])
        self.assertEqual(result["facts"][0]["evidence"], checks[0]["deductions"][0]["evidence"])

    def test_wrong_field_type_is_still_rejected(self):
        checks = full_checks()
        checks[0]["points_awarded"] = "8"
        with patch("llm_service.yandex_model.urlopen", side_effect=[successful_response(
                json.dumps({"checks": checks})) for _ in range(2)]) as send:
            with self.assertRaisesRegex(InvalidModelResponse, "expected integer"):
                self.model.grade(SNAPSHOT, CONTEXT)
        self.assertEqual(send.call_count, 2)

    def test_fails_closed_on_provider_errors_and_context_overflow(self):
        with self.assertRaisesRegex(ModelProviderError, "220001 UTF-8 bytes"):
            self.model.complete("x" * 220_001)
        with patch("llm_service.yandex_model.urlopen", side_effect=HTTPError(
                API_URL, 401, "Unauthorized", {}, None)):
            with self.assertRaisesRegex(ModelProviderError, "HTTP 401") as caught:
                self.model.complete("hello")
        self.assertNotIn("private-key", str(caught.exception))
        with patch("llm_service.yandex_model.urlopen", return_value=io.BytesIO(
                b'{"status":"incomplete","output":[]}')):
            with self.assertRaisesRegex(ModelProviderError, "did not complete"):
                self.model.complete("hello")
        with patch("llm_service.yandex_model.urlopen", side_effect=URLError(
                ssl.SSLEOFError(8, "unexpected EOF"))):
            with self.assertRaisesRegex(ModelProviderError, "TLS connection"):
                self.model.complete("hello")

    def test_accepts_documented_top_level_output_text(self):
        payload = json.dumps({"status": "completed", "output_text": "готово"}).encode()
        with patch("llm_service.yandex_model.urlopen", return_value=io.BytesIO(payload)):
            self.assertEqual(self.model.complete("hello"), "готово".encode())

    def test_prompt_limits_model_to_supported_applicability_decisions(self):
        prompt = build_grading_prompt(CONTEXT, frozenset({"build.commands"}))
        rubric = json.loads(prompt.split("RUBRIC_JSON:\n", 1)[1].split(
            "\nREPOSITORY_CONTEXT_JSON:\n", 1)[0])
        self.assertEqual(rubric["allowed_not_applicable"], ["build.commands"])
        self.assertEqual(len(rubric["checks"]), 21)
        self.assertEqual(rubric["checks"][0]["components"], COMPONENTS["purpose.summary"])
        unclear = rubric["checks"][0]["deduction_codes"]["DOC_PURPOSE_UNCLEAR"]
        self.assertEqual(unclear["required_evidence_types"], ["file_content"])
        self.assertNotIn("detected_build_systems", rubric["checks"][7]["deduction_codes"][
            "DOC_BUILD_INSTRUCTIONS_MISSING"]["required_facts"])


if __name__ == "__main__":
    unittest.main()
