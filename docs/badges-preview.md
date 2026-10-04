# Проверка бейджей и виджета

Страница для ручной проверки: как бейджи, категорийные бейджи и виджет выглядят в README на разных
площадках (SourceCraft, GitHub, локальный просмотр Markdown). Все картинки берутся со стенда
`https://repo-therapist.alo.quest/api/`. Маршруты описаны в [api_docs.md](api_docs.md#api-gateway).

> Категорийные бейджи (`/badge/{org}/{repo}/{category}.svg`), виджет (`/widget/...`),
> shields-JSON и `/score` появились позже общего бейджа. Пока стенд не обновлён, они отвечают
> `404` и картинка не загружается: это проверка развёртывания, а не ошибка разметки.

Что ожидать:

- у публичного репозитория с оценкой — цветной бейдж с числом (зелёный от 75, жёлтый от 60,
  красный ниже);
- категория без данных — серый `n/a`, а не красный `0`;
- у репозитория без публичной оценки (не оценён, `internal`/приватный или неизвестный) — серый
  `n/a` на всех картинках; JSON-маршруты при этом отвечают `404` или `NO_DATA`.

## 1. Мок-репозиторий `timoxa102928/health-mock-strong`

Публичный репозиторий с оценкой. После обновления стенда и повторной оценки у категории Issues
данных нет (все задачи закрыты через секунды после создания и не учитываются), поэтому её бейдж
должен быть серым `n/a`, а не красным `0`.

### Общий бейдж

[![Repo Health](https://repo-therapist.alo.quest/api/badge/timoxa102928/health-mock-strong.svg)](https://repo-therapist.alo.quest/repo/timoxa102928/health-mock-strong)

### Виджет

[![Repo Health](https://repo-therapist.alo.quest/api/widget/timoxa102928/health-mock-strong.svg)](https://repo-therapist.alo.quest/repo/timoxa102928/health-mock-strong)

### Бейджи категорий

| Категория | Бейдж |
|---|---|
| Activity | [![Repo Health: activity](https://repo-therapist.alo.quest/api/badge/timoxa102928/health-mock-strong/activity.svg)](https://repo-therapist.alo.quest/repo/timoxa102928/health-mock-strong) |
| Issues (ожидается `n/a`) | [![Repo Health: issues](https://repo-therapist.alo.quest/api/badge/timoxa102928/health-mock-strong/issues.svg)](https://repo-therapist.alo.quest/repo/timoxa102928/health-mock-strong) |
| Documentation | [![Repo Health: documentation](https://repo-therapist.alo.quest/api/badge/timoxa102928/health-mock-strong/documentation.svg)](https://repo-therapist.alo.quest/repo/timoxa102928/health-mock-strong) |
| Code health | [![Repo Health: code_health](https://repo-therapist.alo.quest/api/badge/timoxa102928/health-mock-strong/code_health.svg)](https://repo-therapist.alo.quest/repo/timoxa102928/health-mock-strong) |

### Бейдж через shields.io

[![Repo Health](https://img.shields.io/endpoint?url=https%3A%2F%2Frepo-therapist.alo.quest%2Fapi%2Fbadge%2Ftimoxa102928%2Fhealth-mock-strong.json)](https://repo-therapist.alo.quest/repo/timoxa102928/health-mock-strong)

### JSON и страница анализа

- Оценка в JSON: <https://repo-therapist.alo.quest/api/score/timoxa102928/health-mock-strong>
- Shields-JSON: <https://repo-therapist.alo.quest/api/badge/timoxa102928/health-mock-strong.json>
- Страница анализа: <https://repo-therapist.alo.quest/repo/timoxa102928/health-mock-strong>

## 2. Основной репозиторий `lct-hackaton-2026/case-18-repo-health-score-team-95`

Репозиторий проекта на SourceCraft. Пока его видимость `internal`, публичной оценки у него нет, и
все картинки ниже должны быть серыми `n/a` (а `/score` отвечает `404`). Это проверка поведения
«нет данных»: после перевода репозитория в публичный и первой оценки здесь появятся числа.

### Общий бейдж

[![Repo Health](https://repo-therapist.alo.quest/api/badge/lct-hackaton-2026/case-18-repo-health-score-team-95.svg)](https://repo-therapist.alo.quest/repo/lct-hackaton-2026/case-18-repo-health-score-team-95)

### Виджет

[![Repo Health](https://repo-therapist.alo.quest/api/widget/lct-hackaton-2026/case-18-repo-health-score-team-95.svg)](https://repo-therapist.alo.quest/repo/lct-hackaton-2026/case-18-repo-health-score-team-95)

### Бейджи категорий

| Категория | Бейдж |
|---|---|
| Activity | [![Repo Health: activity](https://repo-therapist.alo.quest/api/badge/lct-hackaton-2026/case-18-repo-health-score-team-95/activity.svg)](https://repo-therapist.alo.quest/repo/lct-hackaton-2026/case-18-repo-health-score-team-95) |
| Issues | [![Repo Health: issues](https://repo-therapist.alo.quest/api/badge/lct-hackaton-2026/case-18-repo-health-score-team-95/issues.svg)](https://repo-therapist.alo.quest/repo/lct-hackaton-2026/case-18-repo-health-score-team-95) |
| Documentation | [![Repo Health: documentation](https://repo-therapist.alo.quest/api/badge/lct-hackaton-2026/case-18-repo-health-score-team-95/documentation.svg)](https://repo-therapist.alo.quest/repo/lct-hackaton-2026/case-18-repo-health-score-team-95) |
| Code health | [![Repo Health: code_health](https://repo-therapist.alo.quest/api/badge/lct-hackaton-2026/case-18-repo-health-score-team-95/code_health.svg)](https://repo-therapist.alo.quest/repo/lct-hackaton-2026/case-18-repo-health-score-team-95) |

### Бейдж через shields.io

[![Repo Health](https://img.shields.io/endpoint?url=https%3A%2F%2Frepo-therapist.alo.quest%2Fapi%2Fbadge%2Flct-hackaton-2026%2Fcase-18-repo-health-score-team-95.json)](https://repo-therapist.alo.quest/repo/lct-hackaton-2026/case-18-repo-health-score-team-95)

### JSON и страница анализа

- Оценка в JSON: <https://repo-therapist.alo.quest/api/score/lct-hackaton-2026/case-18-repo-health-score-team-95>
- Shields-JSON: <https://repo-therapist.alo.quest/api/badge/lct-hackaton-2026/case-18-repo-health-score-team-95.json>
- Страница анализа: <https://repo-therapist.alo.quest/repo/lct-hackaton-2026/case-18-repo-health-score-team-95>

## 3. Заведомо неизвестный репозиторий

Проверка, что картинка не «ломается»: бейдж и виджет неизвестного репозитория остаются серыми `n/a`.

[![Repo Health](https://repo-therapist.alo.quest/api/badge/no-such-org/no-such-repo.svg)](https://repo-therapist.alo.quest/repo/no-such-org/no-such-repo)
[![Repo Health](https://repo-therapist.alo.quest/api/widget/no-such-org/no-such-repo.svg)](https://repo-therapist.alo.quest/repo/no-such-org/no-such-repo)
