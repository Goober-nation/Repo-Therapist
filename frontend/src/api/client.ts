import type { GradingResponse, RecommendationsResponse } from "./contracts"

export interface RepoHealthApi {
  getGrading(
    orgSlug: string,
    repoSlug: string,
    userToken?: string,
  ): Promise<GradingResponse>
  getRecommendations(
    orgSlug: string,
    repoSlug: string,
    userToken?: string,
  ): Promise<RecommendationsResponse>
}

export class ApiRequestError extends Error {
  readonly status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = "ApiRequestError"
    this.status = status
  }
}

export interface HttpRepoHealthApiOptions {
  baseUrl?: string
  fetcher?: typeof fetch
}

/** HTTP adapter for the documented Gateway routes. */
export class HttpRepoHealthApi implements RepoHealthApi {
  private readonly baseUrl: string
  private readonly fetcher: typeof fetch

  constructor(options: HttpRepoHealthApiOptions = {}) {
    this.baseUrl = (
      options.baseUrl ??
      import.meta.env.VITE_API_BASE_URL ??
      "/api"
    ).replace(/\/$/, "")
    this.fetcher = options.fetcher ?? fetch
  }

  getGrading(
    orgSlug: string,
    repoSlug: string,
    userToken?: string,
  ): Promise<GradingResponse> {
    return this.request<GradingResponse>(
      `/grade/${encodeURIComponent(orgSlug)}/${encodeURIComponent(repoSlug)}`,
      userToken,
    )
  }

  getRecommendations(
    orgSlug: string,
    repoSlug: string,
    userToken?: string,
  ): Promise<RecommendationsResponse> {
    return this.request<RecommendationsResponse>(
      `/reccomendations/${encodeURIComponent(orgSlug)}/${encodeURIComponent(repoSlug)}`,
      userToken,
    )
  }

  private async request<T,>(
    path: string,
    userToken?: string,
    init: RequestInit = {},
  ): Promise<T> {
    const response = await this.fetcher(`${this.baseUrl}${path}`, {
      ...init,
      headers: {
        Accept: "application/json",
        ...(init.body ? { "Content-Type": "application/json" } : {}),
        ...(userToken ? { user_token: userToken } : {}),
        ...init.headers,
      },
    })

    if (!response.ok) {
      const message = await response.text().catch(() => "")
      throw new ApiRequestError(
        message || `API request failed with status ${response.status}`,
        response.status,
      )
    }
    return response.json() as Promise<T>
  }
}
