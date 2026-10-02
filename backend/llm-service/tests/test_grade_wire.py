import json
import unittest
from datetime import datetime

from llm_service.contracts import InvalidEvent
from llm_service.grade_wire import parse_grade_request, build_grade_result, build_grade_failure
from test_kafka_worker import request, FakeModel, REPOSITORY


class GradeWireTests(unittest.TestCase):
    def test_request_without_envelope(self):
        self.assertEqual(parse_grade_request(request())["sourcecraft_uuid"], REPOSITORY)

    def test_bad_requests(self):
        for field, value in [("sourcecraft_uuid", "bad"), ("s3_prefix", "../repo"),
                             ("requested_at", "2026-09-26"), ("requested_at", "2026-09-26T10:00:00")]:
            obj = json.loads(request())
            obj[field] = value
            with self.subTest(field=field, value=value), self.assertRaises(InvalidEvent):
                parse_grade_request(json.dumps(obj).encode())
        with self.assertRaises(InvalidEvent):
            parse_grade_request(b'{"sourcecraft_uuid":"x","sourcecraft_uuid":"y"}')

    def test_omit_unreviewed_round_scores_and_preserve_facts(self):
        payload = FakeModel().grade(None, None)
        payload["grade"] = 83.5
        for section in payload["subcategories"]:
            section["grade"] = 100
        payload["subcategories"][0]["grade"] = None
        payload["subcategories"][1]["grade"] = 92.5
        payload["facts"] = [{"id": "DOC-1", "subcategory": "purpose",
                             "code": "DOC_PURPOSE_UNCLEAR", "facts": {"unclear_aspects": ["summary"]},
                             "evidence": [{"path": "README.md", "quote": "Example"}]}]
        event = build_grade_result(parse_grade_request(request()), payload)
        self.assertEqual(event["grade"], 84)
        self.assertEqual(event["error"], "")
        self.assertEqual(event["subgrades"]["purpose"], 93)
        self.assertNotIn("required_files", event["subgrades"])
        self.assertTrue(all(type(v) is int for v in event["subgrades"].values()))
        self.assertEqual(event["advice"], [{
            "code": "DOC_PURPOSE_UNCLEAR", "priority": "high",
            "text": "Уточните, какую задачу решает проект, для чего его используют и какой результат он даёт."}])
        self.assertNotIn("README.md", event["advice"][0]["text"])
        self.assertNotIn("summary", event["advice"][0]["text"])
        self.assertEqual(event["method"], "llm")
        self.assertIsNotNone(datetime.fromisoformat(event["evaluated_at"].replace("Z", "+00:00")).tzinfo)

    def test_failure_contract_has_no_fake_score(self):
        event = build_grade_failure(REPOSITORY, "CONTENT_FILTER")
        self.assertEqual(set(event), {"sourcecraft_uuid", "grade", "subgrades", "advice",
                                      "method", "error", "evaluated_at"})
        self.assertIsNone(event["method"])
        self.assertEqual(event["error"], "CONTENT_FILTER")
        self.assertIsNone(event["grade"])
        self.assertEqual(event["subgrades"], {})
