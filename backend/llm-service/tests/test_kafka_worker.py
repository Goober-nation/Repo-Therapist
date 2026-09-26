import json
import unittest

from llm_service.grading import GROUPS
from llm_service.kafka_worker import (KafkaConfig, process_documentation_request,
                                      publish_terminal, run_worker)
from llm_service.yandex_model import ModelInputTooLarge, ModelProviderError, ModelRateLimited

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
    def value(self):
        return request()

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
        self.assertEqual(set(event), {"sourcecraft_uuid", "grade", "subgrades", "advice", "evaluated_at"})
        self.assertEqual(event["sourcecraft_uuid"], REPOSITORY)
        self.assertEqual(event["subgrades"], {group: 95 for group in GROUPS})
        self.assertEqual(event["advice"], [])
        self.assertEqual(model.calls, 1)

    def test_processing_failure_does_not_publish_or_commit(self):
        events = []
        consumer, producer = Consumer(events), Producer(events)
        with self.assertRaisesRegex(RuntimeError, "LLM_PROVIDER_UNAVAILABLE"):
            run_worker(KafkaConfig(), FakeStore(), FakeModel(ModelProviderError("secret details")),
                       consumer=consumer, producer=producer)
        self.assertEqual(events, [])
        self.assertEqual(consumer.committed, 0)
        self.assertTrue(consumer.closed)

    def test_missing_s3_and_invalid_requests_fail_without_fake_grade(self):
        with self.assertRaisesRegex(RuntimeError, "S3_PREFIX_NOT_FOUND"):
            process_documentation_request(request(), FakeStore(missing=True), FakeModel())
        with self.assertRaisesRegex(RuntimeError, "INVALID_EVENT"):
            process_documentation_request(b"not json", FakeStore(), FakeModel())

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


if __name__ == "__main__":
    unittest.main()
