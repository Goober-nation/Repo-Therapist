/**
 * Public contracts from docs/api_docs.md.
 *
 * These types intentionally describe the wire format instead of the fields
 * used by the visual components. Mapping happens in `mapper.ts`, so a backend
 * naming change is isolated from the rest of the app.
 */

export type GradeStatus = "PENDING" | "FINISHED"

export interface CategoryScorePayload {
  score?: number | null
  name?: string
  description?: string
  trend?: number | null
}

export type CategoryScoreValue = number | null | CategoryScorePayload
export type CategoryScoresPayload = Record<string, CategoryScoreValue> | Array<Record<string, CategoryScoreValue>>

export interface GradingResponse {
  repo_slug?: string
  health_score?: number
  category_scores?: CategoryScoresPayload | null
  criteria?: CategoryScoresPayload | null
  category_data?: Record<string, unknown>
  missing_data?: Record<string, unknown> | string[]
  graded_at?: string
  status: GradeStatus
}

export interface RecommendationItemResponse {
  subject1?: string
  title?: string
  description?: string
  priority?: "high" | "medium" | "low"
  current_score?: number
  projected_score?: number
}

export interface RecommendationsResponse {
  status: GradeStatus
  reccomendations?: RecommendationItemResponse[]
  recommendations?: RecommendationItemResponse[]
}

export interface SourceCraftOrganization {
  id: string
  slug: string
}

export interface SourceCraftRepository {
  id: string
  name: string
  default_branch: string
  organization: SourceCraftOrganization
  slug: string
  description?: string
  visibility: "public" | "internal" | "private"
  web_url?: string
  last_updated?: string
  language?: {
    name?: string
    color?: string
  }
  counters?: {
    forks?: string
    pull_requests?: string
    issues?: string
    tags?: string
    branches?: string
  }
  rating?: {
    value?: number
    percentile?: number
    reaction_counts?: Array<{
      type?: string
      count?: string
    }>
  }
}

export interface PublicRepositoriesResponse {
  repositories: SourceCraftRepository[]
  next_page_token?: string
}

export interface StartReportResponse {
  operationUuid: string
  status: string
  done: boolean
  reportUuid?: string
  scanUuid?: string
  format?: string
  engineType?: string
}

export interface ReportLinkResponse {
  reportUuid: string
  scanUuid: string
  format?: string
  engineType?: string
  createdAt?: number
  presignedUrl: string
}
