import json
import unittest
from datetime import datetime

from llm_service.contracts import InvalidEvent
from llm_service.grade_wire import parse_grade_request, build_grade_result, build_grade_failure
from llm_service.grade_wire import docs_request_correlation_if_valid
from llm_service.kafka_worker import process_documentation_request
from test_kafka_worker import request, FakeModel, FakeStore, REPOSITORY, JOB


class GradeWireTests(unittest.TestCase):
    def test_request_without_envelope(self):
        parsed = parse_grade_request(request())
        self.assertEqual(parsed["sourcecraft_uuid"], REPOSITORY)
        self.assertEqual(parsed["job_id"], JOB)

    def test_job_id_is_required(self):
        for value in [None, "", "not-a-uuid", JOB.upper()]:
            obj = json.loads(request())
            if value is None:
                del obj["job_id"]
            else:
                obj["job_id"] = value
            with self.subTest(job_id=value), self.assertRaises(InvalidEvent):
                parse_grade_request(json.dumps(obj).encode())

    def test_invalid_request_failure_echoes_job_id(self):
        obj = json.loads(request())
        obj["s3_prefix"] = "../escape"
        raw = json.dumps(obj).encode()
        self.assertEqual(docs_request_correlation_if_valid(raw), (REPOSITORY, JOB))
        event = process_documentation_request(raw, FakeStore(), FakeModel())
        self.assertEqual((event["job_id"], event["sourcecraft_uuid"], event["error"]),
                         (JOB, REPOSITORY, "INVALID_EVENT"))
        del obj["job_id"]
        self.assertIsNone(docs_request_correlation_if_valid(json.dumps(obj).encode()))
        self.assertIsNone(process_documentation_request(json.dumps(obj).encode(), FakeStore(), FakeModel()))

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
        self.assertEqual(event["job_id"], JOB)
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
        event = build_grade_failure(REPOSITORY, JOB, "CONTENT_FILTER")
        self.assertEqual(event["job_id"], JOB)
        self.assertEqual(set(event), {"job_id", "sourcecraft_uuid", "grade", "subgrades", "advice",
                                      "method", "error", "evaluated_at"})
        self.assertIsNone(event["method"])
        self.assertEqual(event["error"], "CONTENT_FILTER")
        self.assertIsNone(event["grade"])
        self.assertEqual(event["subgrades"], {})
