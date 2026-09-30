# Реестр вычетов документации (`documentation-grading-v1`)

> Внутренний результат модели содержит только `grade`, восемь `subcategories` и плоский список
> `facts`; по Kafka передаётся `repo.docs.graded`. Внутренние проверки/компоненты и числовые вычеты не
> передаются. Активный путь модели проверяет только формат; строгие правила доказательств и
> влияния ниже описывают устаревший валидатор и рекомендации по подсчёту, а не требования к
> приёму текущего ответа Yandex. См. [контракт Kafka](kafka-contracts.md) и
> [вывод активной модели](llm-model-output.md).
>
> **Примечание (проверено по коду, 2026-09-28).** По Kafka сейчас реально передаётся
> `repo.docs.graded` с `{sourcecraft_uuid, grade, subgrades, advice, evaluated_at}`, где `advice`
> — рекомендации на русском, построенные по этим кодам (`recommendations.py`). Все коды проверок
> из таблиц ниже присутствуют в коде LLM-сервиса; **групповые коды из последнего раздела в коде
> не реализованы**.

Реестр определяет машиночитаемые вычеты, которые выдаёт LLM-сервис. Ответ оценивания не содержит
заголовков, пояснений или рекомендаций для пользователя. Неизвестные коды, поля фактов, значения
перечислений или варианты доказательств недопустимы.

## Общая форма вычета

```json
{
  "deduction_id": "local_run.configuration:required-parameters",
  "code": "DOC_LOCAL_RUN_CONFIGURATION_INCOMPLETE",
  "raw_points_lost": 2,
  "facts": {
    "missing_aspects": ["required_parameters"]
  },
  "evidence": []
}
```

- `deduction_id` стабилен и уникален в пределах результата;
- `raw_points_lost` — положительное целое;
- `facts` следует зарегистрированной схеме и отвергает дополнительные свойства;
- `missing_aspects` и `unclear_aspects` содержат только ID компонентов, зарегистрированные для
  исходной проверки в `docs/documentation-grading.md`;
- факты детекторов (`detected_*`) получаются обычным кодом, а не свободным выводом модели. Ответ
  модели их не содержит; сервис вставляет проверенные значения после разбора ответа;
- требуется хотя бы одно проверенное доказательство.

## Варианты доказательств

| Тип | Обязательные поля | Применение |
|---|---|---|
| `file_content` | `path`, `start_line`, `end_line`, `quote` | Точный подтверждающий или противоречащий текст |
| `file_exists` | `path` | Наличие артефакта в репозитории |
| `file_missing` | `path` | Отсутствие, доказанное по полному манифесту |
| `document_review` | `paths`, `context_sha256` | Отсутствие смыслового содержания в полностью просмотренных документах |

`document_review.paths` не может быть пустым. Каждый путь должен быть в манифесте и полностью
входить в контекст, идентифицируемый `context_sha256`. Это обязательное доказательство для
утверждений вроде «назначение не описано»; `file_missing` не может доказать отсутствие раздела в
существующем документе.

Если в полном списке файлов репозитория нет выбранной пользовательской документации, код, факты
которого требуют `reviewed_paths`, может вместо этого указать `reviewed_paths: []` и использовать
`file_missing` для `README.md`. Исключение не действует, если какая-либо пользовательская
документация полностью вошла в контекст модели; тогда требуется `document_review`. Несуществующий
файл никогда не помечается как просмотренный.

## Проверки файлов

| Код | Допустимая проверка | Обязательные факты | Доказательство | Минимальная важность |
|---|---|---|---|---|
| `DOC_README_MISSING` | `readme.exists` | `searched_paths` | `file_missing` | `HIGH` |
| `DOC_README_EMPTY` | `readme.exists` | `path`, `empty_reason` | `file_content` или `file_exists` | `HIGH` |
| `DOC_LICENSE_MISSING` | `license.exists` | `searched_paths` | `file_missing` | `HIGH` |
| `DOC_LICENSE_EMPTY` | `license.exists` | `path`, `empty_reason` | `file_content` или `file_exists` | `HIGH` |
| `DOC_CONTRIBUTING_MISSING` | `contributing.exists` | `searched_paths` | `file_missing` | `LOW` |
| `DOC_CONTRIBUTING_EMPTY` | `contributing.exists` | `path`, `empty_reason` | `file_content` или `file_exists` | `LOW` |
| `DOC_CODEOWNERS_MISSING` | `codeowners.exists` | `searched_paths` | `file_missing` | `LOW` |
| `DOC_CODEOWNERS_EMPTY` | `codeowners.exists` | `path`, `empty_reason` | `file_content` или `file_exists` | `LOW` |

`empty_reason` — одно из `EMPTY`, `WHITESPACE_ONLY`, `COMMENTS_ONLY` или `TITLE_ONLY`.

## Назначение и возможности

