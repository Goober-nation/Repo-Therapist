# Ручное тестирование

Как проверить всё вручную — в соответствии с тем, что уже реально проверено. Автоматические
Go-тесты тоже есть (`go test ./...` в каждом модуле, см. ниже), но сквозной поток проверяется
руками.

## Подготовка

```bash
cp .env.example .env   # впишите в SOURCECRAFT_PAT настоящий токен
docker compose up -d   # Postgres + S3 (RustFS) + Kafka
set -a && source .env && set +a
```

## 1. Только Repository service (разовый запуск)

Клонирует репозиторий, загружает в S3 его дерево, историю git и задачи/вехи. Нужен также
запущенный sourcecraft-wrapper-service (`go run ./backend/sourcecraft-wrapper-service/cmd`,
`SOURCECRAFT_URL=https://api.sourcecraft.tech`) — repository-service обращается к нему по
`SC_WRAPPER_ADDR` (по умолчанию `localhost:8083`).

```bash
go run ./backend/repository-service/cmd/repository-service --org=examples --repo=vulnerable-repo
```

Проверьте, что попало в S3 (консоль RustFS на `http://localhost:8055/rustfs/console/index.html`,
логин из `.env`), или через AWS CLI (сервис в compose по-прежнему называется `minio`):

```bash
docker run --rm --network <compose-project>_default \
  -e AWS_ACCESS_KEY_ID="$S3_ACCESS_KEY" -e AWS_SECRET_ACCESS_KEY="$S3_SECRET_KEY" -e AWS_DEFAULT_REGION=us-east-1 \
  amazon/aws-cli --endpoint-url http://minio:9000 s3 ls --recursive s3://repo-content/repos/<sourcecraft_uuid>/
```

Разовый запуск пишет в `repos/<uuid>/`. В полном стеке выгрузка каждого задания лежит в
`repos/<uuid>/<job_id>/`: Grade service удаляет этот префикс после публикации результата оценки,
а Repository service очищает его перед загрузкой. Чтобы сохранить выгрузки для
проверки, задайте `S3_KEEP_REPO_DATA=true`.

(`<compose-project>` — имя проекта docker-compose, обычно имя каталога; если не уверены,
проверьте через `docker compose ps`.)

## 2. Только Grade service (разовый запуск)

Читает выгрузки репозитория из S3 (из шага 1) и вычисляет оценки категорий.

```bash
go run ./backend/grade-service/cmd/grade-service --repo-id=<sourcecraft_uuid>
```

Печатает полную разбивку оценки в JSON — для каждой метрики сырое значение, оценку, вес и признак
отсутствия данных.

## 3. Полный асинхронный поток через Kafka

Запустите оба сервиса в режиме прослушивания (в отдельных терминалах или в фоне):

```bash
go run ./backend/repository-service/cmd/repository-service --listen
go run ./backend/grade-service/cmd/grade-service --listen
```

Оба сервиса подключаются к Redis (`REDIS_ADDRESS`/`REDIS_PASSWORD` из `.env`; Redis и Kafka
в compose доступны только с этой машины: `127.0.0.1:8052` и `127.0.0.1:8053`).

Затем запустите оценку из третьего терминала. Команда публикует `grading.requests` точно так же,
как Aggregator, и печатает сообщение `grading.results` для этого репозитория:

```bash
go run ./backend/grade-service/cmd/grading-request --org=examples --repo=vulnerable-repo
```

С `--visibility=private` команда, как Aggregator, кладёт `SOURCECRAFT_PAT` в Redis под ключом
`grading:token:<job_id>` (TTL 20 минут), а в Kafka отправляет только `job_id`.

`grading.results` содержит только UUID репозитория, поэтому `grading-request` сначала ждёт
сообщение `repo.ready` для этой организации/репозитория (в нём есть UUID), а затем результат с
этим UUID и видимостью. Сообщения других запросов пропускаются, так что запоздавший результат
предыдущего запроса никогда не будет напечатан как текущий. Команда сразу завершается с ненулевым
кодом, если клонирование не удалось (`repo.ready` со статусом `failed`) или в результате есть
`error`; иначе сдаётся по истечении `--wait` (по умолчанию 10m, покрывает стандартный лимит
клонирования).

