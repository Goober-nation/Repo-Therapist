"""Kafka boundary for Documentation grading; no report worker yet."""

from __future__ import annotations

import json
import logging
import os
from dataclasses import dataclass

from .context import ContextBuildError, prepare_documentation_context
from .contracts import InvalidEvent, UnsupportedEventVersion
from .grade_wire import REQUEST_TOPIC, RESULT_TOPIC, parse_grade_request, build_grade_result
from .model_response import InvalidModelResponse
from .s3_store import S3Config, S3Store
from .snapshot import SnapshotError
from .yandex_model import (ModelInputTooLarge, ModelProviderError, ModelRateLimited,
                           YandexModel, YandexModelConfig)

LOG = logging.getLogger(__name__)


@dataclass(frozen=True)
class KafkaConfig:
    brokers: str = "localhost:9092"
    group_id: str = "llm-service-documentation-v1"

    @classmethod
    def from_env(cls) -> KafkaConfig:
        return cls(os.environ.get("KAFKA_BROKERS", "localhost:9092"),
                   os.environ.get("LLM_KAFKA_GROUP_ID", "llm-service-documentation-v1"))

    def __post_init__(self) -> None:
        if not self.brokers.strip() or not self.group_id.strip():
            raise ValueError("Kafka brokers and consumer group are required")


def _failure(exc: Exception) -> tuple[str, bool, str]:
    if isinstance(exc, UnsupportedEventVersion):
        return "UNSUPPORTED_EVENT_VERSION", False, "Версия запроса не поддерживается"
    if isinstance(exc, InvalidEvent):
        return "INVALID_EVENT", False, "Некорректный запрос на оценку документации"
    if isinstance(exc, SnapshotError):
        detail = str(exc)
        if "prefix not found" in detail:
            return "S3_PREFIX_NOT_FOUND", False, "Префикс репозитория не найден"
        if "changed" in detail or "selected file absent" in detail:
            return "S3_CONTENT_CHANGED", True, "Содержимое репозитория изменилось во время чтения"
        return "S3_UNAVAILABLE", True, "Не удалось прочитать репозиторий из S3"
    if isinstance(exc, (ContextBuildError, ValueError)):
        return "CONTEXT_BUILD_FAILED", False, "Не удалось подготовить контекст документации"
    if isinstance(exc, ModelInputTooLarge):
        return "CONTEXT_BUILD_FAILED", False, "Контекст превышает лимит модели"
    if isinstance(exc, InvalidModelResponse):
        return "LLM_INVALID_RESPONSE", True, "Модель вернула ответ неверного формата"
    if isinstance(exc, ModelRateLimited):
        return "LLM_RATE_LIMITED", True, "Лимит запросов к модели временно исчерпан"
    if isinstance(exc, ModelProviderError):
        return "LLM_PROVIDER_UNAVAILABLE", True, "Провайдер модели временно недоступен"
    return "INTERNAL_ERROR", True, "Внутренняя ошибка LLM-сервиса"


def process_documentation_request(raw: bytes, store, model) -> dict:
    """Build the agreed flat result; failures never masquerade as a grade."""
    try:
        request = parse_grade_request(raw)
        snapshot, context = prepare_documentation_context(store, request["s3_prefix"])
        return build_grade_result(request, model.grade(snapshot, context))
    except Exception as exc:
        code, _, _ = _failure(exc)
        LOG.warning("Documentation analysis failed: %s", code)
        # No failure event is defined by the Grade service contract. Stop without
        # committing: restarting the worker retries this input record.
        raise RuntimeError(f"Documentation request failed: {code}") from None


def publish_terminal(producer, event: dict, *, timeout: float = 30.0) -> None:
    """Block until broker acknowledgement; caller may then commit the input offset."""
    error = []
    acknowledged = []

    def delivered(err, _message):
        if err is not None:
            error.append(err)
        else:
            acknowledged.append(True)

    producer.produce(RESULT_TOPIC, key=event["sourcecraft_uuid"].encode("utf-8"),
                     value=json.dumps(event, ensure_ascii=False, allow_nan=False).encode("utf-8"),
                     on_delivery=delivered)
    if producer.flush(timeout) != 0 or error or not acknowledged:
        raise RuntimeError("Kafka did not acknowledge Documentation result")


def run_worker(config: KafkaConfig, store, model, *, consumer=None, producer=None) -> None:
    if consumer is None or producer is None:
        from confluent_kafka import Consumer, Producer

        if consumer is None:
            consumer = Consumer({"bootstrap.servers": config.brokers, "group.id": config.group_id,
                                 "enable.auto.commit": False, "auto.offset.reset": "earliest",
                                 "max.poll.interval.ms": 900_000})
        if producer is None:
            producer = Producer({"bootstrap.servers": config.brokers,
                                 "enable.idempotence": True, "acks": "all"})
    try:
        consumer.subscribe([REQUEST_TOPIC])
        LOG.info("Listening on %s", REQUEST_TOPIC)
        while True:
            message = consumer.poll(1.0)
            if message is None:
                continue
            if message.error():
                raise RuntimeError(f"Kafka consumer error: {message.error()}")
            event = process_documentation_request(message.value(), store, model)
            if event is not None:
                publish_terminal(producer, event)
            committed = consumer.commit(message=message, asynchronous=False)
            if any(partition.error for partition in committed):
                raise RuntimeError("Kafka did not commit input offset")
    finally:
        consumer.close()


def main() -> None:
    logging.basicConfig(level=logging.INFO)
    store = S3Store(S3Config.from_env())
    model = YandexModel(YandexModelConfig.from_env())
    try:
        run_worker(KafkaConfig.from_env(), store, model)
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
