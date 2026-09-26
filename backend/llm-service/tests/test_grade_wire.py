import json
import unittest
from datetime import datetime
from pathlib import Path

from llm_service.contracts import InvalidEvent
from llm_service.grade_wire import parse_grade_request, build_grade_result
from test_kafka_worker import request, FakeModel, REPOSITORY


class GradeWireTests(unittest.TestCase):
    def test_documented_request_matches_parser(self):
        doc = (Path(__file__).resolve().parents[3] / "docs/kafka-contracts.md").read_text()
        section = doc.split("## Documentation grading: active", 1)[1]
        raw = section.split("```json\n", 1)[1].split("\n```", 1)[0].encode()
        self.assertEqual(parse_grade_request(raw)["sourcecraft_uuid"], REPOSITORY)

    def test_fact_titles_cover_registry(self):
        from llm_service.deductions import RULES
        from llm_service.grade_wire import FACT_NAMES
        self.assertEqual(set(FACT_NAMES), set(RULES))

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
        payload["subcategories"][0]["grade"] = None
        payload["subcategories"][1]["grade"] = 92.5
        payload["facts"] = [{"id": "DOC-1", "subcategory": "purpose",
                             "code": "DOC_PURPOSE_UNCLEAR", "facts": {"unclear_aspects": ["summary"]},
                             "evidence": [{"path": "README.md", "quote": "Example"}]}]
        event = build_grade_result(parse_grade_request(request()), payload)
        self.assertEqual(event["grade"], 84)
        self.assertEqual(event["subgrades"]["purpose"], 93)
        self.assertNotIn("required_files", event["subgrades"])
        self.assertTrue(all(type(v) is int for v in event["subgrades"].values()))
        self.assertIsInstance(event["advice"][0], str)
        self.assertIn("README.md", event["advice"][0])
        self.assertIn("summary", event["advice"][0])
        self.assertIsNotNone(datetime.fromisoformat(event["evaluated_at"].replace("Z", "+00:00")).tzinfo)
