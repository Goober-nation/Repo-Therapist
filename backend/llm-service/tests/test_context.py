import json
import unittest

from llm_service.context import (ContextBuildError, allowed_not_applicable_checks, build_model_context,
                                 prepare_documentation_context, select_context_paths)
from llm_service.snapshot import Snapshot


class FakeStore:
    def __init__(self):
        self.objects = {
            "repos/id/README.md": "# App\n\nInstructions\n".encode(),
            "repos/id/docs/run.md": "Run this command.\n".encode(),
            "repos/id/pyproject.toml": b"[project]\nname='app'\n",
            "repos/id/src/app.py": b"print('not sent to model')",
            "repos/id/.env": b"TOKEN=secret",
            "repos/id/_meta/export.json": b"{}",
        }

    def list_objects(self, prefix):
        return {key: (len(value), str(hash(value))) for key, value in self.objects.items()
                if key.startswith(prefix)}

    def read_object(self, key):
        return self.objects[key]


class ContextTests(unittest.TestCase):
    def test_selects_complete_docs_and_manifests_without_source_or_secrets(self):
        snapshot, context = prepare_documentation_context(FakeStore(), "repos/id")
        self.assertEqual(context.total_file_count, 5)
        self.assertEqual(context.selected_file_count, 3)
        self.assertEqual(context.reviewed_paths, {"README.md", "docs/run.md"})
        self.assertEqual(set(snapshot.files), {"README.md", "docs/run.md", "pyproject.toml"})
        sent = json.loads(context.text)
        self.assertNotIn("TOKEN=secret", context.text)
        self.assertNotIn("not sent to model", context.text)
        self.assertIn("src/app.py", sent["repository_paths"])
        self.assertFalse(context.truncated)

    def test_context_limit_fails_without_partial_document_review(self):
        snapshot, _ = prepare_documentation_context(FakeStore(), "repos/id")
        with self.assertRaisesRegex(ContextBuildError, "byte limit"):
            build_model_context(snapshot, max_context_bytes=20)

    def test_inventory_limit_fails(self):
        with self.assertRaisesRegex(ContextBuildError, "path limit"):
            select_context_paths(frozenset({"README.md", "docs/run.md"}), max_paths=1)

    def test_applicability_comes_from_repository_artifacts(self):
        snapshot, context = prepare_documentation_context(FakeStore(), "repos/id")
        allowed = allowed_not_applicable_checks(snapshot, context)
        self.assertIn("build.commands", allowed)
        self.assertIn("testing.commands", allowed)
        snapshot = Snapshot(frozenset(snapshot.all_paths | {"Dockerfile", "tests/test_app.py"}),
                            snapshot.files, snapshot.hashes)
        allowed = allowed_not_applicable_checks(snapshot, context)
        self.assertNotIn("build.commands", allowed)
        self.assertNotIn("testing.commands", allowed)

    def test_nested_service_manifests_are_selected_but_dependency_manifests_are_not(self):
        store = FakeStore()
        store.objects = {
            "repos/id/README.md": b"# Multi-service\n\nRead the service docs.\n",
            "repos/id/backend/api/README.md": b"# API\n\nStart here.\n",
            "repos/id/backend/api/go.mod": b"module example/api\n",
            "repos/id/backend/api/Dockerfile": b"FROM golang:1.25\n",
            "repos/id/node_modules/dependency/package.json": b'{"scripts":{"build":"evil"}}',
        }
        snapshot, context = prepare_documentation_context(store, "repos/id")
        self.assertIn("backend/api/go.mod", snapshot.files)
        self.assertIn("backend/api/Dockerfile", snapshot.files)
        self.assertIn("backend/api/README.md", context.reviewed_paths)
        self.assertNotIn("node_modules/dependency/package.json", snapshot.files)
        facts = json.loads(context.text)["detected_facts"]
        self.assertNotIn("npm", {item["kind"] for item in facts.get("detected_package_managers", [])})


if __name__ == "__main__":
    unittest.main()
