import { batchErrorMessage } from "../regradeView"
import { seriesErrorMessage } from "../seriesView"
import type {
  AdminMeResponse,
  AnalyticsResponse,
  AuditResponse,
  BatchFilterBody,
  BatchPreviewResponse,
  CurrentBatchResponse,
  GatewayRankingResponse,
  GradingResponse,
  HistoryResponse,
  MyOrganizationsResponse,
  MyRepositoriesResponse,
  PositionResponse,
  PublicScoreResponse,
  RecommendationsResponse,
  RegradeBatchDetailResponse,
  RegradeBatchListResponse,
  RegradeBatchResponse,
  RegradeStatusResponse,
  SeriesResponse,
  StatsResponse,
} from "./contracts"

export class ApiRequestError extends Error {
  readonly status: number
  /** A fixed reason word the Gateway sends with a refused admin batch request; empty otherwise. */
  code = ""

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

/** Header every state-changing request that trusts the session cookie carries (the Gateway's CSRF guard). */
const REQUEST_GUARD_HEADER = { "X-Admin-Request": "1" }

/** Wording of the password unlock's refusals; the Gateway answers 401, 403 and 429 with a Retry-After. */
function unlockError(response: Response): ApiRequestError | undefined {
  if (response.status === 401) return new ApiRequestError("Сначала войдите через Яндекс.", 401)
  if (response.status === 403) return new ApiRequestError("Неверный пароль.", 403)
  if (response.status === 429) {
    const minutes = Math.max(1, Math.ceil((Number(response.headers.get("Retry-After")) || 900) / 60))
    return new ApiRequestError(`Слишком много попыток. Повторите через ${minutes} мин.`, 429)
  }
  return undefined
}

/** The Gateway's reason word of a refused request, or "" - reading it must never change the error itself. */
async function reasonCode(response: Response): Promise<string> {
  try {
    const body = (await response.clone().json()) as { code?: unknown } | null
    return typeof body?.code === "string" ? body.code : ""
  } catch {
    return ""
  }
}

const ADMIN_DENIED_MESSAGE = "Нет доступа. Войдите через Яндекс под учётной записью администратора."

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

  /** Aggregate statistics of all graded public repositories; read-only, public grades only. */
  getStats(): Promise<StatsResponse> {
    return this.request("/stats")
  }

  /** Download link of the public grades export (format "csv" or "json"); served by the Gateway. */
  exportUrl(format: "csv" | "json"): string {
    return `${this.baseUrl}/export/grades.${format}`
  }

  /** Whether the signed-in user is an administrator; answered for visitors too (then false). */
  getAdminMe(): Promise<AdminMeResponse> {
    return this.request("/admin/me")
  }

  /**
   * Gives the signed-in account admin rights for the rest of its session when the password is right.
   * The password goes in the request body only. Resolves to the account's new admin status.
   */
  unlockAdmin(password: string): Promise<AdminMeResponse> {
    return this.request(
      "/admin/unlock",
      { method: "POST", headers: REQUEST_GUARD_HEADER, body: JSON.stringify({ password }) },
      unlockError,
    )
  }

  /**
   * Ends the Gateway session behind the cookie (and a password unlock held by it). Idempotent: a session
   * that is already gone is a success too.
   */
  async logout(): Promise<void> {
    await this.request("/auth/logout", { method: "POST", headers: REQUEST_GUARD_HEADER })
  }

  /** Takes the password unlock off the session; resolves to the account's new admin status. */
  lockAdmin(): Promise<AdminMeResponse> {
    return this.request("/admin/lock", { method: "POST", headers: REQUEST_GUARD_HEADER }, unlockError)
  }