Следите за двумя терминалами-слушателями: Repository service логирует клонирование/загрузку и
публикует `repo.ready`; Grade service логирует оценивание и публикует результат.

**Примечание**: если вы тестируете процессы `--listen` через `go run ... &` в shell-скрипте,
`kill <pid>` не остановит сам скомпилированный бинарник (обработка сигналов `go run` не
передаётся дочернему процессу) — сначала соберите бинарник и запускайте его напрямую, или
используйте `pkill -f <binary-name>`, если нужно скриптовать запуск/остановку.

## 4. Пакетно: все репозитории-кандидаты

`scripts/grade-candidates.sh` выполняет шаг 3 по очереди для каждого репозитория из
`docs/candidate-repos.json` с `passes_full_criteria=true`:

1. Если запущен контейнер `go-services`, он останавливается (в нём более старые сборки тех же
   сервисов, и он перехватывал бы сообщения пакета) и снова запускается при выходе из скрипта, в
   том числе по Ctrl-C.
2. Запускаются только инфраструктурные контейнеры (postgres, kafka, minio, migrate); три сервиса
   собираются из текущего кода и запускаются локально.
3. Каждый репозиторий запрашивается с `--wait=15m`; аргументы скрипта передаются в
   `grading-request` и переопределяют это значение (например, `--wait=20m`).
4. Репозитории без результата в конце перезапрашиваются один раз, затем печатается сводка: сколько
   оценено, сколько восстановилось при повторе и какие так и не удались.

Для документации нужен LLM-сервис; для пакета без него: `LLM_ENABLED=false scripts/grade-candidates.sh`.

**Лимиты времени** (стабильность важнее скорости, 2026-09-27): repository-service даёт каждому
репозиторию `CLONE_TIMEOUT` (по умолчанию 10m) на клонирование + выгрузку + загрузку,
grade-service — `GRADE_TIMEOUT` (по умолчанию 4m) на оценивание. Каждый потребитель обрабатывает
по одному сообщению, поэтому репозиторий, исчерпавший весь лимит, задерживает стоящие за ним в
очереди. Временные ошибки SourceCraft/AppSec повторяются внутри обёрток
(`HTTP_RETRY_ATTEMPTS`, `HTTP_RETRY_WAIT_SECONDS`).

## Публичный API, бейджи и виджет

Нужны Postgres и Redis из compose (`docker compose up -d postgres redis`), хотя бы один оценённый
публичный репозиторий в БД, а также запущенные rating-service и gateway-service с теми же `DB_*` и
`REDIS_*`, что и в `.env` (адрес Postgres на хосте — порт 8051, Redis — 8052). Aggregator для этих
маршрутов не нужен.

```bash
G=localhost:8080; R=<org>/<repo>      # оценённый публичный репозиторий
curl -si "$G/score/$R"                # 200, status FINISHED, Access-Control-Allow-Origin: *, max-age=60
curl -s  "$G/score/nope/nope"         # 404 {"error":"not found"}
curl -s  "$G/badge/$R.json"           # {"schemaVersion":1,"label":"repo health","message":"<score>",...}
curl -s  "$G/badge/$R/activity.svg"   # SVG с оценкой категории; documentation без данных -> n/a
curl -so /dev/null -w "%{http_code}\n" "$G/badge/$R/security.svg"   # 404: приватная категория
curl -s  "$G/widget/$R.svg" > widget.svg   # откройте в браузере: карточка с оценкой и 4 категориями
for i in $(seq 1 6); do curl -so /dev/null -w "%{http_code} " "$G/score/$R"; done   # с PUBLIC_API_RATE_LIMIT=5: 200 ... 429
```

Репозиторий, у которого нет публичной оценки, отдаёт `200` со `status: NO_DATA`; непубличный —
`404` на JSON и серый `n/a` на картинках. Как SourceCraft показывает SVG в README, проверяется
только вручную на самой платформе.

## Флаг низкой достоверности

