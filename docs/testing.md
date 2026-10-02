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
