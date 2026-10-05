import type {
  AnalyticsResponse,
  GatewayRankingResponse,
  GradingResponse,
  HistoryResponse,
  MyOrganizationsResponse,
  MyRepositoriesResponse,
  PositionResponse,
  PublicScoreResponse,
  RecommendationsResponse,
  RegradeStatusResponse,
} from "./contracts"

export class ApiRequestError extends Error {
  readonly status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = "ApiRequestError"
    this.status = status
  }
}

/** The service-wide limit on new gradings is reached (Gateway 429 "grading limit reached"). */
export class GradingLimitError extends ApiRequestError {
  readonly retryAfterSeconds: number

  constructor(retryAfterSeconds: number) {
    const minutes = Math.max(1, Math.ceil(retryAfterSeconds / 60))
    super(`Сервис занят: лимит новых оценок исчерпан. Попробуйте через ${minutes} мин.`, 429)
    this.name = "GradingLimitError"
    this.retryAfterSeconds = retryAfterSeconds
  }
}

const GRADING_LIMIT_ERROR = "grading limit reached"
const GRADING_INTERRUPTED_ERROR = "grading interrupted"
const GRADING_INTERRUPTED_MESSAGE =
  "Оценка прервана: сервис анализа перезапускался или завершился с ошибкой. Запустите анализ ещё раз."

export interface HttpRepoHealthApiOptions {
  baseUrl?: string
  fetcher?: typeof fetch
  timeoutMs?: number
}

/** Largest page rating-service accepts. */
export const RANKING_PAGE_SIZE = 100

export type RankingSort = "score" | "likes" | "last_activity"

/** Filters on repository facts (docs/ranking.md); unset means any. A repo whose fact is unknown never matches. */
export interface RankingFilters {
  /** By lines of code; the limits come from rating-service (RANKING_SIZE_SMALL_LOC / _LARGE_LOC). */
  size?: "small" | "medium" | "large"
  /** A CI config file exists (true) or does not (false). */
  ci?: boolean
  /** A SECURITY.md exists (true) or does not (false). */
  security?: boolean
  /** Text searched in the repository name and description. */
  topic?: string
}

function statusMessage(status: number): string {
  // One message for "no access" and "not found": AGENTS.md forbids telling them apart in the UI.
  if (status === 401 || status === 403 || status === 404) {
    return "Репозиторий не найден или нет доступа. Проверьте ссылку и токен SourceCraft."
  }
  if (status === 429) return "Слишком много запросов. Подождите минуту и повторите."
  return `Не удалось выполнить запрос (HTTP ${status}). Попробуйте ещё раз.`
}

/** The owner's SourceCraft PAT switches Gateway to private grading. */
function patHeader(pat?: string): Record<string, string> {
  const token = pat?.trim()
  return token ? { PAT: token } : {}
}

function repoPath(orgSlug: string, repoSlug: string) {
  return `${encodeURIComponent(orgSlug)}/${encodeURIComponent(repoSlug)}`
}

/** HTTP adapter for the Gateway routes (backend/gateway-service/internal/transport/http). */
export class HttpRepoHealthApi {
  private readonly baseUrl: string
  private readonly fetcher: typeof fetch
  private readonly timeoutMs: number

  constructor(options: HttpRepoHealthApiOptions = {}) {
    this.baseUrl = (options.baseUrl ?? "/api").replace(/\/$/, "")
    this.fetcher = options.fetcher ?? fetch.bind(globalThis)
    this.timeoutMs = options.timeoutMs ?? 15000
  }

  getGrading(orgSlug: string, repoSlug: string, pat?: string): Promise<GradingResponse> {
    return this.request(`/grading/${repoPath(orgSlug, repoSlug)}`, { headers: patHeader(pat) })
  }

  getRecommendations(orgSlug: string, repoSlug: string, pat?: string): Promise<RecommendationsResponse> {
    return this.request(`/recommendations/${repoPath(orgSlug, repoSlug)}`, { headers: patHeader(pat) })
  }

  /** One page of the public ranking; sorting and the language filter run on the server. */
  getRanking(
    page = 1,
    sort: RankingSort = "score",
    language = "",
    hideLowConfidence = false,
    filters: RankingFilters = {},
  ): Promise<GatewayRankingResponse> {
    const query = new URLSearchParams({ page: String(page), page_size: String(RANKING_PAGE_SIZE), sortby: sort })
    if (language) query.set("filter", language)
    if (hideLowConfidence) query.set("hide_low_confidence", "true")
    if (filters.size) query.set("size", filters.size)
    if (filters.ci !== undefined) query.set("ci", String(filters.ci))
    if (filters.security !== undefined) query.set("security", String(filters.security))
    const topic = filters.topic?.trim()
    if (topic) query.set("topic", topic)
    return this.request(`/ranking?${query}`)
  }

