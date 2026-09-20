# Repo Health frontend

React/Vite-фронтенд сервиса оценки здоровья репозиториев. В данный момент времени используется mock

## Запуск

```bash
corepack pnpm install
corepack pnpm dev
```

Проверки:

```bash
corepack pnpm exec tsc --noEmit
corepack pnpm run build
```

## Источник данных и соединение с бэкендом

Конфигурация будет находиться в `.env`(пока что все mock):

```bash
VITE_DATA_SOURCE=mock
VITE_API_BASE_URL=http://localhost:8080/api
VITE_USER_TOKEN=
```

Для подключения Gateway установите `VITE_DATA_SOURCE=http`. HTTP-адаптер вызывает
задокументированные маршруты:

- `GET /grade/{org_slug}/{repo_slug}`
- `GET /reccomendations/{org_slug}/{repo_slug}`

Заголовок `user_token` добавляется из `VITE_USER_TOKEN`. Написание
`reccomendations` оставлено в клиенте для совместимости с текущим контрактом;
клиент также принимает исправленное поле `recommendations` в JSON

Слои разделены так:

- `src/api/contracts.ts` — wire DTO из документации;
- `src/api/client.ts` — HTTP-клиент документированных Gateway маршрутов;
- `src/api/mapper.ts` — преобразование DTO в модель UI;
- `src/data/repositoryData.ts` — переключаемый mock/HTTP data source;
- `src/mocks/` — только локальные fixtures и скачивание отчета

Каталог репозиториев пока что остается mock-источником