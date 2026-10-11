"""Admin overrides for the llm processes (same protocol as the Go runtimeconfig/kafkasync package).

The Aggregator publishes every override of an app.yaml key to the compacted topic config.updated
(key = the app.yaml path, value = {"key", "value", "updated_at"}, a tombstone on reset). This module
reads the topic from the first offset before the worker starts, then follows it in a daemon thread,
and reports the process's status (env-locked keys, static keys waiting for a restart) on
config.status so the Aggregator can refuse overrides the environment would shadow.
"""

from __future__ import annotations

import json
import logging
import threading
import time
import uuid
from datetime import datetime, timezone

from .runtime_config import Store

LOG = logging.getLogger(__name__)
TOPIC_UPDATES = "config.updated"
TOPIC_STATUS = "config.status"
REHYDRATE_TIMEOUT = 15.0


def apply_message(store: Store, key: bytes | None, value: bytes | None) -> None:
    """Apply one config.updated message to store; malformed messages are skipped."""
    if not key:
        return
    path = key.decode("utf-8", "replace")
    if value is None:
        store.delete_override(path)
        return
    try:
        update = json.loads(value)
        if update.get("key") != path or not isinstance(update.get("value"), str):
            raise ValueError("key or value mismatch")
    except (ValueError, AttributeError):
        LOG.warning("config: skipping malformed %s message for %r", TOPIC_UPDATES, path)
        return
    try:
        store.set_override(path, update["value"])
    except ValueError as exc:
        LOG.warning("config: override of %s not applied: %s", path, exc)


def status_payload(store: Store, process: str) -> bytes:
    return json.dumps({
        "service": process,
        "sections": list(store.sections),
        "env_locked": store.env_locked_paths(),
        "restart_pending": store.restart_pending(),
        "reported_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
    }).encode()


def _ensure_topics(brokers: str) -> None:
    from confluent_kafka.admin import AdminClient, NewTopic

    admin = AdminClient({"bootstrap.servers": brokers})
    topics = [NewTopic(t, num_partitions=1, replication_factor=1, config={"cleanup.policy": "compact"})
              for t in (TOPIC_UPDATES, TOPIC_STATUS)]
    for topic, future in admin.create_topics(topics, request_timeout=10).items():
        try:
            future.result()
        except Exception as exc:  # TOPIC_ALREADY_EXISTS is the normal case after the first start
            if "TOPIC_ALREADY_EXISTS" not in str(exc):
                LOG.warning("config: creating %s: %s", topic, exc)


def start(store: Store, process: str, consumer=None, producer=None) -> threading.Thread:
    """Rehydrate overrides from config.updated (bounded), publish status, follow in the background."""
    from confluent_kafka import OFFSET_BEGINNING, Consumer, Producer, TopicPartition

    brokers = store.get("KAFKA_BROKERS", "localhost:9092")
    if consumer is None or producer is None:
        try:
            _ensure_topics(brokers)
        except Exception as exc:
            LOG.warning("config: %s; admin overrides will apply once Kafka is reachable", exc)
        consumer = consumer or Consumer({
            "bootstrap.servers": brokers,
            # Every process reads the whole topic: a unique group, nothing committed.
            "group.id": f"config-{process}-{uuid.uuid4().hex[:8]}",
            "enable.auto.commit": False,
            "auto.offset.reset": "earliest",
        })
        producer = producer or Producer({"bootstrap.servers": brokers})
    partition = TopicPartition(TOPIC_UPDATES, 0, OFFSET_BEGINNING)
    deadline = time.monotonic() + REHYDRATE_TIMEOUT
    count = 0
    try:
        _, high = consumer.get_watermark_offsets(TopicPartition(TOPIC_UPDATES, 0), timeout=REHYDRATE_TIMEOUT)
        consumer.assign([partition])
        position = -1
        while position + 1 < high and time.monotonic() < deadline:
            msg = consumer.poll(1.0)
            if msg is None or msg.error():
                continue
            apply_message(store, msg.key(), msg.value())
            position = msg.offset()
            count += 1
        LOG.info("config: %d admin override message(s) read from %s", count, TOPIC_UPDATES)
    except Exception as exc:
        LOG.warning("config: rehydrating admin overrides failed (%s); starting without them for now", exc)
        consumer.assign([partition])

    def publish_status():
        try:
            producer.produce(TOPIC_STATUS, key=process.encode(), value=status_payload(store, process))
            producer.flush(10)
        except Exception as exc:
            LOG.warning("config: publishing status to %s: %s", TOPIC_STATUS, exc)

    store.on_change(lambda: threading.Thread(target=publish_status, daemon=True).start())
    publish_status()

    def follow():
        while True:
            try:
                msg = consumer.poll(1.0)
            except Exception as exc:
                LOG.warning("config: reading %s: %s", TOPIC_UPDATES, exc)
                time.sleep(2)
                continue
            if msg is None or msg.error():
                continue
            apply_message(store, msg.key(), msg.value())

    thread = threading.Thread(target=follow, name="config-sync", daemon=True)
    thread.start()
    return thread