  /** The stored public grade with its low-confidence flag; read-only, never starts a grading. */
  getPublicScore(orgSlug: string, repoSlug: string): Promise<PublicScoreResponse> {
    return this.request(`/score/${repoPath(orgSlug, repoSlug)}`)
  }

  /** The scheduled public regrade: on or off, when it runs next, progress while it runs. */
  getRegradeStatus(): Promise<RegradeStatusResponse> {
    return this.request("/regrade-status")
  }

  /** Stored public score history, oldest first; read-only, never starts a grading. */
  getHistory(orgSlug: string, repoSlug: string): Promise<HistoryResponse> {
    return this.request(`/history/${repoPath(orgSlug, repoSlug)}`)
  }

  /** Why the repository has its place in the default ranking; read-only. */
  getPosition(orgSlug: string, repoSlug: string): Promise<PositionResponse> {
    return this.request(`/position/${repoPath(orgSlug, repoSlug)}`)
  }

  /** Stored bus factor and ownership map; read-only, never starts a grading. */
  getAnalytics(orgSlug: string, repoSlug: string, pat?: string): Promise<AnalyticsResponse> {
    return this.request(`/analytics/${repoPath(orgSlug, repoSlug)}`, { headers: patHeader(pat) })
  }

  /**
   * Repositories visible to the token. With orgSlug only that organization's repos are
   * returned, one batch at a time: pass next_page_token back as pageToken for the next batch.
   */
  getMyRepositories(sourceCraftPat: string, orgSlug?: string, pageToken?: string): Promise<MyRepositoriesResponse> {
    const headers = patHeader(sourceCraftPat)
    if (!headers.PAT) {
      return Promise.reject(new ApiRequestError("Введите SourceCraft PAT.", 401))
    }
    const query = new URLSearchParams()
    if (orgSlug) query.set("org", orgSlug)
    if (orgSlug && pageToken) query.set("page_token", pageToken)
    const search = query.toString()
    return this.request(search ? `/repos?${search}` : "/repos", { headers })
  }

  /** Organizations the token owner belongs to and the one to open first. */
  getMyOrganizations(sourceCraftPat: string): Promise<MyOrganizationsResponse> {
    const headers = patHeader(sourceCraftPat)
    if (!headers.PAT) {
      return Promise.reject(new ApiRequestError("Введите SourceCraft PAT.", 401))
    }
    return this.request("/orgs", { headers })
  }

  async loginWithYandex(oauthToken: string): Promise<void> {
    await this.request("/auth/yandex", {
      method: "POST",
      headers: { Authorization: `OAuth ${oauthToken}` },
    })
  }

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), this.timeoutMs)
    try {
      const response = await this.fetcher(`${this.baseUrl}${path}`, {
        ...init,
        signal: controller.signal,
        credentials: "include",
        headers: {
          Accept: "application/json",
          ...(init.body ? { "Content-Type": "application/json" } : {}),
          ...init.headers,
        },
      })
      if (response.status === 429 || response.status === 502) {
        const body = (await response.json().catch(() => null)) as { error?: unknown } | null
        if (body?.error === GRADING_LIMIT_ERROR) {
          throw new GradingLimitError(Number(response.headers.get("Retry-After")) || 60)
        }
        if (body?.error === GRADING_INTERRUPTED_ERROR) {
          throw new ApiRequestError(GRADING_INTERRUPTED_MESSAGE, response.status)
        }
      }
      if (!response.ok) throw new ApiRequestError(statusMessage(response.status), response.status)
      if (response.status === 204) return undefined as T
      return (await response.json()) as T
    } catch (error) {
      if (controller.signal.aborted) {
        throw new ApiRequestError("Сервер не ответил вовремя. Попробуйте ещё раз.", 0)
      }
      if (error instanceof ApiRequestError) throw error
      throw new ApiRequestError(
        "Не удалось получить ответ сервера. Проверьте подключение и повторите запрос.",
        0,
      )
    } finally {
      clearTimeout(timer)
    }
  }
}
