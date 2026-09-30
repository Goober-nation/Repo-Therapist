"""Kafka boundaries for Documentation grading and repository summaries."""

from __future__ import annotations

import json
import logging
import os
from dataclasses import dataclass

from .algorithmic import algorithmic_grade
from .context import ContextBuildError, prepare_documentation_context
from .contracts import InvalidEvent
from .grade_wire import (REQUEST_TOPIC, RESULT_TOPIC, parse_grade_request,
                         request_uuid_if_valid, build_grade_result, build_grade_failure)
from .model_response import InvalidModelResponse
from .s3_store import S3Config, S3Store
from .snapshot import SnapshotError
from . import summary_wire
from .summary import NoGradeData, generate_summary
from .summary_wire import (build_summary_failure, build_summary_result, parse_summary_request,
                           request_correlation_if_valid)
from .yandex_model import (ModelContentFiltered, ModelDeadlineExceeded, ModelInputTooLarge,
                           ModelProviderError, ModelRateLimited,
                           YandexModel, YandexModelConfig)

LOG = logging.getLogger(__name__)
DOCUMENTATION_MODES = ("auto", "llm", "algorithmic")


def _env_first(*keys: str, default: str) -> str:
    """First nonblank value among env vars keys; the KAFKA_* names shared with the
    Go services come first, the older LLM_SUMMARY_* names are kept as fallbacks."""
    for key in keys:
        value = os.environ.get(key, "").strip()
        if value:
            return value
    return default


@dataclass(frozen=True)
class KafkaConfig:
    brokers: str = "localhost:9092"
    group_id: str = "llm-service-documentation-v1"
    request_topic: str = REQUEST_TOPIC
    result_topic: str = RESULT_TOPIC

    @classmethod
    def from_env(cls) -> KafkaConfig:
        return cls(os.environ.get("KAFKA_BROKERS", "localhost:9092"),
                   os.environ.get("LLM_KAFKA_GROUP_ID", "llm-service-documentation-v1"),
                   _env_first("KAFKA_DOCS_GRADE_REQUESTED_TOPIC", default=cls.request_topic),
                   _env_first("KAFKA_DOCS_GRADED_TOPIC", default=cls.result_topic))

    def __post_init__(self) -> None:
        if not all(value.strip() for value in (self.brokers, self.group_id,
                                               self.request_topic, self.result_topic)):
            raise ValueError("Kafka brokers, consumer group and topics are required")


@dataclass(frozen=True)
class SummaryKafkaConfig:
    brokers: str = "localhost:9092"
    group_id: str = "llm-service-recommendations-v1"
    request_topic: str = summary_wire.REQUEST_TOPIC
    result_topic: str = summary_wire.RESULT_TOPIC

    @classmethod
    def from_env(cls) -> SummaryKafkaConfig:
        env = os.environ.get
        return cls(env("KAFKA_BROKERS", cls.brokers),
                   env("LLM_SUMMARY_KAFKA_GROUP_ID", cls.group_id),
                   _env_first("KAFKA_LLM_REQUESTS_TOPIC", "LLM_SUMMARY_REQUEST_TOPIC",
                              default=cls.request_topic),
                   _env_first("KAFKA_LLM_RECOMMENDATIONS_TOPIC", "LLM_SUMMARY_RESULT_TOPIC",
                              default=cls.result_topic))

    def __post_init__(self) -> None:
        if not all(value.strip() for value in (self.brokers, self.group_id,
                                               self.request_topic, self.result_topic)):
            raise ValueError("Kafka brokers, consumer group and topics are required")