  /** One page of the audit log, newest first; admins only. `beforeId` is the previous page's cursor. */
  async getAudit(filter: { outcome?: string; beforeId?: number; limit?: number } = {}): Promise<AuditResponse> {
    const query = new URLSearchParams()
    if (filter.limit) query.set("limit", String(filter.limit))
    if (filter.beforeId) query.set("before_id", String(filter.beforeId))
    if (filter.outcome) query.set("outcome", filter.outcome)
    const search = query.toString()
    try {
      return await this.request(search ? `/admin/audit?${search}` : "/admin/audit")
    } catch (error) {
      // The repository wording of statusMessage does not fit here.
      if (error instanceof ApiRequestError && (error.status === 401 || error.status === 403)) {
        throw new ApiRequestError(ADMIN_DENIED_MESSAGE, error.status)
      }
      throw error
    }
  }

  /** A graph of the Analytics tab (public gradings only); the range is fixed by the graph. */
  getPublicSeries(name: string): Promise<SeriesResponse> {
    return this.seriesRequest(`/stats/series/${encodeURIComponent(name)}`, "public")
  }

  /** A graph of the admin tab for one of the ranges "1h", "6h", "24h", "7d"; admins only. */
  getAdminSeries(name: string, range: string): Promise<SeriesResponse> {
    return this.seriesRequest(`/admin/metrics/${encodeURIComponent(name)}?range=${encodeURIComponent(range)}`, "admin")
  }

  private async seriesRequest(path: string, scope: "public" | "admin"): Promise<SeriesResponse> {
    try {
      return await this.request<SeriesResponse>(path)
    } catch (error) {
      if (error instanceof ApiRequestError && error.status !== 0) {
        const wording = new ApiRequestError(seriesErrorMessage(error.status, error.code, scope) ?? error.message, error.status)
        wording.code = error.code
        throw wording
      }
      throw error
    }
  }

  /** Dry run of a batch regrade: what the filter would regrade. Starts nothing; admins only. */
  previewBatch(filter: BatchFilterBody): Promise<BatchPreviewResponse> {
    return this.batchRequest("/admin/regrade/preview", { method: "POST", headers: REQUEST_GUARD_HEADER, body: JSON.stringify(filter) })
  }

  /** Starts a batch regrade for the filter. */
  startBatch(filter: BatchFilterBody): Promise<RegradeBatchResponse> {
    return this.batchRequest("/admin/regrade/batches", { method: "POST", headers: REQUEST_GUARD_HEADER, body: JSON.stringify(filter) })
  }

  /** Pauses, resumes or cancels a batch. */
  controlBatch(id: number, action: "pause" | "resume" | "cancel"): Promise<RegradeBatchResponse> {
    return this.batchRequest(`/admin/regrade/batches/${id}/${action}`, { method: "POST", headers: REQUEST_GUARD_HEADER })
  }

  /** A batch with its failed repos. */
  getBatch(id: number): Promise<RegradeBatchDetailResponse> {
    return this.batchRequest(`/admin/regrade/batches/${id}`)
  }

  /** The active batch, or the latest one; its `batch` is null on a stand that has never run one. */
  getCurrentBatch(): Promise<CurrentBatchResponse> {
    return this.batchRequest("/admin/regrade/batches/current")
  }

  /** The most recent batches, newest first. */
  listBatches(limit = 10): Promise<RegradeBatchListResponse> {
    return this.batchRequest(`/admin/regrade/batches?limit=${limit}`)
  }

  /** Batch routes answer with Russian wording: admin denial, or the Gateway's reason word. */
  private async batchRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
    try {
      return await this.request<T>(path, init)
    } catch (error) {
      if (error instanceof ApiRequestError && error.status !== 0) {
        const wording = new ApiRequestError(batchErrorMessage(error.status, error.code) ?? error.message, error.status)
        wording.code = error.code
        throw wording
      }
      throw error
    }
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

  private async request<T>(
    path: string,
    init: RequestInit = {},
    interpret?: (response: Response) => ApiRequestError | undefined,
  ): Promise<T> {
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
      if (!response.ok) {
        const failure = interpret?.(response) ?? new ApiRequestError(statusMessage(response.status), response.status)
        failure.code = await reasonCode(response)
        throw failure
      }
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
