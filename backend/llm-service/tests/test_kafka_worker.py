import json
import unittest
from unittest.mock import patch
import io

from llm_service.grading import GROUPS
from llm_service.kafka_worker import (KafkaConfig, process_documentation_request,
                                      publish_terminal, run_worker)
from llm_service.yandex_model import (ModelContentFiltered, ModelDeadlineExceeded, ModelInputTooLarge,
                                      ModelProviderError, ModelRateLimited,
                                      YandexModel, YandexModelConfig)

ANALYSIS = "018f47d1-e33b-7af3-a642-229ac22a9e82"
REPOSITORY = "019b2102-fa21-7ab7-8ce1-116af406fb5e"
EVENT = "018f47d2-6d5a-7b72-82fd-1e9834d57a91"


def request(prefix=None):
    return json.dumps({"sourcecraft_uuid": REPOSITORY,
                       "s3_prefix": prefix or f"repos/{REPOSITORY}",
                       "requested_at": "2026-09-21T10:15:30Z"}).encode()



class FakeStore:
    def __init__(self, missing=False):
        self.missing = missing

    def list_objects(self, prefix):
        if self.missing:
            return {}
        return {prefix + "README.md": (9, "etag")}

    def read_object(self, key):
        return b"# Example"


class FakeModel:
    def __init__(self, error=None):
        self.error = error
        self.calls = 0

    def grade(self, snapshot, context):
        self.calls += 1
        if self.error:
            raise self.error
        return {"grade": 95, "subcategories": [
            {"id": group, "grade": 95} for group in GROUPS], "facts": []}


class Message:
    def __init__(self, raw=None):
        self.raw = raw or request()

    def value(self):
        return self.raw

    def error(self):
        return None


class Consumer:
    def __init__(self, events):
        self.events = events
        self.committed = 0
        self.closed = False

    def subscribe(self, topics):
        self.topics = topics

    def poll(self, timeout):
        if self.committed:
            raise KeyboardInterrupt()
        return Message()

    def commit(self, **kwargs):
        self.events.append("commit")
        self.committed += 1
        return []

    def close(self):
        self.closed = True


class Producer:
    def __init__(self, events, error=None):
        self.events = events
        self.error = error

    def produce(self, topic, *, key, value, on_delivery):
        self.topic = topic
        self.events.append("produce")
        self.event = json.loads(value)
        self.key = key
        self.callback = on_delivery

    def flush(self, timeout):
        self.events.append("flush")
        self.callback(self.error, None)
        return 0


