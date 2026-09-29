import type {
  GatewayRankingResponse,
  GradingResponse,
  MyRepositoriesResponse,
  RecommendationsResponse,
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
  getRanking(page = 1, sort: RankingSort = "score", language = ""): Promise<GatewayRankingResponse> {
    const query = new URLSearchParams({ page: String(page), page_size: String(RANKING_PAGE_SIZE), sortby: sort })
    if (language) query.set("filter", language)
    return this.request(`/ranking?${query}`)
  }

  getMyRepositories(sourceCraftPat: string): Promise<MyRepositoriesResponse> {
    const headers = patHeader(sourceCraftPat)
    if (!headers.PAT) {
      return Promise.reject(new ApiRequestError("Введите SourceCraft PAT.", 401))
    }
    return this.request("/repos", { headers })
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
