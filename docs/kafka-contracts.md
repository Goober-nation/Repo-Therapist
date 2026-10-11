# Контракты сообщений Kafka

Область: формы сообщений, публикуемых и потребляемых на связях Kafka из схемы архитектуры:

- Aggregator ↔ Grade service — топики `grading.requests` / `grading.results`, **описаны ниже**.
- Grade service ↔ Repository service — **описано ниже**. Этот обмен целиком в зоне одного
  человека (Repository + Grade service), поэтому не требовал общего согласования команды.
- Grade service ↔ LLM service — **описано ниже как плоские сообщения `repo.docs.*`** для анализа
  документации.
- Aggregator ↔ LLM service — **описано ниже как плоские сообщения `llm.recommendations.*`** для
  сводки по репозиторию и приоритизированных рекомендаций.
- Aggregator → все сервисы — настройки из панели администратора: топики `config.updated` /
  `config.status`, **описаны ниже** и в [config.md](config.md).
- Grade service ↔ Security wrapper — не через Kafka: Grade service вызывает обёртку напрямую по
  gRPC (`GetAppsecResult`), см. `grading/security.md`.

## Переменные окружения топиков

Каждое имя топика ниже — значение по умолчанию. Все сервисы читают одну и ту же переменную для
его переопределения, так что одно значение переименовывает топик везде; если ничего не задавать,
остаются документированные имена.

| Топик (по умолчанию) | Переменная | Какие сервисы читают |
|---|---|---|
| `grading.requests` | `KAFKA_GRADING_REQUESTS_TOPIC` | aggregator, grade |
| `grading.results` | `KAFKA_GRADING_RESULTS_TOPIC` | aggregator, grade |
| `repo.grade.requested` | `KAFKA_REPO_GRADE_REQUESTED_TOPIC` | grade, repository |
| `repo.ready` | `KAFKA_REPO_READY_TOPIC` | grade, repository |
| `repo.docs.grade.requested` | `KAFKA_DOCS_GRADE_REQUESTED_TOPIC` | grade, llm |
| `repo.docs.graded` | `KAFKA_DOCS_GRADED_TOPIC` | grade, llm |
| `llm.recommendations.requested` | `KAFKA_LLM_REQUESTS_TOPIC` | aggregator, llm |
| `llm.recommendations.received` | `KAFKA_LLM_RECOMMENDATIONS_TOPIC` | aggregator, llm |

LLM-сервис по-прежнему принимает старые `LLM_SUMMARY_REQUEST_TOPIC`/`LLM_SUMMARY_RESULT_TOPIC`
как запасные; если заданы обе, побеждает переменная `KAFKA_*`. Старое имя, заданное в окружении,
главнее нового имени в `config/app.yaml`.

Имена топиков можно задать и в `config/app.yaml` (`shared.static.kafka_*_topic`); они читаются только
при запуске. Топики настроек `config.updated` / `config.status` не переименовываются.

## Настройки: `config.updated` и `config.status`

Оба топика — с одной партицией и `cleanup.policy=compact` (Kafka хранит последнее сообщение по каждому
ключу). Их создаёт с этими параметрами первый запустившийся сервис; уже существующим топикам политика
выставляется заново.

**`config.updated`** — пишет только Aggregator, читают все сервисы с настройками из панели (aggregator,
gateway, grade, repository, llm-service, llm-summary-service). Ключ сообщения — путь в app.yaml
(`gateway.rate_limit`), значение:

```json
{"key": "gateway.rate_limit", "value": "90", "updated_at": "2026-10-11T12:00:00Z"}
```

Сброс к значению из файла — сообщение с тем же ключом и пустым значением (tombstone). Читатели не
используют группу потребителей: каждый процесс читает топик целиком с первого смещения, при запуске —
до конца (не дольше 15 секунд), затем следит за новыми сообщениями. Значение, не подходящее по типу и
границам `admin_editable`, и ключ, заданный в окружении процесса, процесс не применяет.

**`config.status`** — пишет каждый процесс при запуске и после каждого изменения своих настроек, читает
Aggregator. Ключ — имя процесса (`gateway`, `grade`, `llm-service`, ...), значение:

```json
{"service": "gateway", "sections": ["shared", "gateway", "frontend"],
 "env_locked": ["gateway.rate_limit"], "restart_pending": [], "reported_at": "2026-10-11T12:00:00Z"}
```

`env_locked` — ключи, заданные в окружении процесса (без значений); Aggregator отклоняет изменение
такого ключа. `restart_pending` — `static:`-ключи, изменённые в файле после запуска.

## `repo.grade.requested`

Производитель: Grade service, ретранслирующий каждое сообщение `grading.requests` от Aggregator
(или от ручной команды `grading-request`). Потребитель: Repository service.

```json
{
  "job_id": "0f6c2b8e-3d4a-4b7e-9c1d-5a2e8f9b7c31",
  "org_slug": "examples",
  "repo_slug": "vulnerable-repo",
  "requested_at": "2026-09-19T23:49:12+03:00"
}
```

**Устаревшие запросы.** Grade service (на `grading.requests`) и Repository service (на
`repo.grade.requested`) пропускают запрос, если `requested_at` старше `GRADE_REQUEST_MAX_AGE`
(по умолчанию 20m, как `GRADING_LOCK_TIMEOUT`; `0` выключает проверку). Ответ на пропущенный запрос
не отправляется. Так после сбоя сервера не обрабатываются запросы, оставшиеся незафиксированными в
Kafka: Aggregator к этому времени уже перестал их ждать и освободил блокировку репозитория, а
поздний ответ освободил бы блокировку нового задания. Запрос без `requested_at` не пропускается.

Собственные топики Aggregator в том виде, как их читает/пишет Grade service:

- `grading.requests`: `{job_id, org_slug, repo_slug, visibility, requested_at}`. `job_id` —
  UUID задания оценки, который Aggregator создаёт на каждый запрос. `visibility` — видимость
  репозитория в SourceCraft (`public` | `internal` | `private`). Токен пользователя в сообщении
  нет: он лежит в Redis (см. «Токен пользователя в Redis» ниже). Токен в Redis для `job_id`
  означает приватное оценивание; без токена — публичное. Непубличный `visibility` без токена
  отбрасывается; сообщение без `job_id` тоже отбрасывается.

  ```json
  {
    "job_id": "0f6c2b8e-3d4a-4b7e-9c1d-5a2e8f9b7c31",
    "org_slug": "examples",
    "repo_slug": "vulnerable-repo",
    "visibility": "private",
    "requested_at": "2026-09-19T23:49:12+03:00"
  }
  ```

- `grading.results`: `{sourcecraft_uuid, grade_mode, score, category_scores, advice,
  missing_data, graded_at, confidence, analytics, error}`. `grade_mode` — режим оценки, а не видимость репозитория:
  `private`, если в запросе был токен пользователя, иначе `public` (публичный репозиторий,
  оценённый владельцем, приходит как `private`). Других значений нет; по нему Aggregator выбирает
  таблицу `repo_grades_public` или `repo_grades_private`.
  `advice` — все рекомендации `{category_id, metric_id, priority, text}` по порядку категорий.
  `priority` — `critical` | `high` | `medium` | `low`; `text` — без метки приоритета. Для документации это элементы `advice` из
  `repo.docs.graded`: `metric_id` — их `code`, `priority` — их `priority` (без кода —
  `llm_advice`, с неизвестным приоритетом — `medium`). `error` при успехе — `""`.
  При сбое это код — `REPO_FETCH_FAILED` (repo.ready со статусом failed), `GRADING_FAILED`,
  `GRADING_INTERRUPTED` (grade-service остановлен посреди оценки), `RESULT_BUILD_FAILED` — а
  `score`, `category_scores` и `advice` равны null; Aggregator тогда снимает блокировку
  репозитория и один раз сообщает о сбое следующему запросу оценки этого репозитория. Сбой без
  `sourcecraft_uuid` нельзя сопоставить, и он не публикуется; такую блокировку Aggregator
  снимает сам, когда она занята дольше `GRADING_LOCK_TIMEOUT` (по умолчанию 20m).