def _failure(exc: Exception) -> tuple[str, bool, str]:
    if isinstance(exc, InvalidEvent):
        return "INVALID_EVENT", False, "Некорректный запрос на оценку документации"
    if isinstance(exc, SnapshotError):
        detail = str(exc)
        if "prefix not found" in detail:
            return "S3_PREFIX_NOT_FOUND", False, "Префикс репозитория не найден"
        if "changed" in detail or "selected file absent" in detail:
            return "S3_CONTENT_CHANGED", True, "Содержимое репозитория изменилось во время чтения"
        return "S3_UNAVAILABLE", True, "Не удалось прочитать репозиторий из S3"
    # InvalidModelResponse is a ValueError, so it must be matched first.
    if isinstance(exc, InvalidModelResponse):
        return "LLM_INVALID_RESPONSE", True, "Модель вернула ответ неверного формата"
    if isinstance(exc, (ContextBuildError, ValueError)):
        return "CONTEXT_BUILD_FAILED", False, "Не удалось подготовить контекст документации"
    if isinstance(exc, ModelInputTooLarge):
        return "CONTEXT_BUILD_FAILED", False, "Контекст превышает лимит модели"
    if isinstance(exc, ModelContentFiltered):
        return "CONTENT_FILTER", False, "Провайдер отклонил содержимое репозитория"
    if isinstance(exc, ModelRateLimited):
        return "LLM_RATE_LIMITED", True, "Лимит запросов к модели временно исчерпан"
    if isinstance(exc, ModelDeadlineExceeded):
        return "LLM_TIMEOUT", True, "Истекло время обработки запроса моделью"
    if isinstance(exc, ModelProviderError):
        return "LLM_PROVIDER_UNAVAILABLE", True, "Провайдер модели временно недоступен"
    return "INTERNAL_ERROR", True, "Внутренняя ошибка LLM-сервиса"


def documentation_mode() -> str:
    """LLM_DOCS_MODE: auto (default), llm or algorithmic; typos fail at startup."""
    mode = os.environ.get("LLM_DOCS_MODE", "auto").strip().lower()
    if mode not in DOCUMENTATION_MODES:
        raise ValueError("LLM_DOCS_MODE must be auto, llm or algorithmic")
    return mode


def documentation_model(mode: str):
    """Build the model for this mode; auto without credentials runs algorithmically."""
    if mode == "algorithmic":
        return None
    try:
        return YandexModel(YandexModelConfig.from_env())
    except ValueError:
        if mode != "auto":
            raise
        LOG.warning("Yandex credentials are not set; Documentation is graded algorithmically")
        return None


def _keep(payload: dict, llm_used: bool) -> tuple[dict, bool]:
    return payload, llm_used


def grade_documentation(snapshot, context, model, mode: str, finish=_keep):
    """Return finish(payload, llm_used), llm_used telling whether the model graded.

    In auto mode any failure of the model path — the call itself or turning its
    answer into a result via finish — falls back to the algorithmic grade. Failures
    to read the repository happen earlier and are never masked by the fallback.
    """
    if mode == "algorithmic" or model is None:
        return finish(algorithmic_grade(snapshot, context), False)
    try:
        return finish(model.grade(snapshot, context), True)
    except Exception as exc:
        if mode != "auto":
            raise
        LOG.warning("Documentation model path failed (%s: %s); using algorithmic grade",
                    _failure(exc)[0], type(exc).__name__)
        return finish(algorithmic_grade(snapshot, context), False)


def process_documentation_request(raw: bytes, store, model, mode: str = "llm") -> dict | None:
    """Return success or failure; only uncorrelatable input lacks a result.

    The default ``llm`` keeps model failures visible; the worker reads LLM_DOCS_MODE.
    """
    try:
        request = parse_grade_request(raw)
    except InvalidEvent:
        sourcecraft_uuid = request_uuid_if_valid(raw)
        LOG.warning("Invalid Documentation request; sourcecraft_uuid=%s", sourcecraft_uuid or "unknown")
        if sourcecraft_uuid is None:
            return None
        return build_grade_failure(sourcecraft_uuid, "INVALID_EVENT")
    try:
        snapshot, context = prepare_documentation_context(store, request["s3_prefix"])
        return grade_documentation(
            snapshot, context, model, mode,
            lambda payload, llm_used: build_grade_result(request, payload, llm_used=llm_used))
    except Exception as exc:
        code, _, _ = _failure(exc)
        LOG.error("Documentation analysis failed: %s; sourcecraft_uuid=%s", code,
                  request["sourcecraft_uuid"])
        return build_grade_failure(request["sourcecraft_uuid"], code)