class KafkaWorkerTests(unittest.TestCase):
    def test_success_result_has_exact_flat_fields(self):
        model = FakeModel()
        event = process_documentation_request(request(), FakeStore(), model)
        self.assertEqual(set(event), {"sourcecraft_uuid", "grade", "subgrades", "advice", "error", "evaluated_at"})
        self.assertEqual(event["sourcecraft_uuid"], REPOSITORY)
        self.assertEqual(event["subgrades"], {group: 95 for group in GROUPS})
        self.assertEqual(event["advice"], [])
        self.assertEqual(event["error"], "")
        self.assertEqual(model.calls, 1)

    def test_model_failure_is_published_then_committed_and_worker_continues(self):
        events = []
        consumer, producer = Consumer(events), Producer(events)
        with self.assertRaises(KeyboardInterrupt):
            run_worker(KafkaConfig(), FakeStore(), FakeModel(ModelProviderError("secret details")),
                       consumer=consumer, producer=producer)
        self.assertEqual(events, ["produce", "flush", "commit"])
        self.assertEqual(producer.event["error"], "LLM_PROVIDER_UNAVAILABLE")
        self.assertIsNone(producer.event["grade"])
        self.assertNotIn("secret details", json.dumps(producer.event))
        self.assertEqual(consumer.committed, 1)
        self.assertTrue(consumer.closed)

    def test_content_filter_and_other_failures_are_not_fake_grades(self):
        cases = [(FakeStore(missing=True), FakeModel(), "S3_PREFIX_NOT_FOUND"),
                 (FakeStore(), FakeModel(ModelContentFiltered("refused")), "CONTENT_FILTER"),
                 (FakeStore(), FakeModel(ModelRateLimited("429")), "LLM_RATE_LIMITED"),
                 (FakeStore(), FakeModel(ModelDeadlineExceeded("too slow")), "LLM_TIMEOUT"),
                 (FakeStore(), FakeModel(ModelInputTooLarge("too large")), "CONTEXT_BUILD_FAILED")]
        for store, model, code in cases:
            with self.subTest(code=code):
                result = process_documentation_request(request(), store, model)
                self.assertEqual(result["error"], code)
                self.assertIsNone(result["grade"])
                self.assertEqual(result["subgrades"], {})
                self.assertEqual(result["advice"], [])

    def test_invalid_request_with_uuid_reports_error_without_uuid_is_skipped(self):
        body = json.loads(request())
        body["s3_prefix"] = "../invalid"
        self.assertEqual(process_documentation_request(json.dumps(body).encode(), FakeStore(),
                                                       FakeModel())["error"], "INVALID_EVENT")
        self.assertIsNone(process_documentation_request(b"not json", FakeStore(), FakeModel()))

    def test_offset_commit_follows_delivery_ack(self):
        events = []
        consumer, producer = Consumer(events), Producer(events)
        with self.assertRaises(KeyboardInterrupt):
            run_worker(KafkaConfig(), FakeStore(), FakeModel(), consumer=consumer, producer=producer)
        self.assertEqual(events, ["produce", "flush", "commit"])
        self.assertEqual(producer.key, REPOSITORY.encode())
        self.assertTrue(consumer.closed)
        self.assertEqual(consumer.topics, ["repo.docs.grade.requested"])
        self.assertEqual(producer.topic, "repo.docs.graded")

    def test_failed_delivery_does_not_commit_offset(self):
        events = []
        consumer, producer = Consumer(events), Producer(events, error=RuntimeError("broker"))
        with self.assertRaisesRegex(RuntimeError, "acknowledge"):
            run_worker(KafkaConfig(), FakeStore(), FakeModel(), consumer=consumer, producer=producer)
        self.assertEqual(consumer.committed, 0)
        self.assertTrue(consumer.closed)

    def test_failed_error_delivery_does_not_commit_offset(self):
        events = []
        consumer, producer = Consumer(events), Producer(events, error=RuntimeError("broker"))
        with self.assertRaisesRegex(RuntimeError, "acknowledge"):
            run_worker(KafkaConfig(), FakeStore(), FakeModel(ModelContentFiltered("refused")),
                       consumer=consumer, producer=producer)
        self.assertEqual(producer.event["error"], "CONTENT_FILTER")
        self.assertEqual(consumer.committed, 0)
        self.assertTrue(consumer.closed)

    def test_yandex_content_filter_publishes_failure_without_retry(self):
        model = YandexModel(YandexModelConfig("test-key", "test-folder"))
        raw = json.dumps({"status": "incomplete",
                          "incomplete_details": {"reason": "content_filter"}}).encode()
        with patch("llm_service.yandex_model.urlopen", return_value=io.BytesIO(raw)) as http:
            result = process_documentation_request(request(), FakeStore(), model)
        self.assertEqual(http.call_count, 1)
        self.assertEqual(result["error"], "CONTENT_FILTER")
        self.assertIsNone(result["grade"])

    def test_worker_processes_next_record_after_failure(self):
        class TwoMessages(Consumer):
            def poll(self, timeout):
                if self.committed == 2:
                    raise KeyboardInterrupt()
                return Message()

        class FirstFailure(FakeModel):
            def grade(self, snapshot, context):
                self.calls += 1
                if self.calls == 1:
                    raise ModelContentFiltered("refused")
                return {"grade": 95, "subcategories": [
                    {"id": group, "grade": 95} for group in GROUPS], "facts": []}

        events = []
        consumer, producer = TwoMessages(events), Producer(events)
        with self.assertRaises(KeyboardInterrupt):
            run_worker(KafkaConfig(), FakeStore(), FirstFailure(),
                       consumer=consumer, producer=producer)
        self.assertEqual(events, ["produce", "flush", "commit"] * 2)
        self.assertEqual(producer.event["error"], "")


if __name__ == "__main__":
    unittest.main()
