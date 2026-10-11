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
  /** When the public grade was computed; absent when unknown. */
  graded_at?: string
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

/** The filter of a batch grading (POST /admin/grading/preview and /admin/grading/batches). */
export interface BatchFilterBody {
  languages?: string[]
  min_score?: number
  max_score?: number
  older_than_days?: number
  repos?: string[]
  /** How many matching repositories are graded, highest SourceCraft rating first; required, at least 1. */
  limit: number
  /** Only repositories that already have a public grade; otherwise never-graded ones are graded too. */
  only_regrade?: boolean
  /** Grade even repositories with no SourceCraft activity since their last grade. */
  force?: boolean
}

/** POST /admin/grading/preview: the dry run. */
export interface BatchPreviewResponse {
  /** Public repositories the criteria match. */
  matched: number
  /** How many of them the limit takes. */
  count: number
  sample: string[]
  /** Hand-picked repositories that are not in the public repository list (reported only when the limit cut nothing). */
  unmatched: string[]
  estimate_seconds: number
  /** Estimated repositories the activity check will skip (0 with force). */
  inactive: number
}

export type BatchStatus = "running" | "paused" | "completed" | "cancelled"

/** One grading batch with its counters. `actor` is a hash of the starting admin's Yandex ID. */
export interface GradingBatchResponse {
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
    /** 0 for batches started before the limit existed. */
    limit: number
    only_regrade: boolean
    force: boolean
  }
  total: number
  pending: number
  running: number
  ok: number
  failed: number
  skipped: number
}

/** GET /admin/grading/batches/{id|current}. */
export interface GradingBatchDetailResponse {
  batch: GradingBatchResponse
  failures: { repo: string; error: string }[]
}

/** GET /admin/grading/batches/current: `batch` is null while there has never been a batch. */
export interface CurrentBatchResponse {
  batch: GradingBatchResponse | null
  failures: { repo: string; error: string }[]
}

export interface GradingBatchListResponse {
  batches: GradingBatchResponse[]
  /** The stand's cap on one batch's limit (BATCH_GRADING_MAX_LIMIT); 0 means none. */
  max_limit: number
}

export type CatalogRefreshStatus = "running" | "completed" | "failed" | "cancelled"

/** One refresh of the public repository catalog. `actor` is a hash of the admin's Yandex ID; empty for "startup". */
export interface CatalogRefreshResponse {
  id: number
  started_at: string
  finished_at: string | null
  status: CatalogRefreshStatus
  trigger: "admin" | "startup"
  actor: string
  pages: number
  /** Repositories fetched so far; once completed, repositories in the new catalog. */
  repos: number
  /** A fixed code for a failed refresh; empty otherwise. */
  error: string
}

/** GET /admin/catalog: the stored list of public repositories used by the scheduled and batch gradings. */
export interface CatalogStatusResponse {
  /** Repositories in the catalog in use; 0 before the first successful refresh. */
  size: number
  updated_at: string | null
  /** The running refresh, or the latest one; null before the first. */
  current: CatalogRefreshResponse | null
  /** The refresh that produced the catalog in use. */
  last_completed: CatalogRefreshResponse | null
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

/** Where a setting's running value comes from: environment > admin override > app.yaml > code default. */
export type SettingSource = "env" | "override" | "file" | "default"

/** One admin-editable key of config/app.yaml (GET /admin/settings). */
export interface SettingResponse {
  key: string
  group: string
  title: string
  type: "int" | "float" | "duration" | "bool" | "string" | "enum"
  min?: string
  max?: string
  values?: string[]
  /** The app.yaml value; null when the file leaves the key to the code default. */
  file_value: string | null
  /** The admin override; null when there is none. */
  override: string | null
  updated_at: string | null
  updated_by?: string
  /** Processes whose environment sets the key: the environment always wins, no override applies. */
  env_locked_by: string[]
  effective_from: SettingSource
}

/** A running process and its static (restart-only) keys changed since it started. */
export interface ConfigProcessResponse {
  name: string
  sections: string[]
  restart_pending: string[]
  reported_at: string | null
}

export interface SettingsResponse {
  settings: SettingResponse[]
  processes: ConfigProcessResponse[]
}

/** GET /config: the SPA's runtime settings (the `frontend` section of app.yaml). */
export interface PublicConfigResponse {
  public_url: string
  yandex_client_id: string
  dev_auth_bypass: boolean
  poll_timeout_ms: number
}

/** A public repository of the catalog matched by a search (GET /search/*). */
export interface CatalogRepoResponse {
  sourcecraft_uuid: string
  org_slug: string
  repo_slug: string
  description: string
  web_url: string
  language: { name: string; color: string }
  likes: number
  last_activity_at: string | null
  /** null while the repository has not been graded. */
  public_score: number | null
}

export interface CatalogOrgResponse {
  slug: string
  repo_count: number
}

/** GET /search/suggest: the top 3 repositories and the top organization. */
export interface SearchSuggestResponse {
  repos: CatalogRepoResponse[]
  orgs: CatalogOrgResponse[]
}

export interface SearchReposResponse {
  repos: CatalogRepoResponse[]
  total_count: number
  page: number
  page_size: number
}

export interface SearchOrgsResponse {
  orgs: CatalogOrgResponse[]
  total_count: number
  page: number
  page_size: number
}