Нужен полный стек (`docker compose up -d --build`, фронтенд на http://localhost:8081). Флаг
считается при публичной оценке, поэтому репозитории нужно оценить заново (оценка, сохранённая до
появления флага, остаётся без флага — «неизвестно»). Ручной прогон без изменения других оценок:

```bash
set -a; . ./.env; set +a
(cd backend/grade-service && go build -o /tmp/grading-request ./cmd/grading-request)
KAFKA_BROKERS=localhost:8053 REDIS_ADDRESS=localhost:8052 /tmp/grading-request --org=<org> --repo=<repo> --visibility=public
```

Печатаемый `grading.results` содержит `confidence`. Затем:

```bash
curl -s "localhost:8081/api/score/<org>/<repo>"                       # low_confidence, confidence_reasons; null = неизвестно
curl -s "localhost:8081/api/ranking?hide_low_confidence=true"         # без помеченных; total_count уменьшился
curl -s "localhost:8081/api/ranking?hide_low_confidence=maybe"        # 400
docker compose exec postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "select * from repo_confidence_public"
```

В браузере: на главной странице у помеченных строк бейдж «Мало данных» (причины во всплывающей
подсказке), переключатель «Скрыть оценки с низкой достоверностью» выключен по умолчанию и убирает
такие строки, не меняя порядок остальных; на странице публичного репозитория — блок «Низкая
достоверность оценки» со списком причин; у приватного анализа владельца блока нет. Автотесты
сценариев накрутки и самого флага:

```bash
(cd backend/grade-service && go test ./internal/app/ ./internal/scoring/ -run 'Gaming|Confidence' -v)
```

## Защита от накрутки: сценарии и калибровка порогов

Сценарии накрутки (без внешних сервисов) и проверка боевых значений фильтров:

```bash
(cd backend/grade-service && go test ./internal/app/ ./internal/scoring/ ./internal/config/ -run 'Gaming|Hardening|FilterCommits|FilterIssues|FilterMilestones|ComputeCategoryFilters' -v)
```

Калибровка порогов на реальных историях (вручную, нужны клоны репозиториев и их задачи/вехи): для
каждого репозитория в `CALIB_DIR` лежат `clones/<org>__<repo>/` (git clone с `--shallow-since`),
`exports/<org>__<repo>/{issues,milestones}.json` и `db_categories.json` с сохранёнными оценками
остальных категорий. Тест пересчитывает Активность и Issues без фильтров и с каждым фильтром:

```bash
CALIB_DIR=<каталог> go test -tags=manual ./internal/app/ -run TestHardeningCalibrationManual -v
```

Критерий выбора порога: ни один реальный репозиторий не сдвигается по итоговой оценке больше чем
на 3 балла. После изменения порога в `config/grading.yaml` перезапустите тест, обновите таблицы в
`docs/grading/00-overview.md` и ожидаемые значения в `TestGamingScenariosTable` и
`TestShippedHardeningFilters`.

## Справочная аналитика (фактор автобуса, карта владельцев)

Нужен полный стек (`docker compose up -d --build`). Аналитика считается при оценке (публичной и
приватной), поэтому репозиторий нужно оценить заново; ручной прогон без изменения других оценок
описан в разделе «Флаг низкой достоверности». Затем:

```bash
curl -s "localhost:8081/api/analytics/<org>/<repo>"      # status FINISHED и объект analytics; NO_DATA, если ещё нет
docker compose exec postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "select sourcecraft_uuid, graded_at from repo_analytics_public"
(cd backend/grade-service && go test ./internal/analytics/ -v)
# Ревью PR (этап B): после повторной оценки в объекте analytics есть блок reviews (status ok или no_data с reason);
# выгрузка лежит в S3: <prefix>/_meta/pulls.json (при S3_KEEP_REPO_DATA=true)
# Время до первого ответа на задачи: объект analytics содержит issue_replies; у репозиториев с задачами от
# трёх и более людей (libmdbx, kernel-hardening-checker) — число, у однорукого трекера (cosmos-ontology) — no_data few_authors;
# выгрузка в S3: <prefix>/_meta/issue-replies.json; в категории Issues метрика median_first_reply_hours (вес 0.15)
# Репозиторий с ревью от других людей для проверки: team23/mobile-ios (5 из 10 последних PR); у imbok/uims-portal ревьюер — сам автор
```

В браузере на странице репозитория раздел «Углублённая аналитика»: карточка фактора автобуса с
долями авторов, карточка CODEOWNERS и таблица каталогов. Репозиторий с малым числом коммитов
показывает «Нет данных…», а не нули; у владельца приватного репозитория (вход по токену) данные
приватные.

## Проверки сборки/линтера

Каждый Go-сервис бэкенда — отдельный модуль (связаны через корневой `go.work`), поэтому запускайте
проверки по модулям:

```bash
for d in backend/grade-service backend/repository-service backend/security-wrapper-service backend/sourcecraft-wrapper-service backend/aggregator-service backend/gateway-service backend/rating-service; do
  (cd "$d" && go build ./... && go vet ./...)
done
gofmt -l backend         # не должно ничего вывести
```

## Тесты с тегом manual

Одному тесту нужен живой S3 (сервис `minio` в compose) с уже загруженными данными (не запускается обычным `go test ./...`):

```bash
(cd backend/repository-service && go test -tags=manual ./internal/storage/ -run TestBatchReadManual -v)
```

## Тестовые репозитории для CI/CD

Пять публичных репозиториев в `timoxa102928` с известной историей CI — идентификаторы и ожидаемые
оценки см. в `docs/ci-mock-repos.json`. Нужен PAT владельца этой организации (`cicd/runs`
доступен только владельцу).

Офлайн-тесты на фикстурах (входят в обычный `go test ./...`):

```bash
(cd backend/grade-service && go test ./internal/scoring/ -run CICD -v)
```

Живая проверка против реального API (только подсчёт, запуски читаются напрямую через REST):

```bash
(cd backend/grade-service && go test -tags live ./internal/scoring/ -run LiveMockRepos -v)
```

Полный конвейер для одного репозитория (должен быть запущен sourcecraft-wrapper-service):

```bash
go run ./backend/repository-service/cmd/repository-service --org=timoxa102928 --repo=ci-mock-flaky
go run ./backend/grade-service/cmd/grade-service --repo-id=01a0dd5d-d258-7af2-9b1b-b2346d6d29b6 | jq .categories.ci_cd
```

Пересоздание репозиториев: `scripts/mock-repos/create-all.sh` (пропускает репозитории, в которых
уже есть содержимое; `--force` пушит заново и добавляет новые запуски, что меняет ожидаемые
оценки). Свежие запуски несколько минут стоят в очереди SourceCraft — живой тест быстро падает,
пока хоть один запуск ещё в состоянии `prepared`/`processing`.

Приватный анализ у аккаунта с большим числом репозиториев требует увеличенных таймаутов
(`AGGREGATOR_GRPC_TIMEOUT`, `GRPC_TIMEOUT` в `.env`, например `120s`, и `HTTP_WRITE_TIMEOUT`), иначе запрос с `PAT`
завершится `504` из-за просмотра `/me/repos`; после проверки верните значения по умолчанию.

## История оценок, динамика и сравнение

Предусловие: стек запущен, у публичных репозиториев есть оценки. Описание: [ranking.md](ranking.md).

```bash
curl -s "localhost:8081/api/history/<org>/<repo>"     # points от старых к новым; NO_DATA без истории
curl -s "localhost:8081/api/position/<org>/<repo>"    # place, total, above/below, categories, weakest_category
curl -s "localhost:8081/api/ranking?page_size=5"      # score_change, place_change, grade_count, dynamics_days
curl -s localhost:8081/api/regrade-status              # enabled, next_run_at, running, total/done
docker compose exec postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "select count(*) from repo_grade_history"
(cd backend/rating-service && POSTGRES_DSN="$POSTGRES_DSN" go test ./... -v)    # включая SQL динамики на реальном Postgres
(cd backend/aggregator-service && POSTGRES_DSN="$POSTGRES_DSN" go test ./internal/database/... -v)
```

Фильтры рейтинга (размер, CI, SECURITY.md, тематика; в браузере — на странице `/`):

```bash
curl -s "localhost:8081/api/ranking?size=large&ci=true"            # total_count и записи только с известными фактами
curl -s "localhost:8081/api/ranking?topic=онтолог"                 # поиск по названию и описанию без учёта регистра
curl -s -o /dev/null -w '%{http_code}\n' "localhost:8081/api/ranking?size=huge"   # 400
docker compose exec postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "select * from repo_facts_public"
```

Репозиторий появляется в фильтрах по фактам после первой оценки с фактами (старая оценка пересчитывается
при открытии страницы репозитория).

Динамика появляется, когда у репозитория есть оценка старше `RANKING_DYNAMICS_DAYS` дней. Для
локальной проверки интерфейса можно вручную добавить в `repo_grade_history` строки со старыми
`graded_at` (только в локальной базе) и удалить их после проверки. В браузере: `/` (стрелки и ±Score
у строк; метка «новый» только у репозиториев с одной оценкой; кнопка «Фильтры» со счётчиком и значок «i»;
строка с таймером переоценки под заголовком; галочки в последнем столбце — у второго отмеченного
репозитория появляется кнопка «Сравнить →»), `/compare/<орг>/<репо>/<орг>/<репо>`
и `/repo/<орг>/<репо>` (карточки «Почему это место» и «История оценки»), в том числе в ширине 375 px.


## Документация без модели

`docker compose exec llm-service printenv LLM_DOCS_MODE` должен вернуть `algorithmic`; в логе
`llm-service` — `Documentation mode: algorithmic`. После пересчёта репозитория оценка документации
идёт с `method: "algorithmic"`, на карточке — «Упрощённая оценка: проверено только наличие разделов».
Вернуть модель: `LLM_DOCS_MODE=auto` в `.env` и `docker compose up -d llm-service`.

## Выгрузка отчёта (Markdown / PDF)

Описание: [grading/frontend-results.md](grading/frontend-results.md). Тесты: `cd frontend && npm test`
(`tests/reports.test.js`: структура Markdown, экранирование, пустые данные, сборка PDF с эмодзи,
длинными текстами и 40 рекомендациями).

Проверка глазами: откройте `/repo/<орг>/<репо>` (публичный) и приватный репозиторий через `/my`, нажмите
«Скачать отчёт» → «Скачать MD» и «Скачать PDF». Для PDF-файла удобно `pdftoppm -r 80 -png файл.pdf страница`
и просмотр картинок. Проверьте: пустой репозиторий без оценки; репозиторий без истории и аналитики;
низкая достоверность; документация с пометкой «Упрощённая оценка»; очень длинные названия и рекомендации.
Приватный анализ на аккаунте с многими репозиториями требует увеличенных `AGGREGATOR_GRPC_TIMEOUT`,
`GRPC_TIMEOUT` и `HTTP_WRITE_TIMEOUT` (см. README, «Ограничения»).

## Аналитика платформы и выгрузка

Описание: [platform-analytics.md](platform-analytics.md). Автотесты: `go test ./...` в
`backend/rating-service` (агрегаты, кэш, страницы выгрузки), `backend/gateway-service` (маршруты
`/stats` и `/export/*`, лимит, обрыв потока) и `frontend/tests/stats.test.js`.

Живая проверка (стек запущен, у публичных репозиториев есть оценки):

```bash
curl -s localhost:8080/stats | head -c 400                        # агрегаты
curl -s -D- localhost:8080/export/grades.csv | head -8            # attachment, text/csv, заголовок и строки
curl -s localhost:8080/export/grades.json | python3 -c "import json,sys; d=json.load(sys.stdin); print(d['total'], len(d['grades']))"
for i in 1 2 3 4 5 6; do curl -s -o /dev/null -w "%{http_code} " localhost:8080/export/grades.csv; done   # после пятой — 429
```

Сверка: число строк выгрузки равно `graded_repos` из `/stats`; ни один репозиторий с видимостью не
`public` в выгрузке нет.

## Доступ админа и журнал действий

Описание: [admin.md](admin.md). Автотесты: `go test ./...` в `backend/aggregator-service` (проверка и
хранение событий, страницы с курсором, срок хранения, RPC), `backend/gateway-service` (проверка сессии и
роли, запись отказов без утечек, лимит записи, защита заголовком, маршруты) и `frontend/tests/admin.test.js`.
Тест хранилища журнала (`audit_integration_test.go`) пропускается без `POSTGRES_DSN`; с ним он работает на
настоящей базе с применённой миграцией 000008:

```bash
POSTGRES_DSN="postgres://$POSTGRES_USER:$POSTGRES_PASSWORD@localhost:8051/$POSTGRES_DB?sslmode=disable" \
  go test ./internal/database/postgres-repositories/ -run Audit -v
```

Живая проверка и создание тестовой сессии описаны в разделе «Как проверить на своей машине» в admin.md.
После проверки удалите созданные строки: `delete from audit_log; delete from users where yandex_id = '...'`.

### Доступ админа по паролю

Автотесты: `backend/gateway-service` (пароль и блокировка, отзыв прав при смене пароля, права только своей
сессии, отсутствие гостевых учётных записей, утечки в журнале, маршруты `/admin/unlock` и `/admin/lock`,
заголовок), `frontend/tests/access.test.js`. Тест хранилищ Redis пропускается без `REDIS_TEST_ADDRESS`; с ним
он работает на настоящем Redis, включая скрипт блокировки:

```bash
REDIS_TEST_ADDRESS=localhost:8052 REDIS_TEST_PASSWORD=$REDIS_PASSWORD \
  go test ./internal/database/redis/ -run 'AdminGrant|AttemptGuard' -v
```

Живая проверка: запустите Gateway с `ADMIN_PASSWORD` и коротким `ADMIN_UNLOCK_LOCK=20s`,
создайте тестового пользователя и сессию (см. admin.md) и пройдите: неверный пароль — `403`, пять неверных —
`429` с `Retry-After`, верный пароль во время блокировки — `429`, после блокировки — `200`, `/admin/audit` — `200`,
`/admin/lock` — права сняты; после смены `ADMIN_PASSWORD` и перезапуска права снова нет.

### Журнал действий пользователей

Автотесты: `backend/gateway-service/internal/service/user_audit_test.go` (кто записан, хеш вместо названия при токене и
при отказе, исходы, один раз за окно, непохожие названия, выключенный журнал), `internal/transport/http/admin_test.go`
(обработчики передают действие, куку, наличие токена и ошибку), `backend/aggregator-service` (проверка `scope`) и
`frontend/tests/admin.test.js` (подписи, фильтры, запрос с `scope` и `action`, блок на странице).

С настоящим Postgres (миграция 000008 применена; строки помечены своим хешем и удаляются тестом):

```bash
cd backend/aggregator-service
POSTGRES_DSN='postgres://user:pass@localhost:8051/repotherapist?sslmode=disable' \
  go test ./internal/database/postgres-repositories -run Audit -v
```

Живая проверка: отдельные Postgres и темы Kafka, Aggregator и Gateway, у Aggregator — заглушка обёртки SourceCraft
(иначе репозиторий не определить). Запросы `/grading/...` с кукой и без неё, с заголовком `PAT` и без, шесть опросов подряд
(одна запись), неизвестный и закрытый репозиторий; затем `GET /admin/audit?scope=users` и `scope=admin` от администратора.
В таблице не должно быть названия закрытого репозитория, IP и Яндекс-идентификатора.

### Пакетная переоценка

Автотесты: `backend/aggregator-service` (условия и их проверка, пробный расчёт, не больше трёх репозиториев
одновременно, пауза, продолжение, отмена, ошибки по репозиториям, один пакет за раз, продолжение после перезапуска,
пропуск репозитория, ставшего приватным; запускайте с `-race`), `backend/gateway-service` (маршруты, заголовок,
`code` в ошибках, `current` без пакетов), `frontend/tests/regrade.test.js`. Тест хранилища
(`batch_integration_test.go`) пропускается без `POSTGRES_DSN`; с ним он работает на настоящей базе с миграцией 000009
и создаёт и удаляет свои репозитории в организации `zz-batch-test`:

```bash
POSTGRES_DSN="postgres://$POSTGRES_USER:$POSTGRES_PASSWORD@localhost:8051/$POSTGRES_DB?sslmode=disable" \
  go test ./internal/database/postgres-repositories/ -run Batch -v
```

Живая проверка без вреда для стенда: запустите свой Aggregator с другими темами Kafka
(`KAFKA_GRADING_REQUESTS_TOPIC`, `KAFKA_GRADING_RESULTS_TOPIC` и группой потребителей), чтобы настоящие сервисы
оценки их не видели, и коротким `BATCH_REGRADE_ITEM_TIMEOUT=25s`; добавьте в базу несколько публичных тестовых
репозиториев в отдельной организации (строки в `repos` и `repo_lock`) и выбирайте их своим списком. Репозитории
в таком запуске завершаются ошибкой «результат не пришёл вовремя»; успешный путь проверяют автотесты. После
проверки удалите тестовые репозитории, пакеты, записи журнала и ключи Redis.

### Метрики и графики

Описание: [metrics.md](metrics.md). Автотесты: `go test ./...` в `backend/gateway-service` (пакеты `metrics` и `series`:
метки по шаблону маршрута, неизвестный путь, перехватчики gRPC, известные ряды с нуля, запросы графиков к
Prometheus, кэш, публичные запросы только по публичным оценкам, округление счётчиков, график заканчивается «сейчас»;
маршруты `/stats/series` и `/admin/metrics`), `backend/aggregator-service` (счётчики запуска, результата и времени
оценки, источник запуска `user`/`schedule`/`batch`, отказ по лимиту, LLM, пакеты) и `backend/rating-service` (кэш
аналитики), `frontend/tests/series.test.js`. Сервис LLM (Python): `tests/test_metrics.py` проверяет, что вызов
модели считается с итогом и временем, токены берутся из `usage`, отклонённый ответ всё равно считает токены, неверное
`usage` не считается, метка `model` не содержит адреса с папкой облака. Тесты запускаются в контейнере с той же версией
Python, что и сервис:

```bash
docker run --rm -v "$PWD/backend/llm-service:/app:ro" -w /tmp python:3.11-slim sh -c "cp -r /app /tmp/svc && cd /tmp/svc && pip install -q . pytest && python -m pytest -q -p no:cacheprovider"
```

Живая проверка: поднимите Prometheus и сервисы, откройте `http://localhost:8056/targets` (три цели `UP`), потом
сделайте запросы к Gateway и посмотрите `curl localhost:9101/metrics` изнутри контейнера: должны быть
`gateway_http_requests_total` с шаблонами маршрутов и ни одного настоящего названия репозитория. Для графиков
откройте вкладку «Аналитика» и вкладку «Доступ админа» администратором.

### Выход из аккаунта

Автотесты: `backend/gateway-service` (выход удаляет сессию и право по паролю, идемпотентность, лимит запросов, сбои
хранилищ, `signed_in` без админских настроек, маршрут, заголовок, стирание куки), `frontend/tests/logout.test.js`
(потерянная сессия, ответ для чужой личности, кнопка в верхней панели, запрос выхода). Удаление сессии в Redis
проверяет `TestSessionDeleteRoundTrip` (нужен `REDIS_TEST_ADDRESS`, см. выше).

Живая проверка: с тестовой сессией (см. admin.md) `POST /auth/logout` без заголовка — `403`; с заголовком — `200`
и `Set-Cookie: session_id=; Max-Age=0`; после этого `GET /admin/me` с той же кукой даёт `signed_in: false`, а ключи
`session:*` и `admin_grant:*` в Redis исчезают. В браузере: профиль в `sessionStorage` без сессии на сервере
пропадает после загрузки страницы сам; с живой сессией кнопка «Выйти» очищает профиль, токен и куку и открывает рейтинг.

