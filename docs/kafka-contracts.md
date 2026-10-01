# Контракты сообщений Kafka

Область: формы сообщений, публикуемых и потребляемых на связях Kafka из схемы архитектуры:

- Aggregator ↔ Grade service — топики `grading.requests` / `grading.results`, **описаны ниже**.
- Grade service ↔ Repository service — **описано ниже**. Этот обмен целиком в зоне одного
  человека (Repository + Grade service), поэтому не требовал общего согласования команды.
- Grade service ↔ LLM service — **описано ниже как плоские сообщения `repo.docs.*`** для анализа
  документации.
- Aggregator ↔ LLM service — **описано ниже как плоские сообщения `llm.recommendations.*`** для
  сводки по репозиторию и приоритизированных рекомендаций.
- Grade service ↔ Security wrapper — не через Kafka: Grade service вызывает обёртку напрямую по
  gRPC (`GetAppsecResult`), см. `grading/security.md`.

## Переменные окружения топиков (2026-09-28)

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
как запасные; если заданы обе, побеждает переменная `KAFKA_*`.

## `repo.grade.requested`

Производитель: Grade service, ретранслирующий каждое сообщение `grading.requests` от Aggregator
(или от ручной команды `grading-request`). Потребитель: Repository service.

```json
{
  "org_slug": "examples",
  "repo_slug": "vulnerable-repo",
  "requested_at": "2026-09-19T23:49:12+03:00",
  "token": "<необязательный PAT SourceCraft пользователя>"
}
```

Собственные топики Aggregator в том виде, как их читает/пишет Grade service:

- `grading.requests`: `{org_slug, repo_slug, visibility, requested_at, token?}`. Токен означает
  приватное оценивание; без токена — публичное. Непубличный `visibility` без токена отбрасывается.
- `grading.results`: `{sourcecraft_uuid, visibility, score, category_scores, advice,
  missing_data, graded_at, error}`. `visibility` называет таблицу оценок, в которую пишет
  Aggregator. `advice` — все рекомендации `{category_id, metric_id, text}` по порядку
  категорий. Для документации это тексты
  `advice` из `repo.docs.graded`, каждый как `{"documentation", "llm_advice", "[medium] <текст>"}`. `error` при успехе — `""`.
  При сбое это код — `REPO_FETCH_FAILED` (repo.ready со статусом failed), `GRADING_FAILED`,
  `GRADING_INTERRUPTED` (grade-service остановлен посреди оценки), `RESULT_BUILD_FAILED` — а
  `score`, `category_scores` и `advice` равны null; Aggregator тогда снимает блокировку
  репозитория и один раз сообщает о сбое следующему запросу оценки этого репозитория. Сбой без
  `sourcecraft_uuid` нельзя сопоставить, и он не публикуется; такую блокировку Aggregator
  снимает сам, когда она занята дольше `GRADING_LOCK_TIMEOUT` (по умолчанию 20m).

`token` копируется из `grading.requests`. Если он есть, Repository service использует его для
вызовов обёртки и git clone; если нет — свой `SOURCECRAFT_PAT`. Токен никогда не логируется и
намеренно **не** повторяется в `repo.ready` — Grade service хранит его в памяти (по ключу
`org/repo`, удаляется при `repo.ready` или через 30 минут) для собственного вызова Security
wrapper.

## `repo.ready`

Производитель: Repository service, после завершения (успешного или нет) обработки сообщения
`repo.grade.requested`. Потребитель: Grade service.

```json
{
  "org_slug": "examples",
  "repo_slug": "vulnerable-repo",
  "sourcecraft_uuid": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "s3_prefix": "repos/019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "status": "ready"
}
```

`status` — `"ready"` или `"failed"`; при сбое `error` содержит сообщение, а `s3_prefix`
опускается. `sourcecraft_uuid` передаётся и при сбое, если репозиторий успел определиться;
тогда Grade service публикует `REPO_FETCH_FAILED` в `grading.results`, и Aggregator сразу
снимает блокировку. Без `sourcecraft_uuid` сбой только логируется, блокировка снимается по
`GRADING_LOCK_TIMEOUT`.

Go-типы: `internal/contracts/repo_grading.go`. Проверено сквозным прогоном (2026-09-19): ручной
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
сопоставить: он логируется и пропускается. ID запроса нет, поэтому параллельные прогоны для одного
репозитория различить нельзя.

## Оценка документации: активный контракт Grade ↔ LLM

Grade → LLM, топик `repo.docs.grade.requested`:

```json
{
  "sourcecraft_uuid": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "s3_prefix": "repos/019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "requested_at": "2026-09-26T12:00:00Z"
}
```

LLM → Grade, топик `repo.docs.graded` (успешная оценка):

