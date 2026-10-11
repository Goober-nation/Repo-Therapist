import json
import os
import time
from unittest import mock

from llm_service import config_sync
from llm_service.runtime_config import Store

YAML = """
llm:
  llm_docs_mode: algorithmic
admin_editable:
  - {key: llm.llm_docs_mode, type: enum, values: [auto, llm, algorithmic], group: llm, title: Режим}
"""


def make_store(tmp_path, env=None):
    path = tmp_path / "app.yaml"
    path.write_text(YAML, encoding="utf-8")
    with mock.patch.dict(os.environ, env or {}, clear=True):
        return Store(path)


def update(key, value):
    return json.dumps({"key": key, "value": value, "updated_at": "2026-10-11T00:00:00Z"}).encode()


def test_apply_set_and_tombstone(tmp_path):
    store = make_store(tmp_path)
    config_sync.apply_message(store, b"llm.llm_docs_mode", update("llm.llm_docs_mode", "auto"))
    assert store.get("LLM_DOCS_MODE") == "auto"
    config_sync.apply_message(store, b"llm.llm_docs_mode", None)
    assert store.get("LLM_DOCS_MODE") == "algorithmic"


def test_apply_skips_malformed_and_invalid(tmp_path):
    store = make_store(tmp_path)
    for value in (b"not json", update("other.key", "auto"), update("llm.llm_docs_mode", "magic")):
        config_sync.apply_message(store, b"llm.llm_docs_mode", value)
    assert store.overrides() == {}


def test_env_wins_over_kafka(tmp_path):
    store = make_store(tmp_path, {"LLM_DOCS_MODE": "llm"})
    config_sync.apply_message(store, b"llm.llm_docs_mode", update("llm.llm_docs_mode", "auto"))
    assert store.get("LLM_DOCS_MODE") == "llm"
    assert store.overrides() == {}
    status = json.loads(config_sync.status_payload(store, "llm-service"))
    assert status["env_locked"] == ["llm.llm_docs_mode"]
    assert status["service"] == "llm-service"


class FakeMessage:
    def __init__(self, offset, key, value):
        self._offset, self._key, self._value = offset, key, value

    def error(self):
        return None

    def offset(self):
        return self._offset

    def key(self):
        return self._key

    def value(self):
        return self._value


class FakeConsumer:
    def __init__(self, messages):
        self.messages = list(messages)

    def get_watermark_offsets(self, partition, timeout=None):
        return 0, len(self.messages)

    def assign(self, partitions):
        pass

    def poll(self, timeout):
        if self.messages:
            return self.messages.pop(0)
        time.sleep(timeout)  # a real consumer blocks up to timeout when the topic is idle
        return None


class FakeProducer:
    def __init__(self):
        self.produced = []

    def produce(self, topic, key=None, value=None):
        self.produced.append((topic, key, value))

    def flush(self, timeout=None):
        return 0


def test_start_rehydrates_before_returning_and_reports_status(tmp_path):
    store = make_store(tmp_path)
    consumer = FakeConsumer([
        FakeMessage(0, b"llm.llm_docs_mode", update("llm.llm_docs_mode", "llm")),
        FakeMessage(1, b"llm.llm_docs_mode", update("llm.llm_docs_mode", "auto")),
    ])
    producer = FakeProducer()
    config_sync.start(store, "llm-service", consumer=consumer, producer=producer)
    assert store.get("LLM_DOCS_MODE") == "auto"
    assert producer.produced and producer.produced[0][0] == config_sync.TOPIC_STATUS
    assert producer.produced[0][1] == b"llm-service"