def process_summary_request(raw: bytes, model) -> dict | None:
    """Return a summary or a failure; only uncorrelatable input lacks a result."""
    try:
        request = parse_summary_request(raw)
    except InvalidEvent:
        sourcecraft_uuid, visibility = request_correlation_if_valid(raw)
        LOG.warning("Invalid summary request; sourcecraft_uuid=%s", sourcecraft_uuid or "unknown")
        if sourcecraft_uuid is None:
            return None
        return build_summary_failure(sourcecraft_uuid, visibility, "INVALID_EVENT", model.name)
    try:
        return build_summary_result(request, generate_summary(model, request), model.name)
    except Exception as exc:
        code = "NO_DATA" if isinstance(exc, NoGradeData) else _failure(exc)[0]
        LOG.error("Summary generation failed: %s; sourcecraft_uuid=%s", code,
                  request["sourcecraft_uuid"])
        return build_summary_failure(request["sourcecraft_uuid"], request["visibility"], code,
                                     model.name)


def publish_terminal(producer, event: dict, *, topic: str = RESULT_TOPIC,
                     timeout: float = 30.0) -> None:
    """Block until broker acknowledgement; caller may then commit the input offset."""
    error = []
    acknowledged = []

    def delivered(err, _message):
        if err is not None:
            error.append(err)
        else:
            acknowledged.append(True)

    producer.produce(topic, key=event["sourcecraft_uuid"].encode("utf-8"),
                     value=json.dumps(event, ensure_ascii=False, allow_nan=False).encode("utf-8"),
                     on_delivery=delivered)
    if producer.flush(timeout) != 0 or error or not acknowledged:
        raise RuntimeError(f"Kafka did not acknowledge result on {topic}")


_UNKNOWN_TOPIC_OR_PART = 3  # confluent_kafka.KafkaError.UNKNOWN_TOPIC_OR_PART


def _is_unknown_topic(error) -> bool:
    code = getattr(error, "code", None)
    return callable(code) and code() == _UNKNOWN_TOPIC_OR_PART


def _serve(config, request_topic, result_topic, handle, *, consumer=None, producer=None) -> None:
    """Consume one topic; commit each input only after its result is acknowledged."""
    if consumer is None or producer is None:
        from confluent_kafka import Consumer, Producer

        if consumer is None:
            consumer = Consumer({"bootstrap.servers": config.brokers, "group.id": config.group_id,
                                 "enable.auto.commit": False, "auto.offset.reset": "earliest",
                                 "max.poll.interval.ms": 900_000,
                                 "allow.auto.create.topics": True})
        if producer is None:
            producer = Producer({"bootstrap.servers": config.brokers,
                                 "enable.idempotence": True, "acks": "all"})
    try:
        consumer.subscribe([request_topic])
        LOG.info("Listening on %s", request_topic)
        while True:
            message = consumer.poll(1.0)
            if message is None:
                continue
            if message.error():
                if _is_unknown_topic(message.error()):
                    # Topic is auto-created on the producer's first publish; keep polling.
                    LOG.warning("Waiting for topic %s to be created", request_topic)
                    continue
                raise RuntimeError(f"Kafka consumer error: {message.error()}")
            event = handle(message.value())
            if event is not None:
                publish_terminal(producer, event, topic=result_topic)
            else:
                LOG.error("Uncorrelatable request on %s skipped", request_topic)
            committed = consumer.commit(message=message, asynchronous=False)
            if any(partition.error for partition in committed):
                raise RuntimeError("Kafka did not commit input offset")
    finally:
        consumer.close()


def run_worker(config: KafkaConfig, store, model, *, mode: str = "llm", consumer=None,
               producer=None) -> None:
    _serve(config, config.request_topic, config.result_topic,
           lambda raw: process_documentation_request(raw, store, model, mode),
           consumer=consumer, producer=producer)


def run_summary_worker(config: SummaryKafkaConfig, model, *, consumer=None,
                       producer=None) -> None:
    _serve(config, config.request_topic, config.result_topic,
           lambda raw: process_summary_request(raw, model),
           consumer=consumer, producer=producer)


def main() -> None:
    logging.basicConfig(level=logging.INFO)
    mode = documentation_mode()
    store = S3Store(S3Config.from_env())
    model = documentation_model(mode)
    LOG.info("Documentation mode: %s", mode)
    try:
        run_worker(KafkaConfig.from_env(), store, model, mode=mode)
    except KeyboardInterrupt:
        pass


def summary_main() -> None:
    logging.basicConfig(level=logging.INFO)
    model = YandexModel(YandexModelConfig.from_env())
    try:
        run_summary_worker(SummaryKafkaConfig.from_env(), model)
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
