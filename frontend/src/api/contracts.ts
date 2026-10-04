/**
 * Wire formats of the Gateway HTTP API (backend/gateway-service/internal/transport/http).
 *
 * These types describe what the backend sends, not what the components render.
 * `mapper.ts` converts them, so a backend naming change stays in one place.
 */

export type GradeStatus = "PENDING" | "FINISHED" | "NO_DATA"

type WirePriority = "CRITICAL" | "HIGH" | "MEDIUM" | "LOW" | "critical" | "high" | "medium" | "low"

export interface MetricPayload {
  id?: string
  no_data?: boolean
  raw_value?: number
  score?: number
}

export interface CategoryScorePayload {
  score?: number | null
  no_data?: boolean
  name?: string
  description?: string
  trend?: number | null
  metrics?: MetricPayload[]
  /** Documentation only: "llm" or "algorithmic" (simplified grade). */
  method?: string
}

export type CategoryScoreValue = number | null | CategoryScorePayload
export type CategoryScoresPayload =
  | Record<string, CategoryScoreValue>
  | Array<Record<string, CategoryScoreValue>>

/**
 * grade-service recommendation. `priority` is authoritative; during the transition `text` still
 * starts with the same "[priority]" tag, and older stored grades have only the tag.
 */
export interface AdvicePayload {
  category_id?: string
  metric_id?: string
  priority?: string
  text?: string
}

/**
 * `grade` is what Gateway sends today. The flat fields (`health_score`,
 * `category_scores`, `criteria`) are the older documented shape that the
 * mapper still accepts (docs/grading/frontend-results.md).
 */
export interface GradingResponse {
  status: GradeStatus
  grade?: {
    score?: number | null
    category_scores?: CategoryScoresPayload | null
    advice?: AdvicePayload[] | null
    missing_data?: string[]
    graded_at?: string
  }
  health_score?: number | null
  category_scores?: CategoryScoresPayload | null
  criteria?: CategoryScoresPayload | null
  missing_data?: Record<string, unknown> | string[]
  graded_at?: string
}

export interface RecommendationsResponse {
  status: GradeStatus
  /** Name of the LLM that produced the summary. */
  model_used?: string
  raw_response?: {
    summary?: string
    recommendations?: Array<{
      priority?: WirePriority
      category?: string
      place?: string
      location?: string
      text?: string
    }>
  } | null
}

/** GET /repos (requires the PAT header). */
export interface MyRepository {
  sourcecraft_uuid: string
  org_slug: string
  repo_slug: string
  web_url?: string
  visibility?: "public" | "internal" | "private"
  last_activity_at?: string
  rating?: number
  language?: { name?: string; color?: string }
}

export interface MyRepositoriesResponse {
  repositories: MyRepository[]
  next_page_token?: string
}

/** GET /analytics/{org}/{repo}: informational analytics (bus factor, ownership map). */
export interface AnalyticsResponse {
  status: GradeStatus
  /** null when the repo has none yet. */
  analytics: {
    version?: number
    window_days?: number
    bus_factor?: {
      status?: string
      value?: number
      commits?: number
      authors?: number
      top_authors?: Array<{ name?: string; commits?: number; share?: number }>
    }
    ownership?: {
      status?: string
      areas?: Array<{
        path?: string
        files?: number
        lines?: number
        top_owner?: string
        top_owner_share?: number
        owners?: number
      }>
      codeowners?: { present?: boolean; path?: string; rules?: number; covered_share?: number | null }
    }
  } | null
  graded_at?: string | null
}

/** A generic, pre-written reason a public grade is flagged low-confidence (it names no thresholds). */
export interface ConfidenceReasonPayload {
  code: string
  text: string
}

/** GET /score/{org}/{repo}: the stored public grade; read-only, never starts a grading. */
export interface PublicScoreResponse {
  status: GradeStatus
  score?: number | null
  graded_at?: string | null
  /** null/absent when unknown (grade stored before the flag existed). */
  low_confidence?: boolean | null
  confidence_reasons?: ConfidenceReasonPayload[]
}

export interface GatewayRankingEntry {
  place: number
  sourcecraft_uuid: string
  name: string
  web_url: string
  public_score?: number | null
  likes?: number
  language?: { name?: string; color?: string }
  last_activity_at?: string
  low_confidence?: boolean | null
  confidence_reasons?: ConfidenceReasonPayload[]
}

export interface GatewayRankingResponse {
  entries: GatewayRankingEntry[]
  total_count: number
  page: number
  page_size: number
}
