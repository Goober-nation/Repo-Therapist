"""Grade-service flat Kafka request format."""
import json
import re
from datetime import datetime, timezone
from decimal import Decimal, ROUND_HALF_UP

from .contracts import InvalidEvent, _unique_pairs, _reject_constant, _uuid, validate_documentation_payload

REQUEST_TOPIC = 'repo.docs.grade.requested'
RESULT_TOPIC = 'repo.docs.graded'


def parse_grade_request(raw):
    if not isinstance(raw, bytes) or len(raw) > 524_288:
        raise InvalidEvent('request must be UTF-8 JSON within 512 KiB')
    try:
        request = json.loads(raw.decode('utf-8'), object_pairs_hook=_unique_pairs,
                             parse_constant=_reject_constant)
    except (UnicodeDecodeError, json.JSONDecodeError, RecursionError) as exc:
        raise InvalidEvent('invalid request JSON') from exc
    if not isinstance(request, dict) or set(request) != {'sourcecraft_uuid', 's3_prefix', 'requested_at'}:
        raise InvalidEvent('expected sourcecraft_uuid, s3_prefix, requested_at')
    _uuid(request['sourcecraft_uuid'])
    prefix = request['s3_prefix']
    if (not isinstance(prefix, str) or not prefix or '\\' in prefix or
            any(p in {'', '.', '..'} for p in prefix.split('/'))):
        raise InvalidEvent('invalid repository S3 prefix')
    timestamp = request['requested_at']
    if not isinstance(timestamp, str) or not re.fullmatch(
            r'\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})', timestamp):
        raise InvalidEvent('requested_at must be RFC3339')
    try:
        parsed = datetime.fromisoformat(timestamp.replace('Z', '+00:00'))
        if parsed.tzinfo is None:
            raise ValueError('timezone required')
    except ValueError as exc:
        raise InvalidEvent('requested_at must include a timezone') from exc
    return request


GROUP_NAMES = {
    "required_files": "Обязательные файлы", "purpose": "Назначение проекта",
    "local_run": "Локальный запуск", "build": "Сборка", "testing": "Тестирование",
    "usage": "Использование", "structure": "Структура проекта",
    "consistency": "Согласованность документации",
}


FACT_NAMES = {
    "DOC_PURPOSE_MISSING": "Назначение проекта не описано",
    "DOC_PURPOSE_UNCLEAR": "Назначение проекта описано неясно",
    "DOC_CAPABILITIES_MISSING": "Возможности проекта не описаны",
    "DOC_CAPABILITIES_INCOMPLETE": "Возможности проекта описаны неполно",
    "DOC_LOCAL_RUN_MISSING": "Инструкции локального запуска отсутствуют",
    "DOC_LOCAL_RUN_PREREQUISITES_INCOMPLETE": "Требования для запуска описаны неполно",
    "DOC_LOCAL_RUN_DEPENDENCIES_INCOMPLETE": "Установка зависимостей описана неполно",
    "DOC_LOCAL_RUN_CONFIGURATION_INCOMPLETE": "Настройка для запуска описана неполно",
    "DOC_LOCAL_RUN_COMMANDS_INCOMPLETE": "Команды запуска описаны неполно",
    "DOC_LOCAL_RUN_VERIFICATION_INCOMPLETE": "Проверка успешного запуска описана неполно",
    "DOC_BUILD_INSTRUCTIONS_MISSING": "Инструкции сборки отсутствуют",
    "DOC_BUILD_INSTRUCTIONS_INCOMPLETE": "Инструкции сборки неполные",
    "DOC_BUILD_CONFIGURATION_INCOMPLETE": "Настройка сборки описана неполно",
    "DOC_BUILD_OUTPUT_UNCLEAR": "Результат сборки описан неясно",
    "DOC_TESTING_INSTRUCTIONS_MISSING": "Инструкции тестирования отсутствуют",
    "DOC_TESTING_COMMANDS_INCOMPLETE": "Команды тестирования описаны неполно",
    "DOC_TESTING_ENVIRONMENT_INCOMPLETE": "Тестовое окружение описано неполно",
    "DOC_TESTING_SCOPE_UNCLEAR": "Область тестирования описана неясно",
    "DOC_USAGE_EXAMPLE_MISSING": "Пример использования отсутствует",
    "DOC_USAGE_EXAMPLE_INCOMPLETE": "Пример использования неполный",
    "DOC_USAGE_INPUTS_UNCLEAR": "Входные данные описаны неясно",
    "DOC_USAGE_RESULT_UNCLEAR": "Ожидаемый результат описан неясно",
    "DOC_STRUCTURE_OVERVIEW_MISSING": "Описание структуры отсутствует",
    "DOC_STRUCTURE_OVERVIEW_INCOMPLETE": "Структура описана неполно",
    "DOC_NAVIGATION_INCOMPLETE": "Навигация по документации неполная",
    "DOC_REFERENCE_NOT_FOUND": "Объект, указанный в документации, не найден",
    "DOC_LINK_BROKEN": "Обнаружена некорректная ссылка",
    "DOC_COMMAND_INCONSISTENT": "Команда в документации не согласована с проектом",
    "DOC_SCRIPT_NOT_FOUND": "Указанный скрипт не найден",
    "DOC_CONFIGURATION_INCONSISTENT": "Описание конфигурации не согласовано с проектом",
    "DOC_COMPONENT_DESCRIPTION_INCONSISTENT": "Описание компонента не согласовано с проектом",
}
for _name in ("README", "LICENSE", "CONTRIBUTING", "CODEOWNERS"):
    FACT_NAMES[f"DOC_{_name}_MISSING"] = f"Файл {_name} не найден"
    FACT_NAMES[f"DOC_{_name}_EMPTY"] = f"Файл {_name} не содержит содержательного описания"


def _fact_text(fact):
    # Deterministic rendering preserves details without another paid model call.
    title = FACT_NAMES.get(fact["code"], fact["code"])
    details = "; ".join(f"{key}: {json.dumps(value, ensure_ascii=False, sort_keys=True)}"
                        for key, value in sorted(fact["facts"].items()))
    sources = json.dumps(fact["evidence"], ensure_ascii=False, sort_keys=True)
    return f'{GROUP_NAMES[fact["subcategory"]]}: {title}. {details}. Источники: {sources}'


def build_grade_result(request, payload):
    validate_documentation_payload(payload)
    def integer(value):
        return int(Decimal(str(value)).quantize(Decimal("1"), rounding=ROUND_HALF_UP))
    return {
        "sourcecraft_uuid": request["sourcecraft_uuid"],
        "grade": integer(payload["grade"]),
        "subgrades": {item["id"]: integer(item["grade"])
                      for item in payload["subcategories"] if item["grade"] is not None},
        "advice": [_fact_text(fact) for fact in payload["facts"]],
        "evaluated_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
    }
