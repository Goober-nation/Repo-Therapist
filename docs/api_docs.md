> **Проверка актуальности (по коду, 2026-10-04).** Это ранний черновик API; реальные контракты
> с тех пор изменились. Источник истины — `.proto`-файлы сервисов и
> `backend/gateway-service/internal/transport/http/handler.go`. Расхождения:
>
> - **Обёртка SourceCraft** (`sourcecraft-wrapper-service/internal/transport/rpc/service.proto`,
>   сервис `CodeHealth`): `GetRepoIdByOrgRepoSlug` не существует — вместо него
>   `GetRepositoryBySlugs` и `GetRepoIDBySlugs`; `GetGitUrl` → `GetGitURL`;
>   `GetCiCdRuns` → `GetCiCDRuns`; добавлены `GetMyRepos`, `GetMyOrgs`
>   (`GET /me/orgs` + `GET /user`: организации пользователя и организация по умолчанию —
>   личная, чей slug совпадает с `username`, иначе первая) и `GetOrgRepos` (`GET
>   /orgs/{org_slug}/repos`: одна порция до 100 репозиториев; новые страницы запрашиваются не
>   дольше 5 с, остаток — через `next_page_token`; 404 → `NOT_FOUND`, 403 → `PERMISSION_DENIED`).
> - **Обёртка AppSec** (`security-wrapper-service`, сервис `SecurityWrapper`): `StartReport`,
>   `GetReportStatus` и `GetReportLink` не существуют; единственный RPC — `GetAppsecResult`.
> - **Агрегатор** (`aggregator-service/internal/transport/rpc/server.proto`): RPC — `GetMyRepos`,
>   `GetMyOrgs`, `GetOrgRepos` (проксируют одноимённые RPC обёртки с токеном пользователя),
>   `GetGrading`, `RequestLLMRecommendations` (не `GetReccomendations`), `GetAnalytics`
>   (запрос `{org_slug, repo_slug}`, ответ `{analytics: bytes (JSON), graded_at}`; только чтение).
> - **Рейтинг** (`rating-service/internal/transport/rpc/service.proto`, сервис `Rating`, только
>   чтение БД): `GetRanking`, `GetPublicScore`, `GetPublicSummary`; для неизвестного или не
>   публичного репозитория последние два отвечают `NOT_FOUND`. В разделах ниже не описаны.
> - **API Gateway**: реальные маршруты — `POST /auth/yandex`, `GET /orgs`, `GET /repos[?org=&page_token=]`,
>   `GET /grading/{org}/{repo}` (не `/grade/...`), `GET /recommendations/{org}/{repo}`,
>   `GET /ranking`, `GET /analytics/{org}/{repo}`, а также публичные `GET /score/{org}/{repo}`,
>   `GET /badge/{org}/{repo}[.svg|.json]`, `GET /badge/{org}/{repo}/{category}.svg`,
>   `GET /widget/{org}/{repo}.svg`. PAT передаётся заголовком `PAT` (не `user_token`).
> - JSON-примеры ниже местами синтаксически некорректны (пропущенные запятые, точки вместо
>   запятых) — это эскизы формы, а не точные схемы.

# Обёртка SourceCraft

rpc GetRepoIdByOrgRepoSlug(ByOrgRepoSlugRequest) returns (ByOrgRepoSlugResponse)
**Запрос**

```json
	{
	"repo_slug": "f-dds-",
	"org_slug": "121213fdff"
	}
```

**Ответ**

```json
	{
  "id": "string",
  "name": "string",
  "default_branch": "string",
  "organization": {
    "id": "string",
    "slug": "string"
  },
  "slug": "string",
  "template_type": "not_a_template",
  "is_empty": true,
  "description": "string",
  "visibility": "public",
  "logo": {
    "url": "string"
  },
  "clone_url": {
    "https": "string",
    "ssh": "string"
  },
  "web_url": "string",
  "links": [
    {
      "link": "string",
      "type": "default"
    }
  ],
  "counters": {
    "forks": "string",
    "pull_requests": "string",
    "issues": "string",
    "tags": "string",
    "branches": "string"
  },
  "last_updated": "2019-08-24T14:15:22Z",
  "language": {
    "name": "string",
    "color": "string"
  },
  "parent": {
    "id": "string",
    "slug": "string",
    "organization": {
      "id": "string",
      "slug": "string"
    }
  },
  "migration_source": {
    "url": "string",
    "domain": "string"
  },
  "project": {
    "id": "string",
    "slug": "string"
  },
  "rating": {
    "value": 0.1,
    "percentile": 0.1,
    "reaction_counts": [
      {
        "type": "none",
        "count": "string"
      }
    ]
  }
}
```

