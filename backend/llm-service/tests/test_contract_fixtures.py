"""Wire examples in fixtures/contracts are the canonical shapes of the active Kafka contracts."""

import json
import unittest
from pathlib import Path

from llm_service.grade_wire import parse_grade_request, build_grade_failure
from llm_service.summary_wire import parse_summary_request, build_summary_failure

FIXTURES = Path(__file__).resolve().parent / "fixtures" / "contracts"


def raw(name):
    return (FIXTURES / name).read_bytes()


def load(name):
    return json.loads(raw(name))


def without_time(event):
    return {key: value for key, value in event.items() if key != "evaluated_at"}


class ContractFixtureTests(unittest.TestCase):
    def test_docs_request_fixture_parses(self):
        expected = load("repo.docs.grade.requested.json")["sourcecraft_uuid"]
        self.assertEqual(parse_grade_request(raw("repo.docs.grade.requested.json"))["sourcecraft_uuid"],
                         expected)

    def test_docs_result_fixtures_match_builder_shape(self):
        failure = load("repo.docs.graded.failure.json")
        built = build_grade_failure(failure["sourcecraft_uuid"], failure["error"])
        self.assertEqual(without_time(built), without_time(failure))
        self.assertEqual(set(load("repo.docs.graded.success.json")), set(built))

    def test_recommendations_request_fixture_parses(self):
        fixture = load("llm.recommendations.requested.json")
        parsed = parse_summary_request(raw("llm.recommendations.requested.json"))
        self.assertEqual(parsed["sourcecraft_uuid"], fixture["sourcecraft_uuid"])
        self.assertEqual(parsed["visibility"], fixture["visibility"])

    def test_recommendations_result_fixtures_match_builder_shape(self):
        failure = load("llm.recommendations.received.failure.json")
        built = build_summary_failure(failure["sourcecraft_uuid"], failure["visibility"],
                                      failure["error"], failure["model_used"])
        self.assertEqual(without_time(built), without_time(failure))
        self.assertEqual(set(load("llm.recommendations.received.success.json")), set(built))


if __name__ == "__main__":
    unittest.main()
