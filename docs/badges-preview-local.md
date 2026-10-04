# Проверка бейджей и виджета: локальный стенд

Та же проверка, что в [badges-preview.md](badges-preview.md), но все картинки и ссылки ведут на
локальный стек (`http://localhost:8081`), поэтому она не зависит от развёрнутого стенда и работает с
текущим кодом.

Как пользоваться:

1. Поднимите стек: `docker compose up -d --build` (фронтенд на `http://localhost:8081`, gateway
   доступен через его `/api/`).
2. Убедитесь, что у мок-репозитория есть оценка (откройте
   <http://localhost:8081/repo/timoxa102928/health-mock-strong>, пока оценка считается, картинки
   показывают серый `n/a`; также см. [testing.md](testing.md)).
3. Откройте эту страницу в просмотрщике Markdown **на этой же машине** (предпросмотр редактора или
   браузер). На GitHub и SourceCraft эти картинки не загрузятся: они не могут достучаться до
   `localhost`.

Что ожидать:

- публичный репозиторий с оценкой — цветной бейдж с числом (зелёный от 75, жёлтый от 60, красный
  ниже);
- категория без данных — серый `n/a`, а не красный `0`;
- репозиторий без публичной оценки (не оценён, `internal`/приватный, неизвестный или отсутствующий в
  локальной базе) — серый `n/a` на всех картинках; `/score` при этом отвечает `404` или `NO_DATA`.

## 1. Мок-репозиторий `timoxa102928/health-mock-strong`

Публичный репозиторий с оценкой. У категории Issues данных нет (все задачи закрыты через секунды
после создания и не учитываются), поэтому её бейдж должен быть серым `n/a`, а не красным `0`.

### Общий бейдж

[![Repo Health](http://localhost:8081/api/badge/timoxa102928/health-mock-strong.svg)](http://localhost:8081/repo/timoxa102928/health-mock-strong)

### Виджет

[![Repo Health](http://localhost:8081/api/widget/timoxa102928/health-mock-strong.svg)](http://localhost:8081/repo/timoxa102928/health-mock-strong)

### Бейджи категорий

| Категория | Бейдж |
|---|---|
| Activity | [![Repo Health: activity](http://localhost:8081/api/badge/timoxa102928/health-mock-strong/activity.svg)](http://localhost:8081/repo/timoxa102928/health-mock-strong) |
| Issues (ожидается `n/a`) | [![Repo Health: issues](http://localhost:8081/api/badge/timoxa102928/health-mock-strong/issues.svg)](http://localhost:8081/repo/timoxa102928/health-mock-strong) |
| Documentation | [![Repo Health: documentation](http://localhost:8081/api/badge/timoxa102928/health-mock-strong/documentation.svg)](http://localhost:8081/repo/timoxa102928/health-mock-strong) |
| Code health | [![Repo Health: code_health](http://localhost:8081/api/badge/timoxa102928/health-mock-strong/code_health.svg)](http://localhost:8081/repo/timoxa102928/health-mock-strong) |

### Бейдж через shields.io

shields.io — внешний сервис и до `localhost` не достучится, поэтому здесь проверяется только сам
JSON, который он прочитал бы:

- Shields-JSON: <http://localhost:8081/api/badge/timoxa102928/health-mock-strong.json>
  (ожидается `{"schemaVersion":1,"label":"repo health","message":"<оценка>","color":"..."}`)

### JSON и страница анализа

- Оценка в JSON: <http://localhost:8081/api/score/timoxa102928/health-mock-strong>
- Страница анализа: <http://localhost:8081/repo/timoxa102928/health-mock-strong>

## 2. Основной репозиторий `lct-hackaton-2026/case-18-repo-health-score-team-95`

В локальной базе у основного репозитория нет публичной оценки (он `internal` на SourceCraft и не
оценивался локально), поэтому все картинки ниже должны быть серыми `n/a`, а `/score` и shields-JSON
отвечают без числа. Это проверка поведения «нет данных».

### Общий бейдж

[![Repo Health](http://localhost:8081/api/badge/lct-hackaton-2026/case-18-repo-health-score-team-95.svg)](http://localhost:8081/repo/lct-hackaton-2026/case-18-repo-health-score-team-95)

### Виджет

[![Repo Health](http://localhost:8081/api/widget/lct-hackaton-2026/case-18-repo-health-score-team-95.svg)](http://localhost:8081/repo/lct-hackaton-2026/case-18-repo-health-score-team-95)

### Бейджи категорий

| Категория | Бейдж |
|---|---|
| Activity | [![Repo Health: activity](http://localhost:8081/api/badge/lct-hackaton-2026/case-18-repo-health-score-team-95/activity.svg)](http://localhost:8081/repo/lct-hackaton-2026/case-18-repo-health-score-team-95) |
| Issues | [![Repo Health: issues](http://localhost:8081/api/badge/lct-hackaton-2026/case-18-repo-health-score-team-95/issues.svg)](http://localhost:8081/repo/lct-hackaton-2026/case-18-repo-health-score-team-95) |
| Documentation | [![Repo Health: documentation](http://localhost:8081/api/badge/lct-hackaton-2026/case-18-repo-health-score-team-95/documentation.svg)](http://localhost:8081/repo/lct-hackaton-2026/case-18-repo-health-score-team-95) |
| Code health | [![Repo Health: code_health](http://localhost:8081/api/badge/lct-hackaton-2026/case-18-repo-health-score-team-95/code_health.svg)](http://localhost:8081/repo/lct-hackaton-2026/case-18-repo-health-score-team-95) |

### JSON и страница анализа

- Оценка в JSON (ожидается `404`): <http://localhost:8081/api/score/lct-hackaton-2026/case-18-repo-health-score-team-95>
- Shields-JSON (ожидается `n/a`): <http://localhost:8081/api/badge/lct-hackaton-2026/case-18-repo-health-score-team-95.json>
- Страница анализа: <http://localhost:8081/repo/lct-hackaton-2026/case-18-repo-health-score-team-95>

## 3. Заведомо неизвестный репозиторий

Проверка, что картинка не «ломается»: бейдж и виджет неизвестного репозитория остаются серыми `n/a`.

[![Repo Health](http://localhost:8081/api/badge/no-such-org/no-such-repo.svg)](http://localhost:8081/repo/no-such-org/no-such-repo)
[![Repo Health](http://localhost:8081/api/widget/no-such-org/no-such-repo.svg)](http://localhost:8081/repo/no-such-org/no-such-repo)
