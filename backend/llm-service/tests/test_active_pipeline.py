"""Exercise the active Kafka -> S3 -> Yandex HTTP -> Kafka path offline."""
import json
from http.client import IncompleteRead
import unittest
from unittest.mock import patch

from llm_service.context import build_model_context, select_context_paths
from llm_service.formatted_response import parse_formatted_response
from llm_service.kafka_worker import KafkaConfig, run_worker
from llm_service.snapshot import Snapshot, load_snapshot
from llm_service.s3_store import S3Config, S3Store
from llm_service.yandex_model import YandexModel, YandexModelConfig, ModelInputTooLarge, ModelProviderError
from test_kafka_worker import Consumer, Producer, REPOSITORY
from test_s3_store import FakeS3
from test_snapshot import FakeStore
from test_yandex_model import full_checks, successful_response, SNAPSHOT, CONTEXT


class ActivePipelineTests(unittest.TestCase):
    def test_actual_adapter_request_and_wire_result(self):
        client = FakeS3()
        prefix = f"repos/{REPOSITORY}/"
        client.objects.clear()
        client.objects.update({
            prefix: b"", prefix + "docs/": b"",
            prefix + "README.md": b"# Demo\nA useful project.\n[Testing](manual/tests.md)\n",
            prefix + "manual/tests.md": b"# Testing\nRun pytest.\n",
            prefix + "src/secret.py": b"DO_NOT_SEND_SOURCE_CODE",
        })
        checks = full_checks()
        checks[0]["points_awarded"] = 7
        checks[0]["deductions"] = [{"deduction_id": "d1", "code": "DOC_PURPOSE_UNCLEAR",
                                    "raw_points_lost": 1,
                                    "facts": {"description": "Неясно назначение проекта"},
                                    "evidence": [{"type": "file_exists", "path": "README.md"}]}]
        events = []
        consumer, producer = Consumer(events), Producer(events)
        model = YandexModel(YandexModelConfig("test-key", "test-folder"))
        with patch("llm_service.yandex_model.urlopen", return_value=successful_response(
                json.dumps({"checks": checks}))) as http:
            with self.assertRaises(KeyboardInterrupt):
                run_worker(KafkaConfig(), S3Store(S3Config(access_key="a", secret_key="s"), client),
                           model, consumer=consumer, producer=producer)
        body = json.loads(http.call_args.args[0].data)
        self.assertEqual(body["model"], "gpt://test-folder/aliceai-llm")
        self.assertEqual(body["text"]["format"]["type"], "json_schema")
        context = json.loads(body["input"].split("REPOSITORY_CONTEXT_JSON:\n", 1)[1])
        self.assertLessEqual(len(json.dumps(context, ensure_ascii=False, separators=(",", ":")).encode()), 32000)
        self.assertNotIn("DO_NOT_SEND_SOURCE_CODE", body["input"])
        self.assertTrue(any(d["path"] == "manual/tests.md" for d in context["documents"]))
        self.assertEqual(http.call_count, 1)
        self.assertEqual(events, ["produce", "flush", "commit"])
        self.assertEqual(producer.topic, "repo.docs.graded")
        result = producer.event
        self.assertEqual(set(result), {"sourcecraft_uuid", "grade", "subgrades", "advice", "method", "error", "evaluated_at"})
        self.assertEqual(result["sourcecraft_uuid"], REPOSITORY)
        self.assertEqual(result["error"], "")
        self.assertEqual(producer.key.decode(), REPOSITORY)
        self.assertTrue(all(type(v) is int and 0 <= v <= 100 for v in result["subgrades"].values()))
        self.assertNotIn("build", result["subgrades"])
        self.assertEqual(result["method"], "llm")
        self.assertTrue(any("Уточните, какую задачу решает проект" in item["text"] for item in result["advice"]))
        self.assertFalse(any("Неясно назначение проекта" in item["text"] for item in result["advice"]))

    def test_folder_markers_are_not_repository_files(self):
        store = FakeStore()
        store.objects.update({"repos/123/": b"", "repos/123/docs/": b""})
        snapshot = load_snapshot(store, "repos/123", {"README.md"})
        self.assertEqual(snapshot.all_paths, {"README.md"})

    def test_json_escaping_cannot_overflow_context(self):
        names = ["Overview", "Installation", "Configuration", "Build", "Testing", "Usage", "Structure", "Requirements"]
        files = {f"docs/{name}.md": f"# {name}\n" + "\x01" * 4000 for name in names}
        context = build_model_context(Snapshot(frozenset(files), files, {}))
        data = json.loads(context.text)
        self.assertLessEqual(len(context.text.encode()), 32000)
        self.assertGreater(len(data["documents"]), 0)
        covered = {check for item in data["documents"] for check in item["check_ids"]}
        self.assertEqual({check for check, status in data["coverage"].items() if status == "sampled"}, covered)
        self.assertEqual(context.reviewed_paths, {item["path"] for item in data["documents"]})

    def test_named_root_documentation_is_discovered(self):
        paths = frozenset({"INSTALL.md", "BUILD.md", "TESTING.md", "README.md", "app.py"})
        self.assertEqual(select_context_paths(paths), set(paths) - {"app.py"})

    def test_not_applicable_cannot_emit_deficiency_facts(self):
        checks = full_checks()
        checks[0].update(outcome="NOT_APPLICABLE", points_awarded=None, components=None,
                         deductions=[{"deduction_id": "d", "code": "DOC_PURPOSE_UNCLEAR",
                                      "raw_points_lost": 1, "facts": {}, "evidence": []}])
        result = parse_formatted_response(json.dumps({"checks": checks}).encode(), SNAPSHOT, CONTEXT)
        self.assertEqual(next(c for c in result["checks"] if c["check_id"] == "purpose.summary")["deductions"], [])

    def test_schema_is_included_in_request_budget(self):
        model = YandexModel(YandexModelConfig("key", "folder"))
        with patch("llm_service.yandex_model.urlopen") as http:
            with self.assertRaises(ModelInputTooLarge):
                model.complete("hello", json_schema={"description": "x" * 220000})
            http.assert_not_called()

    def test_truncated_http_response_has_controlled_error(self):
        model = YandexModel(YandexModelConfig("key", "folder"))
        with patch("llm_service.yandex_model.urlopen", side_effect=IncompleteRead(b"partial")):
            with self.assertRaisesRegex(ModelProviderError, "transport failed"):
                model.complete("hello")

    def test_non_null_not_applicable_is_normalized_without_model_retry(self):
        checks = full_checks()
        checks[0]["outcome"] = "NOT_APPLICABLE"
        # Reproduce model output: score and components still contain numbers.
        model = YandexModel(YandexModelConfig("key", "folder"))
        with patch("llm_service.yandex_model.urlopen", return_value=successful_response(
                json.dumps({"checks": checks}))) as http:
            result = model.grade(SNAPSHOT, CONTEXT)
        self.assertEqual(http.call_count, 1)
        self.assertEqual(result["grade"], 100)
        self.assertIsNone(next(c for c in result["subcategories"] if c["id"] == "purpose")["grade"])
        self.assertEqual(result["facts"], [])

    def test_unreviewed_unused_values_do_not_block_grading(self):
        checks = full_checks()
        coverage = json.loads(CONTEXT.text)["coverage"]
        check = next(c for c in checks if coverage[c["check_id"]] == "not_reviewed")
        check.update(points_awarded="unused", components={}, deductions=None)
        result = parse_formatted_response(json.dumps({"checks": checks}).encode(), SNAPSHOT, CONTEXT)
        actual = next(c for c in result["checks"] if c["check_id"] == check["check_id"])
        self.assertEqual(actual["outcome"], "NOT_REVIEWED")
        self.assertIsNone(actual["points_awarded"])
        self.assertEqual(actual["deductions"], [])
