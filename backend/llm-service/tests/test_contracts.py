import json
import unittest

from llm_service.contracts import (CATEGORIES, InvalidEvent, UnsupportedEventVersion,
                                   parse_request)


ANALYSIS = "018f47d1-e33b-7af3-a642-229ac22a9e82"
REPOSITORY = "019b2102-fa21-7ab7-8ce1-116af406fb5e"
EVENT = "018f47d2-6d5a-7b72-82fd-1e9834d57a91"


def envelope(event_type, payload):
    return {
        "event_id": EVENT,
        "event_type": event_type,
        "event_version": 1,
        "analysis_id": ANALYSIS,
        "repository_id": REPOSITORY,
        "data_scope": "public",
        "created_at": "2026-09-21T10:15:30Z",
        "payload": payload,
    }


def encode(event):
    return json.dumps(event).encode()


class ContractTests(unittest.TestCase):
    def test_documentation_request_uses_repository_prefix(self):
        event = envelope("documentation.analysis.requested", {"s3_prefix": f"repos/{REPOSITORY}"})
        self.assertEqual(parse_request(encode(event))["repository_id"], REPOSITORY)
        event["payload"]["s3_prefix"] = "repos/someone-else"
        with self.assertRaisesRegex(InvalidEvent, "s3_prefix"):
            parse_request(encode(event))

    def test_report_request_requires_all_categories_and_exact_deductions(self):
        categories = [{"category": name, "status": "CALCULATED", "score": 100}
                      for name in sorted(CATEGORIES)]
        event = envelope("report.generation.requested", {
            "output_language": "ru", "scores": {
                "overall": {"status": "CALCULATED", "score": 95},
                "categories": categories,
            }, "problems": [],
        })
        parse_request(encode(event))
        categories[0]["score"] = 90
        with self.assertRaisesRegex(InvalidEvent, "do not explain"):
            parse_request(encode(event))
        event["payload"]["problems"] = [{
            "id": "P-1", "category": categories[0]["category"], "code": "SOME_CODE",
            "severity": "LOW", "score_impact": {"category_points_lost": 10},
            "facts": {}, "evidence": [{"summary": "Verified by analyzer", "type": "metric"}],
        }]
        parse_request(encode(event))

    def test_duplicate_json_fields_and_unsupported_versions_are_rejected(self):
        event = envelope("documentation.analysis.requested", {"s3_prefix": f"repos/{REPOSITORY}"})
        event["event_version"] = 2
        with self.assertRaises(UnsupportedEventVersion):
            parse_request(encode(event))
        duplicated = encode(envelope("documentation.analysis.requested",
                                     {"s3_prefix": f"repos/{REPOSITORY}"}))
        duplicated = duplicated.replace(b'"event_version": 1',
                                        b'"event_version": 1, "event_version": 1')
        with self.assertRaisesRegex(InvalidEvent, "duplicate"):
            parse_request(duplicated)


if __name__ == "__main__":
    unittest.main()
