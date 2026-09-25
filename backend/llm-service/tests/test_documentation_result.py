import copy
import json
import unittest
from pathlib import Path

from llm_service.contracts import (InvalidEvent, UnsupportedEventVersion, build_documentation_completed,
                                   parse_documentation_result, parse_request, validate_documentation_payload)
from llm_service.documentation_result import documentation_payload
from llm_service.formatted_response import parse_formatted_response
from llm_service.context import build_model_context
from llm_service.snapshot import Snapshot
from test_model_response import SNAPSHOT, CONTEXT, full_checks
from test_contracts import envelope, REPOSITORY, encode, CATEGORIES


def compact():
    return documentation_payload(parse_formatted_response(
        json.dumps({"checks": full_checks()}).encode(), SNAPSHOT, CONTEXT))


class DocumentationResultTests(unittest.TestCase):
    def test_published_contract_examples_match_parsers(self):
        doc = (Path(__file__).resolve().parents[3] / "docs/kafka-contracts.md").read_text()
        for event_type, parser in (("documentation.analysis.completed", parse_documentation_result),
                                   ("documentation.analysis.requested", parse_request),
                                   ("report.generation.requested", parse_request)):
            with self.subTest(event_type=event_type):
                section = doc.split(f"### `{event_type}`", 1)[1]
                raw = section.split("```json\n", 1)[1].split("\n```", 1)[0].encode()
                self.assertEqual(parser(raw)["event_type"], event_type)

    def test_compact_payload_and_envelope_roundtrip(self):
        payload = compact()
        self.assertEqual(set(payload), {"grade", "subcategories", "facts"})
        self.assertEqual(payload["grade"], 100)
        self.assertEqual(len(payload["subcategories"]), 8)
        self.assertEqual(payload["facts"], [])
        request = envelope("documentation.analysis.requested", {"s3_prefix": f"repos/{REPOSITORY}"})
        request["commit_sha"] = "a" * 40
        result = build_documentation_completed(request, payload)
        self.assertEqual(result["event_version"], 2)
        self.assertEqual(result["request_event_id"], request["event_id"])
        for key in ("analysis_id", "repository_id", "data_scope", "commit_sha"):
            self.assertEqual(result[key], request[key])
        self.assertEqual(parse_documentation_result(encode(result)), result)
        result["event_version"] = 1
        with self.assertRaises(UnsupportedEventVersion):
            parse_documentation_result(encode(result))

    def test_missing_objective_files_are_in_flat_facts(self):
        files = {k: v for k, v in SNAPSHOT.files.items() if k != "LICENSE"}
        snapshot = Snapshot(frozenset(files), files, {})
        details = parse_formatted_response(json.dumps({"checks": full_checks()}).encode(),
                                           snapshot, build_model_context(snapshot))
        payload = documentation_payload(details)
        self.assertEqual(payload["grade"], 95)
        self.assertEqual(payload["facts"][0]["subcategory"], "required_files")
        self.assertEqual(payload["facts"][0]["code"], "DOC_LICENSE_MISSING")
        self.assertNotIn("raw_points_lost", payload["facts"][0])

    def test_inapplicable_group_is_null_and_does_not_dilute_score(self):
        checks = full_checks()
        for check in checks:
            if check["check_id"].startswith("build."):
                check.update(outcome="NOT_APPLICABLE", components=None, points_awarded=None)
        payload = documentation_payload(parse_formatted_response(
            json.dumps({"checks": checks}).encode(), SNAPSHOT, CONTEXT))
        self.assertEqual(payload["grade"], 100)
        self.assertIsNone(next(c for c in payload["subcategories"] if c["id"] == "build")["grade"])

    def test_format_rejects_extra_fields_duplicates_and_non_numeric_grade(self):
        for field, value in (("grade", True), ("grade", float("nan")), ("checks", [])):
            with self.subTest(field=field, value=value):
                payload = compact()
                payload[field] = value
                with self.assertRaises(InvalidEvent):
                    validate_documentation_payload(payload)
        payload = compact()
        payload["subcategories"][1] = copy.deepcopy(payload["subcategories"][0])
        with self.assertRaises(InvalidEvent):
            validate_documentation_payload(payload)

    def test_report_v2_consumes_compact_documentation_without_impacts(self):
        documentation = compact()
        documentation["grade"] = 80
        documentation["facts"] = [{"id": "DOC-0001", "subcategory": "purpose",
                                    "code": "MODEL_CODE", "facts": {"text": "Не описано назначение"},
                                    "evidence": []}]
        categories = [{"category": name, "status": "CALCULATED", "score":
                       80 if name == "documentation" else 100} for name in sorted(CATEGORIES)]
        request = envelope("report.generation.requested", {
            "output_language": "ru", "scores": {"overall": {"status": "CALCULATED", "score": 97},
                                                     "categories": categories},
            "problems": [], "documentation": documentation})
        request["event_version"] = 2
        self.assertEqual(parse_request(encode(request))["payload"]["documentation"], documentation)
        request["payload"].pop("documentation")
        with self.assertRaisesRegex(InvalidEvent, "requires documentation"):
            parse_request(encode(request))