`job_id` копируется из `grading.requests`. Repository service читает по нему токен из Redis:
если он есть, использует его для вызовов обёртки и git clone; если нет — свой `SOURCECRAFT_PAT`.
Ошибка Redis — сбой задания (`repo.ready` со статусом `failed`), без перехода на сервисный PAT.
Токен не передаётся ни в одном сообщении Kafka и никогда не логируется.

### Токен пользователя в Redis

- Ключ: `grading:token:<job_id>`, значение — PAT SourceCraft пользователя. Пишется только для
  приватной оценки; для публичной ключа нет.
- Пишет Aggregator перед публикацией `grading.requests`, с TTL, равным `GRADING_LOCK_TIMEOUT`
  (по умолчанию 20m): дольше блокировки репозитория задание не живёт. Если публикация не удалась,
  Aggregator сразу удаляет ключ.
- Читают Grade service (при `grading.requests` — чтобы определить режим, и при `repo.ready` — для
  вызова Security wrapper) и Repository service (для клонирования).
- Удаляет Grade service сразу после вызова Security wrapper, а если до него не дошло (сбой
  клонирования, истёкший токен, ошибка) — в конце обработки `repo.ready`. Иначе ключ истекает по TTL.
- Режим определяется как раньше: токен есть → `private`. Если Grade запомнил задание как
  приватное, а токена к `repo.ready` уже нет (истёк), оценка не выполняется: в `grading.results`
  уходит `GRADING_FAILED`. Неизвестное Grade задание (перезапуск сервиса) оценивается по наличию
  токена в Redis.
- Redis защищён паролем (`REDIS_PASSWORD`, `requirepass`); адрес и пароль сервисы читают из
  `REDIS_ADDRESS`/`REDIS_PASSWORD`/`REDIS_DB`, как gateway.

## `repo.ready`

Производитель: Repository service, после завершения (успешного или нет) обработки сообщения
`repo.grade.requested`. Потребитель: Grade service.

```json
{
  "job_id": "0f6c2b8e-3d4a-4b7e-9c1d-5a2e8f9b7c31",
  "org_slug": "examples",
  "repo_slug": "vulnerable-repo",
  "sourcecraft_uuid": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "s3_prefix": "repos/019b2102-fa21-7ab7-8ce1-116af406fb5e/0f6c2b8e-3d4a-4b7e-9c1d-5a2e8f9b7c31",
  "status": "ready"
}
```

`job_id` повторяет значение из `repo.grade.requested`; по нему Grade service находит своё задание
(не по `org/repo`), поэтому перекрывающиеся прогоны одного репозитория не смешиваются.
`s3_prefix` — выгрузка именно этого задания, `repos/<uuid>/<job_id>`: Repository service перед
загрузкой очищает только этот префикс, а Grade service после оценки удаляет только его.
`status` — `"ready"` или `"failed"`; при сбое `error` содержит сообщение, а `s3_prefix`
опускается. `sourcecraft_uuid` передаётся и при сбое, если репозиторий успел определиться;
тогда Grade service публикует `REPO_FETCH_FAILED` в `grading.results`, и Aggregator сразу
снимает блокировку. Без `sourcecraft_uuid` сбой только логируется, блокировка снимается по
`GRADING_LOCK_TIMEOUT`.

`confidence` — флаг низкой достоверности **публичной** оценки:
`{"low_confidence": bool, "reasons": [{"code", "text"}]}`. Для `grade_mode: private`, при сбое и
если флаг выключен в `config/grading.yaml`, поле равно `null`. `reasons` — заготовленные общие
тексты на русском по кодам `LOW_CODE`, `LOW_HISTORY`, `LOW_COVERAGE` (без порогов и чисел), пустые
при `low_confidence: false`. Флаг не меняет `score`. Aggregator пишет его в `repo_confidence_public`
сразу после публичной оценки; `null` при публичной оценке удаляет старую запись, чтобы устаревший
флаг не пережил отключение. Отсутствие записи значит «неизвестно» и никогда не «низкая
достоверность». Методика: `docs/grading/00-overview.md`.