rpc GetGitUrl(GetGitUrlRequest) returns (GetGitUrlResponse)
**Запрос**

```json
	{
	"repo_id": "0od0fff-ddsddd-dkewe"
	}
```

**Ответ**

```json
	{
 "https_url" : "wewewewewe"
 "ssh_url" : "wewewddsdsd"
	}
```

rpc GetCiCdRuns(GetCiCdDataRequest) returns (GetCiCdRunsResponse)

**Запрос**

```json
	{
	"repo_id": "0od0fff-ddsddd-dkewe"
	}
```

**Ответ**

```json
{
  "runs": [
    {
      "id": "string",
      "slug": "string",
      "dates": {
        "created_at": "2019-08-24T14:15:22Z",
        "started_at": "2019-08-24T14:15:22Z",
        "finished_at": "2019-08-24T14:15:22Z",
        "updated_at": "2019-08-24T14:15:22Z"
      },
      "status": "created",
      "workflows": [
        {
          "id": "string",
          "slug": "string",
          "description": "string",
          "dates": {
            "created_at": "2019-08-24T14:15:22Z",
            "started_at": "2019-08-24T14:15:22Z",
            "finished_at": "2019-08-24T14:15:22Z",
            "updated_at": "2019-08-24T14:15:22Z"
          },
          "status": "created",
          "tasks": [
            {
              "id": "string",
              "slug": "string",
              "description": "string",
              "dates": {
                "created_at": "2019-08-24T14:15:22Z",
                "started_at": "2019-08-24T14:15:22Z",
                "finished_at": "2019-08-24T14:15:22Z",
                "updated_at": "2019-08-24T14:15:22Z"
              },
              "status": "created",
              "cubes": [
                {
                  "id": "string",
                  "slug": "string",
                  "dates": {},
                  "status": "created",
                  "artifacts": [
                    null
                  ],
                  "relations": {
                    "needs": []
                  }
                }
              ],
              "progress": {
                "percent": 0.1,
                "current_cube": {
                  "id": "string",
                  "slug": "string",
                  "dates": {},
                  "status": "created",
                  "artifacts": [
                    null
                  ],
                  "relations": {
                    "needs": []
                  }
                }
              },
              "relations": {
                "needs": [
                  {}
                ]
              }
            }
          ],
          "progress": {
            "percent": 0.1,
            "current_cube": {
              "id": "string",
              "slug": "string",
              "dates": {
                "created_at": "2019-08-24T14:15:22Z",
                "started_at": "2019-08-24T14:15:22Z",
                "finished_at": "2019-08-24T14:15:22Z",
                "updated_at": "2019-08-24T14:15:22Z"
              },
              "status": "created",
              "artifacts": [
                {
                  "id": "string",
                  "local_path": "string",
                  "dates": {},
                  "status": "registered",
                  "download_url": "string"
                }
              ],
              "relations": {
                "needs": [
                  {}
                ]
              }
            }
          },
          "environment_id": "string"
        }
      ],
      "event_type": "push",
      "error_messages": [
        "string"
      ],
      "pull": {
        "id": "string",
        "slug": "string"
      },
      "user": {
        "id": "string",
        "slug": "string"
      }
    }
  ],
  "next_page_token": "string"
}
```

rpc GetIssues(GetIssuesRequest) returns (GetIssuesResponse);
**Запрос**

```json
	{
	"repo_id": "0od0fff-ddsddd-dkewe"
	}
```

**Ответ**

