import copy
import unittest

from llm_service.grading import (CHECKS, COMPONENTS, deterministic_checks,
                                 grade_documentation, required_file_paths)


HASH = "a" * 64
FILES = {
    "README.md": "# Example\n\nA useful project description.\n",
    "LICENSE": "MIT License\nPermission is hereby granted...\n",
    "CONTRIBUTING.md": "# Contributing\n\nPlease open a pull request.\n",
    ".github/CODEOWNERS": "* @team\n",
}


def semantic_full():
    return {
        check_id: {
            "check_id": check_id,
            "check_type": spec.kind,
            "outcome": "EVALUATED",
            "points_awarded": spec.maximum,
            "max_points": spec.maximum,
            "components": dict(COMPONENTS[check_id]),
            "deductions": [],
        }
        for check_id, spec in CHECKS.items()
        if spec.kind != "DETERMINISTIC"
    }


def grade(files=FILES, semantic=None):
    return grade_documentation(files, semantic if semantic is not None else semantic_full(),
                               context_sha256=HASH, reviewed_paths={"README.md"} & set(files))


class GradingTests(unittest.TestCase):
    def test_perfect_result_has_eight_groups_and_no_loss(self):
        result = grade()
        self.assertEqual(result["score"], {"status": "CALCULATED", "value": 100.0, "scale": 100})
        self.assertEqual(len(result["checks"]), len(CHECKS))
        self.assertEqual(len(result["subcategories"]), 8)
        self.assertEqual(sum(c["max_points"] for c in result["checks"]), 100)
        self.assertEqual(result["problems"], [])

    def test_missing_required_file_is_evaluated_zero_with_verified_absence(self):
        files = {k: v for k, v in FILES.items() if k != "LICENSE"}
        result = grade(files)
        self.assertEqual(result["score"]["value"], 95)
        license_check = next(c for c in result["checks"] if c["check_id"] == "license.exists")
        self.assertEqual(license_check["outcome"], "EVALUATED")
        self.assertEqual(license_check["deductions"][0]["code"], "DOC_LICENSE_MISSING")
        self.assertEqual(license_check["deductions"][0]["evidence"][0]["type"], "file_missing")
        self.assertEqual(result["problems"][0]["score_impact"]["category_points_lost"], 5)
        self.assertEqual(result["problems"][0]["severity"], "HIGH")

    def test_not_applicable_and_no_data_renormalize_without_penalty(self):
        semantic = semantic_full()
        for check_id in ("build.commands", "build.configuration", "build.output"):
            semantic[check_id].update(outcome="NOT_APPLICABLE", points_awarded=None, components=None)
        semantic["usage.primary_example"].update(outcome="NO_DATA", points_awarded=None,
                                                 components=None)
        result = grade(semantic=semantic)
        self.assertEqual(result["score"]["value"], 100)
        self.assertEqual(result["score"]["status"], "CALCULATED_PARTIAL")
        self.assertEqual(result["coverage"]["applicable_points"], 90)
        self.assertEqual(result["coverage"]["evaluated_points"], 85)
        groups = {g["id"]: g for g in result["subcategories"]}
        self.assertEqual(groups["build"]["status"], "NOT_APPLICABLE")
        self.assertEqual(groups["usage"]["status"], "CALCULATED_PARTIAL")

    def test_deductions_must_match_lost_points_and_real_evidence(self):
        semantic = semantic_full()
        semantic["local_run.configuration"].update(
            points_awarded=2,
            components={"required_parameters": 0, "configuration_steps": 1, "secret_handling": 1},
            deductions=[{
            "deduction_id": "local_run.configuration:required-parameters",
            "code": "DOC_LOCAL_RUN_CONFIGURATION_INCOMPLETE",
            "raw_points_lost": 2,
            "facts": {"missing_aspects": ["required_parameters"]},
            "evidence": [{"type": "file_content", "path": "README.md", "start_line": 3,
                          "end_line": 3, "quote": "useful project"}],
        }])
        result = grade(semantic=semantic)
        self.assertEqual(result["score"]["value"], 98)
        self.assertEqual(next(g for g in result["subcategories"] if g["id"] == "local_run")["score"], 90)
        self.assertEqual(result["problems"][0]["source_checks"], ["local_run.configuration"])
        self.assertEqual(result["problems"][0]["score_impact"]["category_points_lost"], 2)

        bad = copy.deepcopy(semantic)
        bad["local_run.configuration"]["deductions"][0]["evidence"][0]["quote"] = "invented"
        with self.assertRaisesRegex(ValueError, "not verified"):
            grade(semantic=bad)

        bad = copy.deepcopy(semantic)
        bad["local_run.configuration"]["deductions"][0]["raw_points_lost"] = 1
        with self.assertRaisesRegex(ValueError, "do not explain"):
            grade(semantic=bad)

        bad = copy.deepcopy(semantic)
        bad["local_run.configuration"]["components"]["required_parameters"] = 1
        with self.assertRaisesRegex(ValueError, "component sum differs"):
            grade(semantic=bad)

    def test_missing_semantic_check_and_invalid_path_are_rejected(self):
        semantic = semantic_full()
        del semantic["purpose.summary"]
        with self.assertRaisesRegex(ValueError, "semantic checks differ"):
            grade(semantic=semantic)
        with self.assertRaisesRegex(ValueError, "relative and normalized"):
            deterministic_checks({"../README.md": "text"})

    def test_objective_files_can_be_selected_from_complete_listing(self):
        all_paths = set(FILES) | {"src/main.py"}
        selected = required_file_paths(all_paths)
        self.assertEqual(selected, set(FILES))
        result = grade_documentation(FILES, semantic_full(), context_sha256=HASH,
                                     reviewed_paths={"README.md"}, all_paths=all_paths)
        self.assertEqual(result["score"]["value"], 100)


if __name__ == "__main__":
    unittest.main()