```json
{
  "sourcecraft_uuid": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "grade": 85,
  "subgrades": {"required_files": 100, "purpose": 80, "local_run": 75},
  "advice": ["Уточните, какую задачу решает проект, для чего его используют и какой результат он даёт."],
  "error": "",
  "evaluated_at": "2026-09-26T12:00:10Z"
}
```

LLM → Grade, тот же топик и ключ Kafka (оценка не удалась):

```json
{
  "sourcecraft_uuid": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "grade": null,
  "subgrades": {},
  "advice": [],
  "error": "CONTENT_FILTER",
  "evaluated_at": "2026-09-26T12:00:10Z"
}
```

`error` — всегда строка: `""` при успехе, иначе короткий стабильный код без секретов и сырого
содержимого модели/репозитория. При сбое `grade` равен `null`, `subgrades` — `{}`, `advice` — `[]`
(ноль никогда не обозначает недоступную оценку); Grade считает непустой `error` или `grade: null`
отсутствием данных (`NO_DATA`) для документации и игнорирует остальные поля. `evaluated_at` —
момент завершения попытки, успешной или нет. Отказ фильтра содержимого или исчерпанные попытки
модели/S3 дают результат сбоя; истечение 60-секундного бюджета модели даёт `LLM_TIMEOUT`.
Собственный 70-секундный таймаут Grade покрывает отсутствие обработчика или сбой брокера.

Ключи разделов: `required_files`, `purpose`, `local_run`, `build`, `testing`, `usage`,
`structure`, `consistency`. Непросмотренные или неприменимые разделы опускаются в `subgrades` и
никогда не передаются нулём или null. Оценки — целые 0–100, округление половины вверх; итог
нормируется по оценённым проверкам, а не как среднее процентов разделов (см.
`docs/documentation-grading.md`). `advice` содержит только рекомендации по улучшению на русском,
выбранные по коду недостатка с запасным вариантом на уровне раздела — без причин, имён файлов,
доказательств и хэшей, и без рекомендаций для непросмотренных, неприменимых и идеальных разделов.

Метки времени — RFC3339 с часовым поясом. S3-префикс — относительный префикс ключа объекта без
завершающего слэша; сервис только читает объекты репозитория.

## Сводка и рекомендации по репозиторию: активный контракт Aggregator ↔ LLM (2026-09-27)

Aggregator → LLM, топик `llm.recommendations.requested` (Go `LLMRecommendationsRequestedEvent`):

```json
{
  "visibility": "private",
  "sourcecraft_uuid": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "org_slug": "examples",
  "repo_slug": "demo",
  "grade": {
    "Score": 64,
    "CategoryScores": {"security": 40, "activity": 90, "documentation": 70,
                       "ci_cd": null, "issues": 80, "code_health": 55},
    "Advice": ["Обновите уязвимые зависимости."],
    "MissingData": ["ci_cd"],
    "GradedAt": "2026-09-27T11:59:00Z"
  },
  "requested_at": "2026-09-27T12:00:00+03:00"
}
```

`grade` — это Go-структура `RepoGrade`, у которой есть только теги `db`, поэтому её поля приходят
с Go-именами. Парсер принимает и Go-имена (`Score`, `CategoryScores`, …), и snake_case (`score`,
`category_scores`, …); одно и то же поле в обоих написаниях отвергается. Правила:

- `visibility` — `public` или `private`; slug-и непустые; `requested_at` — RFC3339 (точность до
  наносекунд из Go принимается).
- `Score` — целое 0–100 или `null`. `CategoryScores` сопоставляет ID категории с числом 0–100,
  `null` (нет данных) или объектом с полем `score`. `Advice` и `MissingData` — списки строк или
  `null`. Остальные поля `grade` игнорируются.
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
  "visibility": "private",
  "error": ""
}
```

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
  `LLM_STRENGTH_THRESHOLD` (целое 0–100, по умолчанию 75); граница 50 для `HIGH` фиксирована.
- Лишние маркеры списков и пунктуация по краям текста модели обрезаются. Формулировки не
  проверяются. Модели предписано использовать только данные запроса, не выдумывать цифры и факты
  и не считать отсутствие данных проблемой. Оценки она никогда не меняет.

При сбое `raw_response` равен `null`, а `error` — стабильный код: `INVALID_EVENT`, `NO_DATA` (ни
у одной категории нет оценки, модель не вызывается), `LLM_TIMEOUT`, `LLM_RATE_LIMITED`,
`LLM_PROVIDER_UNAVAILABLE`, `LLM_INVALID_RESPONSE`, `CONTENT_FILTER`, `INTERNAL_ERROR`.
`model_used` заполняется в обоих случаях. `visibility` копируется из запроса или равен `""`, если
в запросе не было корректного значения.

В пределах 60-секундного бюджета модели разрешён один повтор для исправления формата.