```json
{
  "issues": [
    {
      "id": "string",
      "slug": "string",
      "title": "string",
      "description": "string",
      "status": {
        "id": "string",
        "slug": "string",
        "name": "string",
        "status_type": "initial"
      },
      "author": {
        "id": "string",
        "slug": "string"
      },
      "updated_by": {
        "id": "string",
        "slug": "string"
      },
      "created_at": "2019-08-24T14:15:22Z",
      "updated_at": "2019-08-24T14:15:22Z",
      "assignee": {
        "id": "string",
        "slug": "string"
      },
      "labels": [
        {
          "id": "string",
          "slug": "string",
          "name": "string",
          "color": "string"
        }
      ],
      "linked_prs": [
        {
          "id": "string",
          "slug": "string"
        }
      ],
      "priority": "trivial",
      "visibility": "public",
      "milestone": {
        "id": "string",
        "slug": "string"
      },
      "deadline": "2019-08-24T14:15:22Z",
      "started_at": "2019-08-24T14:15:22Z",
      "completed_at": "2019-08-24T14:15:22Z",
      "repository": {
        "id": "string",
        "slug": "string",
        "organization": {
          "id": "string",
          "slug": "string"
        }
      }
    }
  ],
  "next_page_token": "string"
}
```

rpc GetMilestones(GetMilestonesRequest) returns (GetMilestonesResponse);

**Запрос**

```json
	{
	"repo_id": "0od0fff-ddsddd-dkewe"
	}
```

**Ответ**

```json
{
  "items": [
    {
      "id": "string",
      "name": "string",
      "slug": "string",
      "description": "string",
      "start_date": "2019-08-24T14:15:22Z",
      "deadline": "2019-08-24T14:15:22Z",
      "status": "open",
      "author": {
        "id": "string",
        "slug": "string"
      },
      "updated_by": {
        "id": "string",
        "slug": "string"
      },
      "created_at": "2019-08-24T14:15:22Z",
      "updated_at": "2019-08-24T14:15:22Z"
    }
  ],
  "next_page_token": "string"
}
```

rpc ListAllPublicRepos(GetPublicReposRequest) returns (GetPublicReposResponse);
**Ответ**

```json
{
  "repositories": [
    {
      "id": "string",
      "name": "string",
      "default_branch": "string",
      "organization": {
        "id": "string",
        "slug": "string"
      },
      "slug": "string",
      "template_type": "not_a_template",
      "is_empty": true,
      "description": "string",
      "visibility": "public",
      "logo": {
        "url": "string"
      },
      "clone_url": {
        "https": "string",
        "ssh": "string"
      },
      "web_url": "string",
      "links": [
        {
          "link": "string",
          "type": "default"
        }
      ],
      "counters": {
        "forks": "string",
        "pull_requests": "string",
        "issues": "string",
        "tags": "string",
        "branches": "string"
      },
      "last_updated": "2019-08-24T14:15:22Z",
      "language": {
        "name": "string",
        "color": "string"
      },
      "parent": {
        "id": "string",
        "slug": "string",
        "organization": {
          "id": "string",
          "slug": "string"
        }
      },
      "migration_source": {
        "url": "string",
        "domain": "string"
      },
      "project": {
        "id": "string",
        "slug": "string"
      },
      "rating": {
        "value": 0.1,
        "percentile": 0.1,
        "reaction_counts": [
          {
            "type": "none",
            "count": "string"
          }
        ]
      }
    }
  ],
  "next_page_token": "string"
}
```

# 2\. Обёртка AppSec

rpc StartReport(StartReportRequest) returns (StartReportResponse);

**Запрос**

```json
	{
	"repo_id": "0od0fff-ddsddd-dkewe"
	}
```

**Ответ**

```json
{
  "operationUuid": "string",
  "status": "IN_PROGRESS",
  "done": true,
  "reportUuid": "string",
  "scanUuid": "string",
  "format": "string",
  "engineType": "string"
}
```

rpc GetReportStatus(GetReportStatusRequest) returns (GetReportStatusResponse);

**Запрос**

```json
	{
	"repo_id": "0od0fff-ddsddd-dkewe",
	"operationUuid": "string",
	}
```

**Ответ**

```json
{
  "operationUuid": "string",
  "status": "IN_PROGRESS",
  "done": true,
  "reportUuid": "string",
  "scanUuid": "string",
  "format": "string",
  "engineType": "string"
}
```

rpc GetReportLink(GetReportLinkRequest) returns (GetReportLinkResponse);

**Запрос**

```json
	{
	"repo_id": "0od0fff-ddsddd-dkewe",
	"scanUuid": "string",
	}
```

**Ответ**

```json
{
  "reportUuid": "string",
  "scanUuid": "string",
  "format": "string",
  "engineType": "string",
  "createdAt": 0,
  "presignedUrl": "string"
}
```

# Агрегатор

rpc GetGrading(GetGradingRequest) returns (GetGradingResponse);

**Запрос**