`analytics` — справочные показатели (фактор автобуса, карта владельцев кода, ревью
pull request) для любого `grade_mode`, объект версии 4; `null` при сбое и если аналитика выключена в
`config/grading.yaml`. Не влияет на `score`, за исключением времени первого ответа на задачи, которое входит в категорию «Задачи» отдельной метрикой (см. `docs/grading/issues.md`). Aggregator хранит его без разбора в
`repo_analytics_public`/`repo_analytics_private` (по `grade_mode`) и удаляет старую запись, если
пришёл `null`; сбой записи только логируется и оценку не отменяет. Формат и методика:
`docs/grading/analytics.md`.

`facts` — наблюдения о репозитории для фильтров публичного рейтинга: `{files, loc,
has_ci, has_security_policy}`, члены `null`, если их не удалось установить (это «неизвестно», а не
«нет»). Отправляется только с публичной оценкой, для приватной — `null`. Не влияет на `score`.
Aggregator сохраняет объект в `repo_facts_public` и удаляет старую строку, если пришёл `null` или
нечитаемый объект; сбой записи только логируется. Методика: `docs/ranking.md`.

Пример успешного `grading.results` (сокращён):

```json
{
  "sourcecraft_uuid": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "grade_mode": "private",
  "score": 64,
  "category_scores": {
    "documentation": {"id": "documentation", "no_data": false, "score": 52,
                      "metrics": [{"id": "purpose", "no_data": false, "raw_value": 80, "score": 0.8, "weight": 0.5}],
                      "method": "algorithmic"}
  },
  "advice": [
    {"category_id": "ci_cd", "metric_id": "has_ci", "priority": "critical",
     "text": "CI-пайплайн не найден. …"},
    {"category_id": "documentation", "metric_id": "DOC_README_MISSING", "priority": "high",
     "text": "Добавьте в README описание проекта и ссылки на основные инструкции."}
  ],
  "missing_data": [],
  "graded_at": "2026-09-27T11:59:00.123456Z",
  "confidence": null,
  "analytics": null,
  "error": ""
}
```

`method` есть только у объекта категории `documentation` и только когда документация оценена
(`"llm"` или `"algorithmic"`, см. `repo.docs.graded`); у остальных категорий, при отсутствии данных
и при неизвестном значении поле опускается.

Go-типы: `internal/contracts/repo_grading.go`. Проверено сквозным прогоном: ручной
запуск → Repository service → `repo.ready` → Grade service → напечатанная оценка «Активности»,
совпавшая с результатом, ранее независимо проверенным вручную.

---

# Контракты LLM-сервиса

Два процесса на плоском JSON, без конверта и полей версии. Все топики используют
`sourcecraft_uuid` как ключ Kafka; производители используют партиционер с учётом ключа. Kafka
гарантирует порядок только внутри одной партиции топика, поэтому ни один процесс не полагается на
порядок между топиками.

1. Grade service отправляет S3-префикс репозитория и получает итоговую оценку документации,
   оценки разделов и рекомендации на русском. Оценкой документации владеет LLM-сервис; Grade
   service берёт её без изменений и владеет межкатегорийным Repo Health Score. Методика:
   `docs/documentation-grading.md`.
2. Aggregator отправляет итоговые оценки и получает сводку на русском с приоритизированными
   рекомендациями. LLM-сервис никогда не меняет оценки.

Содержимое репозитория, учётные данные, токены и промпты в эти сообщения никогда не попадают.

| Топик | Производитель | Потребитель | Группа потребителей |
|---|---|---|---|
| `repo.docs.grade.requested` | Grade service | LLM service | `llm-service-documentation-v1` (`LLM_KAFKA_GROUP_ID`) |
| `repo.docs.graded` | LLM service | Grade service | своя у Grade service |
| `llm.recommendations.requested` | Aggregator | LLM service | `llm-service-recommendations-v1` (`LLM_SUMMARY_KAFKA_GROUP_ID`) |
| `llm.recommendations.received` | LLM service | Aggregator | своя у Aggregator |

