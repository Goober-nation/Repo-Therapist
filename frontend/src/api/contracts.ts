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

/** GET /orgs (requires the PAT header): organizations the token owner belongs to. */
export interface MyOrganization {
  slug: string
  display_name?: string
  /** The owner's own (personal) organization. */
  personal?: boolean
}

export interface MyOrganizationsResponse {
  organizations: MyOrganization[]
  default_org_slug?: string
}

interface AreaPayload {
  path?: string
  files?: number
  lines?: number
  top_owner?: string
  top_owner_share?: number
  owners?: number
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
    reviews?: {
      status?: string
      reason?: string
      sampled?: number
      merged?: number
      unreviewed?: number
      unreviewed_share?: number | null
      median_merge_hours?: number | null
      bottlenecks?: {
        status?: string
        reason?: string
        reviewed?: number
        open?: number
        waiting?: number
        waiting_days?: number
        median_first_review_hours?: number | null
      }
    }
    issue_replies?: {
      status?: string
      reason?: string
      sampled?: number
      replied?: number
      unanswered?: number
      replied_share?: number | null
      median_first_reply_hours?: number | null
      cap_days?: number
    }
    ownership?: {
      status?: string
      areas?: AreaPayload[]
      subareas?: AreaPayload[]
      files?: Array<{ path?: string; lines?: number; top_owner?: string; top_owner_share?: number; owners?: number }>
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
  /** Present on GET /score: the four public categories, a null score meaning no data. */
  name?: string
  web_url?: string
  level?: string
  categories?: { id: string; score?: number | null; no_data?: boolean }[]
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
  /** Null when the repo had no grade a window ago ("new"). */
  score_change?: number | null
  /** Null unless the list is sorted by score and the repo was graded a window ago. */
  place_change?: number | null
  /** How many public gradings of the repo are stored; a repo is "new" while this is at most one. */
  grade_count?: number
}

export interface GatewayRankingResponse {
  entries: GatewayRankingEntry[]
  total_count: number
  page: number
  page_size: number
  /** Window of score_change / place_change in days (RANKING_DYNAMICS_DAYS). */
  dynamics_days?: number
}

/** GET /regrade-status: the scheduled public regrade. Times are null while unknown or off. */
export interface RegradeStatusResponse {
  enabled: boolean
  interval_seconds: number
  next_run_at: string | null
  running: boolean
  started_at: string | null
  last_finished_at: string | null
  /** Repositories of the current or last run; total is 0 until the list is loaded. */
  total: number
  done: number
}

/** GET /history/{org}/{repo}: stored public gradings, oldest first. */
export interface HistoryResponse {
  status: GradeStatus
  points?: {
    graded_at: string
    score: number
    categories?: { id: string; score?: number | null; no_data?: boolean }[]
    low_confidence?: boolean | null
  }[]
}

/** GET /position/{org}/{repo}: why the repository has its place in the default ranking. */
export interface PositionResponse {
  status: GradeStatus
  place?: number
  total?: number
  score?: number
  above?: { place: number; org: string; name: string; score: number; points: number } | null
  below?: { place: number; org: string; name: string; score: number; points: number } | null
  categories?: { id: string; score: number; top10_median?: number | null; gap?: number | null }[]
  weakest_category?: string
}

/** GET /stats: aggregate statistics over all graded public repositories (public grades only). */
export interface StatsResponse {
  graded_repos: number
  avg_score: number
  median_score: number
  distribution: { min: number; max: number; repos: number }[]
  levels: { level: string; repos: number }[]
  languages: { name: string; color: string; repos: number; avg_score: number }[]
  categories: { id: string; avg_score: number; repos: number }[]
  top_advice: { category_id: string; metric_id: string; priority: string; repos: number }[]
  trend: { week_start: string | null; avg_score: number; repos: number }[]
  most_improved: { org: string; name: string; score: number; past_score: number; change: number }[]
  improved_days: number
  min_language_repos: number
  generated_at: string | null
}

/** GET /admin/me: whether the caller is an administrator. Always answered, for visitors too. */
export interface AdminMeResponse {
  is_admin: boolean
  /** The session cookie belongs to a signed-in account. */
  signed_in: boolean
  /** A password unlock is configured on this stand. */
  unlock_available: boolean
  /** The admin rights come from a password unlock of this session (and can be taken off). */
  via_password: boolean
}

/** One entry of the audit log (GET /admin/audit). `actor` is a hash of the Yandex ID, never the ID. */
export interface AuditEntryResponse {
  id: number
  at: string
  actor_kind: string
  actor: string
  action: string
  target_kind: string
  target: string
  outcome: string
  details: Record<string, unknown>
}

/** GET /admin/audit: one page, newest first; `next_before_id` is the cursor of the next page (0 = last). */
export interface AuditResponse {
  entries: AuditEntryResponse[]
  next_before_id: number
}

/** The filter of a batch regrade (POST /admin/regrade/preview and /admin/regrade/batches). */
export interface BatchFilterBody {
  languages?: string[]
  min_score?: number
  max_score?: number
  older_than_days?: number
  repos?: string[]
  /** The first N matching public repositories in SourceCraft rating order. */
  top_rated?: number
  all?: boolean
  /** Regrade even repositories with no SourceCraft activity since their last grade. */
  force?: boolean
}

/** POST /admin/regrade/preview: the dry run. */
export interface BatchPreviewResponse {
  count: number
  /** More repositories matched than one batch may hold; the batch cannot start. */
  capped: boolean
  sample: string[]
  /** Hand-picked repositories that are not public repositories known to the service. */
  unmatched: string[]
  estimate_seconds: number
  /** Estimated repositories the activity check will skip (0 with force). */
  inactive: number
}

export type BatchStatus = "running" | "paused" | "completed" | "cancelled"

/** One regrade batch with its counters. `actor` is a hash of the starting admin's Yandex ID. */
export interface RegradeBatchResponse {
  id: number
  created_at: string
  finished_at: string | null
  status: BatchStatus
  actor: string
  filter: {
    languages: string[] | null
    min_score: number | null
    max_score: number | null
    older_than_days: number | null
    repos: string[] | null
    top_rated: number | null
    all: boolean
    force: boolean
  }
  total: number
  pending: number
  running: number
  ok: number
  failed: number
  skipped: number
}

/** GET /admin/regrade/batches/{id|current}. */
export interface RegradeBatchDetailResponse {
  batch: RegradeBatchResponse
  failures: { repo: string; error: string }[]
}

/** GET /admin/regrade/batches/current: `batch` is null while there has never been a batch. */
export interface CurrentBatchResponse {
  batch: RegradeBatchResponse | null
  failures: { repo: string; error: string }[]
}

export interface RegradeBatchListResponse {
  batches: RegradeBatchResponse[]
}

/** One curve of a graph: `points` are [unix seconds, value]. `label` is "" when the query has no grouping. */
export interface SeriesLineResponse {
  label: string
  points: [number, number][]
}

/** GET /stats/series/{name} and GET /admin/metrics/{name}: one graph read from Prometheus. */
export interface SeriesResponse {
  name: string
  /** count | seconds | per_second. */
  unit: string
  step_seconds: number
  generated_at: string
  series: SeriesLineResponse[]
}
