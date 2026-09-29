# Враппер для сурскрафта

rpc GetRepoIdByOrgRepoSlug(ByOrgRepoSlugRequest) returns (ByOrgRepoSlugResponse)
**Request**

```json
	{
	"repo_slug": "f-dds-",
	"org_slug": "121213fdff"
	}
```

**Response**

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
**Request**

```json
	{
	"repo_id": "0od0fff-ddsddd-dkewe"
	}
```

**Response**

```json
	{
 "https_url" : "wewewewewe"
 "ssh_url" : "wewewddsdsd"
	}
```

rpc GetCiCdRuns(GetCiCdDataRequest) returns (GetCiCdRunsResponse)

**Request**

```json
	{
	"repo_id": "0od0fff-ddsddd-dkewe"
	}
```

**Response**

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
**Request**

```json
	{
	"repo_id": "0od0fff-ddsddd-dkewe"
	}
```

**Response**

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

**Request**

```json
	{
	"repo_id": "0od0fff-ddsddd-dkewe"
	}
```

**Response**

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
**Response**

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

# 2\. Враппер для аппсека

rpc StartReport(StartReportRequest) returns (StartReportResponse);

**Request**

```json
	{
	"repo_id": "0od0fff-ddsddd-dkewe"
	}
```

**Response**

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

**Request**

```json
	{
	"repo_id": "0od0fff-ddsddd-dkewe",
	"operationUuid": "string",
	}
```

**Response**

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

**Request**

```json
	{
	"repo_id": "0od0fff-ddsddd-dkewe",
	"scanUuid": "string",
	}
```

**Response**

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

# Аггрегатор

rpc GetGrading(GetGradingRequest) returns (GetGradingResponse);

**Request**

```json
	{
	"user_token": "fdlfdfodkof"
	"repo_slug": "deb1kk2",
	"org_slug": "lolkek67",
	}
```

**Response**

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
**Response**

```json
{
  "status": "PENDING"
}
```

rpc GetReccomendations(GetReccomendationsRequest) returns (GetReccomendationsResponse);

**Request**

```json
	{
	"user_token": "fdlfdfodkof"
	"repo_slug": "deb1kk2",
	"org_slug": "lolkek67",
	}
```

**Response**

```json
{
	"status" : "PENDING"
}
```

**ИЛИ**
**Response**

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
| `GET /repos` | `PAT` (обязателен) | `{repositories: [{sourcecraft_uuid, org_slug, repo_slug, web_url, visibility, last_activity_at, rating, language: {name, color}}], next_page_token}` |
| `GET /grading/{org}/{repo}` | `PAT` (опционален) | `{status, grade: {sourcecraft_uuid, score, category_scores, advice, missing_data, graded_at}}` |
| `GET /recommendations/{org}/{repo}` | `PAT` (опционален) | `{status, raw_response: {summary, strengths, weaknesses, recommendations: [{priority, category, text}]}, model_used, evaluated_at}` |
| `GET /ranking?page=&page_size=&sortby=&filter=` | — | `{entries: [{place, sourcecraft_uuid, name, web_url, public_score, likes, language, last_activity_at}], total_count, page, page_size}` |
| `GET /badge/{org}/{repo}.svg` | — | SVG-бейдж с сохранённой публичной оценкой (`image/svg+xml`, `Cache-Control: max-age=300`); оценку не запускает, лимит по IP не применяется; неизвестный или не оценённый репозиторий — серый `n/a` |

`status`: `PENDING` (оценка запущена и считается), `FINISHED`, `NO_DATA`. Запрос оценки сам
запускает её, если оценки нет или в репозитории была активность после прошлого анализа.

`category_scores` — объект по ID категории: `{id, no_data, score (0–100), metrics: [{id, no_data,
raw_value, score (0–1)}]}`. `advice` — `[{category_id, metric_id, text}]`, текст начинается с тега
приоритета `[critical|high|medium|low]` (`config/recommendations.yaml`).
