import copy
import json
import unittest

from llm_service.context import build_model_context
from llm_service.grading import CHECKS, COMPONENTS
from llm_service.model_response import InvalidModelResponse, validate_model_response
from llm_service.snapshot import Snapshot


FILES = {"README.md": "# Demo\n\nA small documented project.\n",
         "LICENSE": "MIT License\nPermission granted.\n",
         "CONTRIBUTING.md": "# Contributing\n\nOpen a PR.\n",
         "CODEOWNERS": "* @team\n"}
SNAPSHOT = Snapshot(frozenset(FILES), FILES, {})
CONTEXT = build_model_context(SNAPSHOT)
HASH = CONTEXT.sha256


def full_checks():
    return [{"check_id": check_id, "check_type": spec.kind,
             "outcome": "EVALUATED", "points_awarded": spec.maximum,
             "max_points": spec.maximum, "components": dict(COMPONENTS[check_id]),
             "deductions": []}
            for check_id, spec in CHECKS.items() if check_id in COMPONENTS]


def evaluate(checks, **kwargs):
    return validate_model_response(json.dumps({"checks": checks}).encode(), SNAPSHOT, CONTEXT,
                                   **kwargs)


class ModelResponseTests(unittest.TestCase):
    def test_accepts_complete_structured_response_and_recomputes_score(self):
        result = evaluate(full_checks())
        self.assertEqual(result["score"]["value"], 100)
        self.assertEqual(result["context"]["sha256"], HASH)

    def test_recomputes_check_total_from_integer_components(self):
        checks = full_checks()
        summary = next(item for item in checks if item["check_id"] == "purpose.summary")
        summary["points_awarded"] = 8
        result = evaluate(checks)
        scored = next(item for item in result["checks"]
                      if item["check_id"] == "purpose.summary")
        self.assertEqual(scored["points_awarded"], sum(summary["components"].values()))

    def test_rejects_non_integer_component_values_before_summing(self):
        checks = full_checks()
        summary = next(item for item in checks if item["check_id"] == "purpose.summary")
        summary["components"]["project_identity"] = True
        with self.assertRaisesRegex(InvalidModelResponse, "integer components"):
            evaluate(checks)

    def test_derives_exact_quote_line_numbers_instead_of_trusting_model(self):
        checks = full_checks()
        summary = next(item for item in checks if item["check_id"] == "purpose.summary")
        summary["components"]["problem_statement"] -= 1
        summary["points_awarded"] -= 1
        summary["deductions"] = [{
            "deduction_id": "purpose.summary:unclear-problem",
            "code": "DOC_PURPOSE_UNCLEAR", "raw_points_lost": 1,
            "facts": {"unclear_aspects": ["problem_statement"]},
            "evidence": [{"type": "file_content", "path": "README.md",
                          "start_line": 900, "end_line": 901,
                          "quote": "A small documented project."}],
        }]
        result = evaluate(checks)
        scored = next(item for item in result["checks"]
                      if item["check_id"] == "purpose.summary")
        self.assertEqual(scored["deductions"][0]["evidence"][0]["start_line"], 3)
        self.assertEqual(scored["deductions"][0]["evidence"][0]["end_line"], 3)

    def test_rejects_file_evidence_quote_not_in_snapshot(self):
        checks = full_checks()
        summary = next(item for item in checks if item["check_id"] == "purpose.summary")
        summary["components"]["problem_statement"] -= 1
        summary["points_awarded"] -= 1
        summary["deductions"] = [{
            "deduction_id": "purpose.summary:unclear-problem",
            "code": "DOC_PURPOSE_UNCLEAR", "raw_points_lost": 1,
            "facts": {"unclear_aspects": ["problem_statement"]},
            "evidence": [{"type": "file_content", "path": "README.md",
                          "start_line": 3, "end_line": 3, "quote": "invented quote"}],
        }]
        with self.assertRaisesRegex(InvalidModelResponse, "not an exact excerpt"):
            evaluate(checks)

    def test_rejects_prose_extra_fields_and_duplicate_json_keys(self):
        response = json.dumps({"checks": full_checks(), "advice": "Just trust me"}).encode()
        with self.assertRaisesRegex(InvalidModelResponse, "only checks"):
            validate_model_response(response, SNAPSHOT, CONTEXT)
        response = b'{"checks":[],"checks":[]}'
        with self.assertRaisesRegex(InvalidModelResponse, "duplicate"):
            validate_model_response(response, SNAPSHOT, CONTEXT)
        checks = full_checks()
        checks[0]["explanation"] = "not allowed during grading"
        with self.assertRaisesRegex(InvalidModelResponse, "unknown or missing check fields"):
            evaluate(checks)

    def test_rejects_unknown_code_and_unregistered_facts(self):
        checks = full_checks()
        check = next(item for item in checks if item["check_id"] == "purpose.summary")
        check["points_awarded"] = 0
        check["components"] = {name: 0 for name in check["components"]}
        check["deductions"] = [{
            "deduction_id": "purpose.summary:missing", "code": "DOC_PURPOSE_MISSING",
            "raw_points_lost": 8,
            "facts": {"missing_aspects": ["project_identity"],
                      "reviewed_paths": ["README.md"]},
            "evidence": [{"type": "document_review", "paths": ["README.md"],
                          "context_sha256": HASH}],
        }]
        self.assertEqual(evaluate(checks)["score"]["value"], 92)
        bad = copy.deepcopy(checks)
        bad[0]["deductions"][0]["code"] = "DOC_INVENTED_CODE"
        with self.assertRaisesRegex(InvalidModelResponse, "not registered"):
            evaluate(bad)
        bad = copy.deepcopy(checks)
        bad[0]["deductions"][0]["facts"]["recommendation"] = "add prose"
        with self.assertRaisesRegex(InvalidModelResponse, "facts do not match schema"):
            evaluate(bad)
        bad = copy.deepcopy(checks)
        bad[0]["deductions"][0]["facts"]["missing_aspects"] = ["invented_component"]
        with self.assertRaisesRegex(InvalidModelResponse, "unknown component"):
            evaluate(bad)

    def test_rejects_unjustified_missing_data_and_applicability(self):
        checks = full_checks()
        build = next(item for item in checks if item["check_id"] == "build.commands")
        build.update(outcome="NOT_APPLICABLE", points_awarded=None, components=None)
        with self.assertRaisesRegex(InvalidModelResponse, "unjustified NOT_APPLICABLE"):
            evaluate(checks, allowed_not_applicable=frozenset())
        self.assertEqual(evaluate(checks)["score"]["status"], "CALCULATED")
        self.assertEqual(evaluate(checks, allowed_not_applicable=frozenset({"build.commands"}))
                         ["score"]["status"], "CALCULATED")

    def test_no_documents_use_verified_missing_file_not_fabricated_review(self):
        checks = full_checks()
        summary = checks[0]
        summary["points_awarded"] = 0
        summary["components"] = {name: 0 for name in summary["components"]}
        summary["deductions"] = [{
            "deduction_id": "purpose.summary:missing", "code": "DOC_PURPOSE_MISSING",
            "raw_points_lost": 8,
            "facts": {"missing_aspects": ["project_identity"], "reviewed_paths": []},
            "evidence": [{"type": "file_missing", "path": "README.md"}],
        }]
        empty = Snapshot(frozenset(), {}, {})
        context = build_model_context(empty)
        result = validate_model_response(json.dumps({"checks": checks}).encode(), empty, context)
        self.assertEqual(result["score"]["value"], 72)
        with self.assertRaises(InvalidModelResponse):
            evaluate(checks)

    def test_injects_empty_required_detector_list_when_inventory_has_no_match(self):
        checks = full_checks()
        usage = next(item for item in checks if item["check_id"] == "usage.primary_example")
        usage["components"]["usage_context"] = 0
        usage["points_awarded"] -= 1
        usage["deductions"] = [{
            "deduction_id": "usage.primary_example:no-example",
            "code": "DOC_USAGE_EXAMPLE_MISSING", "raw_points_lost": 1,
            "facts": {"reviewed_paths": ["README.md"]},
            "evidence": [{"type": "document_review", "paths": ["README.md"],
                          "context_sha256": HASH}],
        }]
        result = evaluate(checks)
        usage_result = next(item for item in result["checks"]
                            if item["check_id"] == "usage.primary_example")
        self.assertEqual(usage_result["deductions"][0]["facts"]["detected_interfaces"], [])

    def test_injects_zero_required_detector_count_when_inventory_has_no_match(self):
        checks = full_checks()
        structure = next(item for item in checks if item["check_id"] == "structure.overview")
        structure["components"]["component_roles"] = 0
        structure["points_awarded"] -= 1
        structure["deductions"] = [{
            "deduction_id": "structure.overview:missing-roles",
            "code": "DOC_STRUCTURE_OVERVIEW_INCOMPLETE", "raw_points_lost": 1,
            "facts": {"missing_aspects": ["component_roles"],
                      "documented_component_count": 0},
            "evidence": [{"type": "document_review", "paths": ["README.md"],
                          "context_sha256": HASH}],
        }]
        result = evaluate(checks, allowed_not_applicable=frozenset())
        structure_result = next(item for item in result["checks"]
                                if item["check_id"] == "structure.overview")
        self.assertEqual(
            structure_result["deductions"][0]["facts"]["detected_component_count"], 0)

    def test_rejects_unknown_enum_and_wrong_evidence_variant(self):
        checks = full_checks()
        usage = next(item for item in checks if item["check_id"] == "usage.primary_example")
        usage["components"]["usage_context"] = 0
        usage["points_awarded"] = 4
        usage["deductions"] = [{
            "deduction_id": "usage.primary_example:context",
            "code": "DOC_USAGE_EXAMPLE_INCOMPLETE", "raw_points_lost": 1,
            "facts": {"missing_aspects": ["usage_context"], "example_type": "alien"},
            "evidence": [{"type": "file_content", "path": "README.md", "start_line": 3,
                          "end_line": 3, "quote": "documented project"}],
        }]
        with self.assertRaisesRegex(InvalidModelResponse, "unknown enum"):
            evaluate(checks)
        usage["deductions"][0]["facts"]["example_type"] = "cli"
        usage["deductions"][0]["evidence"] = [{
            "type": "document_review", "paths": ["README.md"], "context_sha256": HASH,
        }]
        with self.assertRaisesRegex(InvalidModelResponse, "evidence type not allowed"):
            evaluate(checks)


if __name__ == "__main__":
    unittest.main()