| Код | Допустимая проверка | Обязательные факты | Доказательство | Минимальная важность |
|---|---|---|---|---|
| `DOC_PURPOSE_MISSING` | `purpose.summary` | `missing_aspects`, `reviewed_paths` | `document_review` | `LOW` |
| `DOC_PURPOSE_UNCLEAR` | `purpose.summary` | `unclear_aspects` | `file_content`; необязательно `document_review` | `LOW` |
| `DOC_CAPABILITIES_MISSING` | `purpose.capabilities` | `coverage_level=NONE`, `documented_capability_count=0`, `reviewed_paths` | `document_review` | `LOW` |
| `DOC_CAPABILITIES_INCOMPLETE` | `purpose.capabilities` | `coverage_level`, `documented_capability_count` | `file_content`, `document_review` | `LOW` |

Неполный `coverage_level` — одно из `MINIMAL`, `PARTIAL` или `MOST`.

## Локальная настройка и запуск

| Код | Допустимые проверки | Обязательные факты | Доказательство | Минимальная важность |
|---|---|---|---|---|
| `DOC_LOCAL_RUN_MISSING` | любая `local_run.*` | `missing_checks`, `reviewed_paths` | `document_review` | `LOW` |
| `DOC_LOCAL_RUN_PREREQUISITES_INCOMPLETE` | `local_run.prerequisites` | `missing_aspects`, необязательно `detected_requirements` | документация плюс доказательство детектора | `LOW` |
| `DOC_LOCAL_RUN_DEPENDENCIES_INCOMPLETE` | `local_run.dependencies` | `missing_aspects`, необязательно `detected_package_managers` | документация плюс доказательство из манифеста | `LOW` |
| `DOC_LOCAL_RUN_CONFIGURATION_INCOMPLETE` | `local_run.configuration` | `missing_aspects`, необязательно `detected_configuration` | документация плюс доказательство конфигурации | `LOW` |
| `DOC_LOCAL_RUN_COMMANDS_INCOMPLETE` | `local_run.execution` | `missing_aspects`, `documented_command_count`, необязательно `detected_entrypoints` | документация плюс доказательство точки входа | `LOW` |
| `DOC_LOCAL_RUN_VERIFICATION_INCOMPLETE` | `local_run.verification` | `missing_aspects`, `documented_verification_count` | `file_content` или `document_review` | `LOW` |

`missing_checks` использует `prerequisites`, `dependencies`, `configuration`, `execution` и
`verification`. Записи детекторов содержат контролируемый `kind`, стабильный идентификатор и путь
к источнику. Значения секретов никогда не включаются; имена переменных окружения допустимы.

## Сборка

| Код | Допустимая проверка | Обязательные факты | Доказательство | Минимальная важность |
|---|---|---|---|---|
| `DOC_BUILD_INSTRUCTIONS_MISSING` | `build.commands` | `detected_build_systems`, `reviewed_paths` | артефакт сборки плюс `document_review` | `LOW` |
| `DOC_BUILD_INSTRUCTIONS_INCOMPLETE` | `build.commands` | `missing_aspects`, `documented_command_count`, `detected_build_systems` | документация плюс доказательство сборки | `LOW` |
| `DOC_BUILD_CONFIGURATION_INCOMPLETE` | `build.configuration` | `missing_aspects`, необязательно `detected_configuration` | документация плюс доказательство сборки | `LOW` |
| `DOC_BUILD_OUTPUT_UNCLEAR` | `build.output` | `missing_aspects`, необязательно `detected_artifacts` | документация плюс доказательство сборки | `LOW` |

Виды артефактов: `binary`, `container_image`, `package`, `archive`, `frontend_bundle`,
`generated_documentation` или `other`.

## Тестирование

| Код | Допустимая проверка | Обязательные факты | Доказательство | Минимальная важность |
|---|---|---|---|---|
| `DOC_TESTING_INSTRUCTIONS_MISSING` | `testing.commands` | `detected_test_types`, `detected_test_commands`, `reviewed_paths` | тестовый артефакт плюс `document_review` | `LOW` |
| `DOC_TESTING_COMMANDS_INCOMPLETE` | `testing.commands` | `missing_aspects`, `documented_command_count`, `detected_test_types` | документация плюс доказательство тестов | `LOW` |
| `DOC_TESTING_ENVIRONMENT_INCOMPLETE` | `testing.environment` | `missing_aspects`, необязательно `detected_requirements` | документация плюс конфигурация тестов | `LOW` |
| `DOC_TESTING_SCOPE_UNCLEAR` | `testing.scope` | `missing_aspects`, `detected_test_types` | документация плюс доказательство тестов | `LOW` |

Типы тестов: `unit`, `integration`, `end_to_end`, `contract`, `smoke`, `performance` или `other`.
`detected_test_commands` может быть пустым списком, если тестовые файлы есть, но явная команда не
найдена в выбранных манифестах или конфигурации CI. Пустой список не означает, что команды нет
нигде в репозитории.

## Использование