```json
	{
	"user_token": "fdlfdfodkof"
	"repo_slug": "deb1kk2",
	"org_slug": "lolkek67",
	}
```

**Ответ**

```json
{
  "repo_slug" : "sdsdd",
  "health_score" : 56,
  "category_scores" : [	
						{
							"category1": 50,
						},
				]
  "category_data" : {
			"data1": "string".
},
  "missing_data": {
			"data1": "string".
},
  "graded_at" : "timestamp",
  "status": "FINISHED"
}
```

**ИЛИ**
**Ответ**

```json
{
  "status": "PENDING"
}
```

rpc GetReccomendations(GetReccomendationsRequest) returns (GetReccomendationsResponse);

**Запрос**

```json
	{
	"user_token": "fdlfdfodkof"
	"repo_slug": "deb1kk2",
	"org_slug": "lolkek67",
	}
```

**Ответ**

```json
{
	"status" : "PENDING"
}
```

**ИЛИ**
**Ответ**

```json
{
  "status": "FINISHED",
  "reccomendations": [
	  {
	  "subject1" : "dsdsdsdd"
	  },
]
}
```

# Api Gateway

Актуальные HTTP-маршруты (`backend/gateway-service/internal/transport/http/handler.go`).
Фронтенд ходит к ним через `/api/*` (Vite proxy или nginx). Заголовок `PAT` необязателен:
с токеном владельца Gateway отдаёт приватную оценку (6 категорий), без него — публичную.

| Маршрут | Заголовки | Ответ |
|---|---|---|
| `POST /auth/yandex` | `Authorization: OAuth <token>` | `{session_id}` + cookie `session_id` |
| `GET /orgs` | `PAT` (обязателен) | `{organizations: [{slug, display_name, personal}], default_org_slug}` — организации владельца токена; `default_org_slug` — личная организация, иначе первая |
| `GET /repos?org={slug}&page_token=` | `PAT` (обязателен) | репозитории одной организации, видимые токену (в том числе закрытые), одной порцией того же формата, что ниже; непустой `next_page_token` передаётся обратно как `page_token` для следующей порции. Нет организации или нет доступа — `404`/`403`, интерфейс показывает одно общее сообщение |
| `GET /repos` | `PAT` (обязателен) | без `org` — прежний список по всем организациям (`GET /me/repos`, может не уложиться в таймаут для больших организаций): `{repositories: [{sourcecraft_uuid, org_slug, repo_slug, web_url, visibility, last_activity_at, rating, language: {name, color}}], next_page_token}` |
| `GET /grading/{org}/{repo}` | `PAT` (опционален) | `{status, grade: {sourcecraft_uuid, score, category_scores, advice, missing_data, graded_at}}` |
| `GET /recommendations/{org}/{repo}` | `PAT` (опционален) | `{status, raw_response: {summary, recommendations: [{priority, category, text}]}, model_used, evaluated_at}` |
| `GET /ranking?page=&page_size=&sortby=&filter=&hide_low_confidence=` | — | `{entries: [{place, sourcecraft_uuid, name, web_url, public_score, likes, language, last_activity_at, low_confidence, confidence_reasons}], total_count, page, page_size}` |
| `GET /analytics/{org}/{repo}` | `PAT` (опционален) | `{status, analytics, graded_at}`: справочные показатели (фактор автобуса, карта владельцев, ревью pull request); с токеном владельца — приватные |
| `GET /badge/{org}/{repo}.svg` | — | SVG-бейдж с сохранённой публичной оценкой (`image/svg+xml`, `Cache-Control: max-age=300`); оценку не запускает, лимит по IP не применяется; неизвестный или не оценённый репозиторий — серый `n/a` |
| `GET /badge/{org}/{repo}/{category}.svg` | — | SVG-бейдж одной публичной категории: `activity`, `issues`, `documentation`, `code_health`; категория без данных — серый `n/a`, любой другой ID (в том числе `security` и `ci_cd`) — `404` |
| `GET /badge/{org}/{repo}.json` | — | JSON для shields.io endpoint-бейджа: `{schemaVersion: 1, label, message, color}`; неизвестный или не оценённый репозиторий — `n/a` и `lightgrey` |
| `GET /widget/{org}/{repo}.svg` | — | SVG-карточка для README: оценка, уровень здоровья и четыре публичные категории (`image/svg+xml`, `Cache-Control: max-age=300`); без данных — серая карточка «Нет данных» |
| `GET /score/{org}/{repo}` | — | публичный JSON API: `{org, repo, name, web_url, status, score, level, categories: [{id, score, no_data}], missing_data, graded_at}`; только чтение |