Эталонные JSON-примеры всех активных сообщений LLM лежат в
`backend/llm-service/tests/fixtures/contracts/` и проверяются
`tests/test_contract_fixtures.py`; примеры ниже — иллюстративные копии.

## Семантика доставки (оба процесса)

Доставка «хотя бы один раз». Обработчик LLM фиксирует смещение входа только после того, как Kafka
подтвердит результат успеха или сбоя, поэтому падение между публикацией и фиксацией может
повторить результат для того же `sourcecraft_uuid`; потребители должны переносить дубликаты. Если
не удалась сама публикация результата, вход остаётся незафиксированным, и запасным служит
собственный таймаут запрашивающей стороны. Запрос без корректного `sourcecraft_uuid` нельзя
сопоставить: он логируется и пропускается. Для документации то же относится к `job_id`: ответ
повторяет его, и Grade service сопоставляет ответ с прогоном по `job_id`, поэтому параллельные
прогоны одного репозитория не смешиваются. У сводки (`llm.recommendations.*`) ID запроса нет.

## Оценка документации: активный контракт Grade ↔ LLM

Grade → LLM, топик `repo.docs.grade.requested`:

```json
{
  "job_id": "0f6c2b8e-3d4a-4b7e-9c1d-5a2e8f9b7c31",
  "sourcecraft_uuid": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "s3_prefix": "repos/019b2102-fa21-7ab7-8ce1-116af406fb5e/0f6c2b8e-3d4a-4b7e-9c1d-5a2e8f9b7c31",
  "requested_at": "2026-09-26T12:00:00Z"
}
```

Все четыре поля обязательны; `job_id` и `sourcecraft_uuid` — UUID в каноническом виде. LLM-сервис
повторяет `job_id` в ответе (и в успехе, и в сбое). Некорректный запрос с валидными `job_id` и
`sourcecraft_uuid` получает ответ `INVALID_EVENT`; без них — только логируется.

LLM → Grade, топик `repo.docs.graded` (успешная оценка):

```json
{
  "job_id": "0f6c2b8e-3d4a-4b7e-9c1d-5a2e8f9b7c31",
  "sourcecraft_uuid": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "grade": 85,
  "subgrades": {"required_files": 100, "purpose": 80, "local_run": 75},
  "advice": [
    {"code": "DOC_PURPOSE_UNCLEAR", "priority": "high",
     "text": "Уточните, какую задачу решает проект, для чего его используют и какой результат он даёт."}
  ],
  "method": "llm",
  "error": "",
  "evaluated_at": "2026-09-26T12:00:10Z"
}
```

LLM → Grade, тот же топик и ключ Kafka (оценка не удалась):

```json
{
  "job_id": "0f6c2b8e-3d4a-4b7e-9c1d-5a2e8f9b7c31",
  "sourcecraft_uuid": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "grade": null,
  "subgrades": {},
  "advice": [],
  "method": null,
  "error": "CONTENT_FILTER",
  "evaluated_at": "2026-09-26T12:00:10Z"
}
```

`error` — всегда строка: `""` при успехе, иначе короткий стабильный код без секретов и сырого
содержимого модели/репозитория. При сбое `grade` равен `null`, `subgrades` — `{}`, `advice` — `[]`,
`method` — `null` (ноль никогда не обозначает недоступную оценку); Grade считает непустой `error` или `grade: null`
отсутствием данных (`NO_DATA`) для документации и игнорирует остальные поля. `evaluated_at` —
момент завершения попытки, успешной или нет. Отказ фильтра содержимого или исчерпанные попытки
модели/S3 дают результат сбоя; истечение 60-секундного бюджета модели даёт `LLM_TIMEOUT`.
Собственный 70-секундный таймаут Grade покрывает отсутствие обработчика или сбой брокера.
LLM-сервис пропускает запрос старше `LLM_DOCS_REQUEST_MAX_AGE_SECONDS` секунд (по умолчанию 70,
отсчёт от `requested_at`) без чтения S3 и вызова модели: результат не публикуется, смещение
фиксируется. Grade к этому моменту уже не ждёт ответа и отбросил бы его.

