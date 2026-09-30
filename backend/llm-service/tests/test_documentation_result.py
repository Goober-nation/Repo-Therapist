import copy
import json
import unittest

from llm_service.contracts import InvalidEvent, validate_documentation_payload
from llm_service.documentation_result import documentation_payload
from llm_service.formatted_response import parse_formatted_response
from llm_service.context import build_model_context
from llm_service.snapshot import Snapshot
from test_model_response import SNAPSHOT, CONTEXT, full_checks


def compact():
    return documentation_payload(parse_formatted_response(
        json.dumps({"checks": full_checks()}).encode(), SNAPSHOT, CONTEXT))


class DocumentationResultTests(unittest.TestCase):
    def test_compact_payload_shape(self):
        payload = compact()
        self.assertEqual(set(payload), {"grade", "subcategories", "facts"})
        self.assertEqual(payload["grade"], 100)
        self.assertEqual(len(payload["subcategories"]), 8)
        self.assertEqual(payload["facts"], [])

    def test_missing_objective_files_are_in_flat_facts(self):
        files = {k: v for k, v in SNAPSHOT.files.items() if k != "LICENSE"}
        snapshot = Snapshot(frozenset(files), files, {})
        details = parse_formatted_response(json.dumps({"checks": full_checks()}).encode(),
                                           snapshot, build_model_context(snapshot))
        payload = documentation_payload(details)
        self.assertEqual(payload["grade"], 83.87)  # 26 / 31 after unreviewed checks are excluded
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