`GET /ranking` принимает `hide_low_confidence=true|false` (по умолчанию `false`): при `true` из
списка и из `total_count` убираются репозитории с флагом `low_confidence: true`; порядок остальных
не меняется, репозитории без флага («неизвестно») не скрываются. Недопустимое значение — `400`.

Публичные маршруты `/ranking`, `/score`, `/badge` и `/widget` отдают `Access-Control-Allow-Origin: *`,
остальные — нет. Картинки и shields-JSON не ограничиваются по IP: `429` сломал бы изображение в
README. `GET /score/...` ограничен отдельно, `PUBLIC_API_RATE_LIMIT` запросов на IP за
`PUBLIC_API_RATE_LIMIT_WINDOW` (по умолчанию 300 в минуту, свой ключ в Redis; в docker-compose те же
переменные); при превышении — `429 {"error":"rate limit exceeded"}`.

`GET /score/{org}/{repo}` не запускает оценку, в отличие от `/grading`. Поля ответа:

- `status`: `FINISHED` — есть публичная оценка; `NO_DATA` — репозиторий публичный, но ещё не
  оценён (`score: null`, `categories: []`, `graded_at: null`). Неизвестный или не публичный
  репозиторий — `404`; приватная оценка здесь никогда не отдаётся.
- `level`: `excellent` (от 90), `healthy` (от 75), `attention` (от 60), `at_risk` (ниже 60),
  `no_data`. Границы совпадают с цветами бейджей и уровнями во фронтенде.
- `categories`: публичные категории по ID в алфавитном порядке. `no_data: true` и `score: null` —
  данных нет (это не плохая оценка); `score: 0` — реальный ноль.
- `missing_data`: категории публичной оценки без данных; `graded_at`: время оценки (RFC 3339).
- `low_confidence` / `confidence_reasons` (2026-10-04): флаг низкой достоверности публичной оценки.
  `true` — оценка опирается на мало данных, `confidence_reasons` — `[{code, text}]` с заготовленными
  общими причинами; `false` — флага нет, список пуст; `null` — неизвестно (оценка сохранена до
  появления флага; такой репозиторий никогда не считается низкодостоверным). Флаг не меняет
  `score`, `level` и `status`.
- Кэш: `Cache-Control: public, max-age=60`.

Встраивание в README на SourceCraft (подставьте адрес стенда и репозиторий):

```markdown
[![Repo Health](https://repo-therapist.alo.quest/api/badge/ORG/REPO.svg)](https://repo-therapist.alo.quest/repo/ORG/REPO)
[![Repo Health](https://repo-therapist.alo.quest/api/widget/ORG/REPO.svg)](https://repo-therapist.alo.quest/repo/ORG/REPO)
[![activity](https://repo-therapist.alo.quest/api/badge/ORG/REPO/activity.svg)](https://repo-therapist.alo.quest/repo/ORG/REPO)
```

`GET /analytics/{org}/{repo}` только читает: оценку не запускает. `status`: `FINISHED` (в `analytics`
объект версии 2 (версия 1 — без блока `reviews`), формат — `docs/grading/analytics.md`) или `NO_DATA` (`analytics: null`, у репозитория
ещё нет аналитики, например он оценён до её появления). Приватный репозиторий без токена владельца —
`403`, неизвестный — `404`. CORS не включён: ответ зависит от токена.

`status`: `PENDING` (оценка запущена и считается), `FINISHED`, `NO_DATA`. Запрос оценки сам
запускает её, если оценки нет, в репозитории была активность после прошлого анализа или у оценки
нет строки аналитики (`ANALYTICS_REGRADE_MISSING`, по умолчанию включено).

`category_scores` — объект по ID категории: `{id, no_data, score (0–100), metrics: [{id, no_data,
raw_value, score (0–1)}]}`; у `documentation` дополнительно может быть `method` (`"llm"` или
`"algorithmic"` — упрощённая оценка). `advice` — `[{category_id, metric_id, priority, text}]`,
`priority` — `critical|high|medium|low` (`config/recommendations.yaml`); текст без метки. У оценок,
сохранённых до 2026-10, поля нет, а текст начинается с метки `[priority]`.
