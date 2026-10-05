# Repo Health frontend

React/Vite-фронтенд сервиса оценки здоровья репозиториев. Данные берёт только у API Gateway
(`/api/*`); моков нет.

## Запуск

```bash
corepack pnpm install
corepack pnpm dev
```

`dev` и `build` сначала копируют `docs/grading/*.md` в `src/rules` (`scripts/sync-rules.mjs`,
кроме `frontend-results.md`); без каталога `docs/` остаётся закоммиченная копия.

Проверки:

```bash
corepack pnpm test
corepack pnpm exec tsc --noEmit
corepack pnpm run build
```

## Соединение с бэкендом

Браузер обращается к тому же origin, путь `/api/*` проксируется на Gateway:

- в разработке — Vite proxy, адрес задаётся `GATEWAY_URL` в `frontend/.env` (по умолчанию
  `http://localhost:8080`, префикс `/api` отбрасывается);
- в Docker — nginx (`nginx.conf`): `/api/` → `go-services:8080`; фронтенд на `http://localhost:8081`.

Вход через Яндекс ID включается, когда при сборке задан `VITE_YANDEX_CLIENT_ID` (в Docker берётся
из `.env` корня репозитория). Токен SourceCraft пользователь вводит на странице «Мои репозитории»;
он хранится в `localStorage` 1 час (`src/patStorage.ts`) и передаётся заголовком `PAT`.

## Страницы

| Путь | Страница |
|---|---|
| `/` | публичный рейтинг (сортировка, кнопка «Фильтры» с языком, размером, CI, SECURITY.md, тематикой и переключателем «Скрыть оценки с низкой достоверностью», изменение места и Score за неделю со значком «i», таймер плановой переоценки, галочки в последнем столбце и кнопка «Сравнить →» у второго выбранного) |
| `/repo/:org/:repo` | публичный анализ: оценка, категории, рекомендации, «Почему это место», история оценки, углублённая аналитика, бейджи и виджет |
| `/my` | репозитории владельца токена |
| `/my/:org/:repo` | приватный анализ с токеном владельца |
| `/compare/:orgA/:repoA/:orgB/:repoB` | сравнение двух публичных репозиториев: Score, категории, общий график истории |
| `/rules[/:section]` | правила оценки (по одному разделу на категорию) |

## Структура

- `src/api/contracts.ts` — DTO ответов Gateway;
- `src/api/client.ts` — HTTP-клиент маршрутов Gateway (`/grading`, `/recommendations`, `/analytics`, `/ranking`, `/history`, `/position`, `/score`, `/regrade-status`, `/repos`, `/auth/yandex`);
- `src/api/mapper.ts` — преобразование DTO в модель UI;
- `src/api/yandexAuth.ts`, `src/components/YandexSignIn.tsx` — вход через Яндекс ID;
- `src/useRepositoryAnalysis.ts` — запуск и опрос анализа;
- `src/pages/`, `src/components/` — страницы и компоненты (`AnalyticsSection`, `BadgeSnippet`, `AnalysisResults`);
- `src/reports/` — выгрузка отчёта в Markdown и PDF;
- `src/rules/` — копии документов `docs/grading`;
- `tests/` — тесты (`node --test`).
