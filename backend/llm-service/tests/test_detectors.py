import json
import unittest

from llm_service.context import build_model_context
from llm_service.detectors import detect_repository_facts
from llm_service.grading import CHECKS, COMPONENTS
from llm_service.model_response import InvalidModelResponse, validate_model_response
from llm_service.snapshot import Snapshot


def checks_at_maximum():
    return [{"check_id": check_id, "check_type": spec.kind, "outcome": "EVALUATED",
             "points_awarded": spec.maximum, "max_points": spec.maximum,
             "components": dict(COMPONENTS[check_id]), "deductions": []}
            for check_id, spec in CHECKS.items() if check_id in COMPONENTS]


class DetectorTests(unittest.TestCase):
    def setUp(self):
        self.files = {
            "README.md": "# Demo\n\nA documented project.\n",
            "LICENSE": "MIT License\nPermission granted.\n",
            "CONTRIBUTING.md": "# Contributing\n\nOpen a PR.\n",
            "CODEOWNERS": "* @team\n",
            "package.json": json.dumps({"scripts": {"build": "vite build", "test": "vitest run"}}),
            ".env.example": "API_URL=https://example.invalid\n",
            "Dockerfile": "FROM python:3.12\nCMD [\"python\", \"app.py\"]\n",
        }
        self.paths = frozenset(set(self.files) | {"tests/integration/test_api.py", "src/main.py"})
        self.snapshot = Snapshot(self.paths, self.files, {})

    def test_detects_only_file_backed_facts_and_includes_them_in_context(self):
        facts = detect_repository_facts(self.snapshot)
        self.assertIn("docker", {item["kind"] for item in facts["detected_build_systems"]})
        self.assertIn("npm_script", {item["kind"] for item in facts["detected_test_commands"]})
        self.assertEqual(facts["detected_test_types"], ["integration"])
        self.assertEqual(facts["detected_identifiers"], ["API_URL"])
        self.assertIn("src", {item["identifier"] for item in facts["detected_components"]})
        self.assertIn("cli", {item["kind"] for item in facts["detected_interfaces"]})
        context = build_model_context(self.snapshot)
        sent = json.loads(context.text)
        self.assertEqual(sent["detected_facts"], facts)
        self.assertNotIn("vitest run", str(sent["repository_paths"]))

    def test_injects_detector_facts_after_model_response(self):
        context = build_model_context(self.snapshot)
        checks = checks_at_maximum()
        build = next(item for item in checks if item["check_id"] == "build.commands")
        build["points_awarded"] = 0
        build["components"] = {name: 0 for name in build["components"]}
        build["deductions"] = [{
            "deduction_id": "build.commands:missing", "code": "DOC_BUILD_INSTRUCTIONS_MISSING",
            "raw_points_lost": 6, "facts": {"reviewed_paths": ["README.md"]},
            "evidence": [{"type": "document_review", "paths": ["README.md"],
                          "context_sha256": context.sha256},
                         {"type": "file_exists", "path": "Dockerfile"}],
        }]
        result = validate_model_response(json.dumps({"checks": checks}).encode(),
                                         self.snapshot, context)
        facts = build["deductions"][0]["facts"]
        self.assertNotIn("detected_build_systems", facts)
        actual = next(item for item in result["checks"] if item["check_id"] == "build.commands")
        self.assertEqual(actual["deductions"][0]["facts"]["detected_build_systems"],
                         detect_repository_facts(self.snapshot)["detected_build_systems"])
        self.assertEqual(result["score"]["value"], 94)

        build["deductions"][0]["facts"]["detected_build_systems"] = [{
            "kind": "docker", "identifier": "invented", "path": "Dockerfile",
        }]
        with self.assertRaisesRegex(InvalidModelResponse, "model supplied a detector fact"):
            validate_model_response(json.dumps({"checks": checks}).encode(), self.snapshot, context)

    def test_malformed_manifest_does_not_create_script_facts(self):
        files = dict(self.files)
        files["package.json"] = "{not-json"
        facts = detect_repository_facts(Snapshot(self.paths, files, {}))
        self.assertFalse(any(item["kind"] == "npm_script"
                             for item in facts.get("detected_build_systems", [])))
        self.assertFalse(any(item["kind"] == "npm_script"
                             for item in facts.get("detected_test_commands", [])))

    def test_nested_manifest_scripts_are_detected_with_their_own_source_path(self):
        files = {"backend/ui/package.json": json.dumps({
            "scripts": {"build": "vite build", "test": "vitest run"}
        })}
        paths = frozenset(set(files) | {"vendor/dep/package.json"})
        facts = detect_repository_facts(Snapshot(paths, files, {}))
        self.assertEqual(facts["detected_test_commands"], [{
            "kind": "npm_script", "identifier": "npm run test", "path": "backend/ui/package.json",
        }])
        self.assertNotIn("vendor/dep/package.json", {
            item["path"] for item in facts["detected_package_managers"]
        })

    def test_rejects_context_changed_after_model_input(self):
        original = Snapshot(self.paths, dict(self.files), {})
        context = build_model_context(original)
        altered_files = dict(self.files)
        altered_files["package.json"] = json.dumps({"scripts": {"test": "different command"}})
        altered = Snapshot(self.paths, altered_files, {})
        with self.assertRaisesRegex(InvalidModelResponse, "context changed"):
            validate_model_response(json.dumps({"checks": checks_at_maximum()}).encode(),
                                    altered, context)

    def test_source_build_and_tests_without_declared_command_remain_usable_facts(self):
        files = {name: content for name, content in self.files.items()
                 if name not in {"package.json", "Dockerfile"}}
        paths = frozenset(set(files) | {"src/main.go", "scripts/build.sh", "tests/test_api.py"})
        snapshot = Snapshot(paths, files, {})
        facts = detect_repository_facts(snapshot)
        self.assertIn("compiled_source", {item["kind"] for item in facts["detected_build_systems"]})
        self.assertIn("build_script", {item["kind"] for item in facts["detected_build_systems"]})
        self.assertEqual(facts["detected_test_types"], ["unit"])
        self.assertEqual(facts["detected_test_commands"], [])

        context = build_model_context(snapshot)
        checks = checks_at_maximum()
        testing = next(item for item in checks if item["check_id"] == "testing.commands")
        testing["points_awarded"] = 0
        testing["components"] = {name: 0 for name in testing["components"]}
        testing["deductions"] = [{
            "deduction_id": "testing.commands:missing",
            "code": "DOC_TESTING_INSTRUCTIONS_MISSING", "raw_points_lost": 5,
            "facts": {"reviewed_paths": ["README.md"]},
            "evidence": [{"type": "document_review", "paths": ["README.md"],
                          "context_sha256": context.sha256},
                         {"type": "file_exists", "path": "tests/test_api.py"}],
        }]
        result = validate_model_response(json.dumps({"checks": checks}).encode(),
                                         snapshot, context)
        actual = next(item for item in result["checks"] if item["check_id"] == "testing.commands")
        self.assertEqual(actual["deductions"][0]["facts"]["detected_test_commands"], [])
        self.assertEqual(result["score"]["value"], 95)


if __name__ == "__main__":
    unittest.main()