| Код | Допустимая проверка | Обязательные факты | Доказательство | Минимальная важность |
|---|---|---|---|---|
| `DOC_USAGE_EXAMPLE_MISSING` | `usage.primary_example` | `detected_interfaces`, `reviewed_paths` | доказательство интерфейса плюс `document_review` | `LOW` |
| `DOC_USAGE_EXAMPLE_INCOMPLETE` | `usage.primary_example` | `missing_aspects`, `example_type` | `file_content` | `LOW` |
| `DOC_USAGE_INPUTS_UNCLEAR` | `usage.inputs` | `missing_aspects`, необязательно `detected_required_input_count` | документация плюс доказательство интерфейса | `LOW` |
| `DOC_USAGE_RESULT_UNCLEAR` | `usage.expected_result` | `missing_aspects`, необязательно `detected_result_kind` | документация плюс доказательство интерфейса | `LOW` |

Виды интерфейсов и примеров — закрытые перечисления, охватывающие HTTP API, библиотеки, CLI,
пользовательские интерфейсы, инфраструктуру, шаблоны, документацию и `other`. Значения:
`http_api`, `library`, `cli`, `user_interface`, `infrastructure`, `template`, `documentation` и
`other`.

## Структура и навигация

| Код | Допустимая проверка | Обязательные факты | Доказательство | Минимальная важность |
|---|---|---|---|---|
| `DOC_STRUCTURE_OVERVIEW_MISSING` | `structure.overview` | `detected_components`, `reviewed_paths` | доказательство компонентов плюс `document_review` | `LOW` |
| `DOC_STRUCTURE_OVERVIEW_INCOMPLETE` | `structure.overview` | `missing_aspects`, `documented_component_count`, `detected_component_count` | документация плюс доказательство компонентов | `LOW` |
| `DOC_NAVIGATION_INCOMPLETE` | `structure.navigation` | `missing_aspects`, необязательно `unlinked_documents` | документация и доказательство файлов | `LOW` |

## Согласованность

| Код | Допустимая проверка | Обязательные факты | Доказательство | Минимальная важность |
|---|---|---|---|---|
| `DOC_REFERENCE_NOT_FOUND` | `consistency.references` | `reference_kind`, `source_path`, `referenced_target` | `file_content`, `file_missing` | `LOW` |
| `DOC_LINK_BROKEN` | `consistency.references` | `link_kind`, `source_path`, `target`, необязательно `anchor` | доказательства источника и цели | `LOW` |
| `DOC_COMMAND_INCONSISTENT` | `consistency.commands` | `documented_command`, `mismatch_kind` | документация плюс доказательство из манифеста | `LOW` |
| `DOC_SCRIPT_NOT_FOUND` | `consistency.commands` | `documented_command`, `missing_script`, `manifest_path` | документация и полный раздел scripts | `LOW` |
| `DOC_CONFIGURATION_INCONSISTENT` | `consistency.commands` | `configuration_kind`, `documented_identifier`, `detected_identifiers` | документация плюс доказательство конфигурации | `LOW` |
| `DOC_COMPONENT_DESCRIPTION_INCONSISTENT` | `consistency.description` | `claim_kind`, `documented_identifier`, `mismatch_kind` | утверждение плюс доказательство из репозитория | `LOW` |

Внешние HTTP-ссылки в v1 не проверяются; доступность сети не должна влиять на оценку репозитория.

Остальные перечисления фактов v1 закрыты: `reference_kind` — `file`, `directory`, `script`,
`configuration` или `other`; `link_kind` — `file`, `directory` или `anchor`; `mismatch_kind` —
`missing`, `wrong_target`, `wrong_parameter`, `wrong_value` или `other`; `configuration_kind` —
`environment_variable`, `config_file`, `flag` или `other`; `claim_kind` — `component`,
`capability` или `technology`; `detected_result_kind` — `text`, `json`, `file`, `http_response`,
`user_interface` или `other`.

## Групповые коды проблем

> **Не реализовано** (проверено по коду 2026-09-28): ни один из кодов ниже не встречается в
> `backend/llm-service/src`. Раздел описывает задуманное поведение.

Код сервиса может заменить несколько связанных кодов вычетов одним кодом проблемы, сохранив все
исходные ID вычетов и их доказательства:

- `DOC_DOCUMENTATION_MISSING`;
- `DOC_LOCAL_RUN_INCOMPLETE`;
- `DOC_BUILD_INCOMPLETE`;
- `DOC_TESTING_INCOMPLETE`;
- `DOC_USAGE_INCOMPLETE`;
- `DOC_STRUCTURE_INCOMPLETE`;
- `DOC_DOCUMENTATION_OUTDATED`.

Групповые коды никогда не создают дополнительной потери баллов. Их `category_points_lost` —
нормированная сумма исходных вычетов. `DOC_DOCUMENTATION_OUTDATED` использует `mismatch_count` и
`affected_areas`; остальные групповые коды — `affected_checks` и объединение зарегистрированных
`missing_aspects`.