Ключи разделов: `required_files`, `purpose`, `local_run`, `build`, `testing`, `usage`,
`structure`, `consistency`. Непросмотренные или неприменимые разделы опускаются в `subgrades` и
никогда не передаются нулём или null. Оценки — целые 0–100, округление половины вверх; итог
нормируется по оценённым проверкам, а не как среднее процентов разделов (см.
`docs/documentation-grading.md`). `advice` — список объектов `{code, priority, text}`: только
рекомендации по улучшению на русском, выбранные по коду недостатка с запасным вариантом на уровне
раздела — без причин, имён файлов, доказательств и хэшей, и без рекомендаций для непросмотренных,
неприменимых и идеальных разделов. Одинаковые тексты не повторяются (остаётся первый код).

- `code` — код недостатка (`DOC_…`, см. `docs/documentation-codes.md`); для запасного текста
  раздела — `DOC_SECTION_<ID раздела в верхнем регистре>` (например, `DOC_SECTION_USAGE`); для
  пометки об упрощённой оценке — `DOC_ALGORITHMIC_NOTICE`.
- `priority` — `critical` | `high` | `medium` | `low`, задаётся в `backend/llm-service/config/advice.yaml`
  для каждого кода. `critical` для документации не используется. `high`: нет README, назначения
  проекта, инструкции запуска, LICENSE, или команда не совпадает со скриптами проекта. `medium`:
  нет основного раздела (возможности, сборка, тесты, пример), неверная ссылка или настройка,
  неполные требования/зависимости/настройки запуска; запасной текст раздела — тоже `medium`.
  `low`: детали существующего раздела, CONTRIBUTING, CODEOWNERS и пометка об упрощённой оценке.

`method` — как получена оценка: `"llm"` — модель оценила выборку документации; `"algorithmic"` —
модель недоступна (режим `auto`) или отключена (режим `algorithmic`), и сервис проверил только
наличие файлов и разделов. При сбое `method` равен `null`. При `"algorithmic"` последним элементом
`advice` по-прежнему идёт пометка `DOC_ALGORITHMIC_NOTICE` («Документация оценена упрощённо, без
LLM») — для потребителей, которые не читают `method`; Grade service передаёт `method` в объект
категории `documentation` в `grading.results`, и фронтенд показывает пометку на карточке
документации.

Метки времени — RFC3339 с часовым поясом. S3-префикс — относительный префикс ключа объекта без
завершающего слэша; сервис только читает объекты репозитория.

## Сводка и рекомендации по репозиторию: активный контракт Aggregator ↔ LLM

Aggregator → LLM, топик `llm.recommendations.requested` (Go `LLMRecommendationsRequestedEvent`):

```json
{
  "visibility": "private",
  "sourcecraft_uuid": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "org_slug": "examples",
  "repo_slug": "demo",
  "grade": {
    "score": 64,
    "category_scores": {"security": 40, "activity": 90, "documentation": 70,
                        "ci_cd": null, "issues": 80, "code_health": 55},
    "advice": [
      {"category_id": "security", "priority": "critical",
       "text": "Есть открытые находки критической важности. …"}
    ],
    "missing_data": ["ci_cd"],
    "graded_at": "2026-09-27T11:59:00.123456Z"
  },
  "requested_at": "2026-09-27T12:00:00+03:00"
}
```

Aggregator отправляет `grade` в snake_case (Go `LLMGrade`). Парсер для совместимости принимает и
Go-имена (`Score`, `CategoryScores`, …); одно и то же поле в обоих написаниях отвергается. Правила:

- `visibility` — `public` или `private`; slug-и непустые; `requested_at` — RFC3339 (точность до
  наносекунд из Go принимается).
- `Score` — целое 0–100 или `null`. `CategoryScores` сопоставляет ID категории с числом 0–100,
  `null` (нет данных) или объектом с полем `score`. `MissingData` — список строк или `null`.
