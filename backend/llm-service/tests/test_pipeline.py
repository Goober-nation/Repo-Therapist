import json
import unittest

from llm_service.context import prepare_documentation_context
from llm_service.grading import CHECKS, COMPONENTS
from llm_service.model_response import InvalidModelResponse, validate_model_response


class FakeStore:
    objects = {
        "repos/id/README.md": b"# Demo\n\nA useful project.\n",
        "repos/id/LICENSE": b"MIT License\nPermission granted.\n",
        "repos/id/CONTRIBUTING.md": b"# Contributing\n\nOpen a PR.\n",
        "repos/id/CODEOWNERS": b"* @team\n",
        "repos/id/src/main.py": b"print('hello')\n",
    }

    def list_objects(self, prefix):
        return {key: (len(value), str(hash(value))) for key, value in self.objects.items()
                if key.startswith(prefix)}

    def read_object(self, key):
        return self.objects[key]


def model_checks():
    return [{"check_id": check_id, "check_type": spec.kind, "outcome": "EVALUATED",
             "points_awarded": spec.maximum, "max_points": spec.maximum,
             "components": dict(COMPONENTS[check_id]), "deductions": []}
            for check_id, spec in CHECKS.items() if check_id in COMPONENTS]


class PipelineTests(unittest.TestCase):
    def test_s3_context_through_verified_model_result(self):
        snapshot, context = prepare_documentation_context(FakeStore(), "repos/id")
        checks = model_checks()
        purpose = checks[0]
        purpose["components"]["summary_clarity"] = 0
        purpose["points_awarded"] = 7
        purpose["deductions"] = [{
            "deduction_id": "purpose.summary:clarity", "code": "DOC_PURPOSE_UNCLEAR",
            "raw_points_lost": 1, "facts": {"unclear_aspects": ["summary_clarity"]},
            "evidence": [{"type": "file_content", "path": "README.md", "start_line": 3,
                          "end_line": 3, "quote": "A useful project."}],
        }]
        response = json.dumps({"checks": checks}).encode()
        result = validate_model_response(response, snapshot, context)
        self.assertEqual(result["score"]["value"], 99)
        self.assertEqual(result["problems"][0]["score_impact"]["category_points_lost"], 1)
        self.assertEqual(result["context"]["sha256"], context.sha256)
        self.assertNotIn("hello", context.text)

        checks[0]["deductions"][0]["evidence"][0]["quote"] = "invented claim"
        with self.assertRaisesRegex(InvalidModelResponse, "exact excerpt"):
            validate_model_response(json.dumps({"checks": checks}).encode(), snapshot, context)


if __name__ == "__main__":
    unittest.main()