- `advice` — до 50 рекомендаций Grade `{category_id, priority, text}` или `null`. Aggregator берёт
  `priority` из поля рекомендации, а у оценок, сохранённых до появления поля, — из метки
  `"[priority] "` в тексте (иначе `medium`); метку из `text` убирает. Парсер LLM-сервиса так же
  принимает строку старого формата (приоритет из метки или `medium`). `priority` вне
  `critical|high|medium|low` и не-строковый `text` отвергаются; пустые тексты отбрасываются.
  Модель видит рекомендации как `service_recommendations` с категорией и приоритетом.
- `graded_at` — RFC3339 или `null`: время оценки, которую описывает запрос. Сервис копирует его
  без изменений в ответ. Остальные поля `grade` игнорируются.
- Для `public` категории `security` и `ci_cd` скрываются от модели, даже если присутствуют.

LLM → Aggregator, топик `llm.recommendations.received` (Go `LLMRecommendationsReceivedEvent`):

```json
{
  "sourcecraft_uuid": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "raw_response": {
    "summary": "Репозиторий получил 64 балла из 100. …",
    "recommendations": [
      {"priority": "HIGH", "category": "security", "text": "Обновите уязвимые зависимости."}
    ]
  },
  "model_used": "aliceai-llm",
  "evaluated_at": "2026-09-27T12:00:08Z",
  "graded_at": "2026-09-27T11:59:00.123456Z",
  "visibility": "private",
  "error": ""
}
```

`graded_at` — дословная копия `grade.graded_at` из запроса (в том числе при сбое; `null`, если в
запросе его не было или он некорректен). Aggregator сохраняет его в
`repo_llm_responses_*.graded_at` и считает сохранённый ответ актуальным, только если этот
`graded_at` в точности равен `graded_at` текущей оценки той же таблицы; `NULL` (ответы, сохранённые
до появления поля) актуальным не считается, и Aggregator запрашивает сводку заново. Так ответ на
старую оценку, пришедший после переоценки, не выдаётся за сводку новой, а расхождение часов
сервисов не влияет (`evaluated_at` для этого больше не используется).

Правила `raw_response`. Модель пишет только `summary` и тексты рекомендаций; приоритеты задаёт
код по оценкам запроса. Сильные и слабые стороны сюда не входят — фронтенд считает их сам по
оценкам категорий.

- `summary` (модель): непустой текст на русском, не более 1500 символов.
- `recommendations`: до 8 объектов `{priority, category, text}`. Модель пишет `text` (не более
  600 символов) и выбирает `category` — всегда категорию с числовой оценкой, видимую для этого
  `visibility`. `priority` задаёт код по оценке этой категории: `HIGH` ниже 50 или безопасность
  ниже 75; `MEDIUM` ниже 75; иначе `LOW`. Сортировка по приоритету, затем по более слабой
  категории. Модель просят дать хотя бы одну рекомендацию на каждую слабую категорию; это не
  проверяется принудительно.
- Граница 75 выше (`MEDIUM`/`LOW` и `HIGH` для безопасности) —
  `LLM_PRIORITY_THRESHOLD` (целое 0–100, по умолчанию 75; прежнее имя `LLM_STRENGTH_THRESHOLD`
  читается, если новое не задано); граница 50 для `HIGH` фиксирована.
- Лишние маркеры списков и пунктуация по краям текста модели обрезаются. Формулировки не
  проверяются. Модели предписано использовать только данные запроса, не выдумывать цифры и факты
  и не считать отсутствие данных проблемой. Оценки она никогда не меняет.

При сбое `raw_response` равен `null`, а `error` — стабильный код: `INVALID_EVENT`, `NO_DATA` (ни
у одной категории нет оценки, модель не вызывается), `LLM_TIMEOUT`, `LLM_RATE_LIMITED`,
`LLM_PROVIDER_UNAVAILABLE`, `LLM_INVALID_RESPONSE`, `CONTENT_FILTER`, `INTERNAL_ERROR`.
`model_used` заполняется в обоих случаях. `visibility` копируется из запроса или равен `""`, если
в запросе не было корректного значения.

В пределах 60-секундного бюджета модели разрешён один повтор для исправления формата.
