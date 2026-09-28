export type GradeStatus = "PENDING" | "FINISHED" | "NO_DATA"

export type Priority = "high" | "medium" | "low"

/**
 * A measured input behind a category score. `value` is the raw measurement and `score` the
 * metric's own grade (0–1) from grade-service; both are null when the metric had no data.
 */
export interface MetricFact {
  id: string
  value: number | null
  score: number | null
}

export interface MetricResult {
  score: number | null
  trend: number | null
  name?: string
  description?: string
  facts?: MetricFact[]
}

export type CategoryScores = Record<string, MetricResult>

export interface Recommendation {
  id: string
  priority: Priority
  title: string
  description: string
  category?: string
  location?: string
}

export interface RepositorySummary {
  id: string
  sourceCraftUuid?: string
  orgSlug?: string
  repoSlug?: string
  name: string
  fullName: string
  language: string
  /** Position in the public ranking, as reported by the backend. */
  place?: number
  visibility?: "public" | "internal" | "private"
  /** "private" = graded with the owner's SourceCraft token (all six categories). */
  analysisMode?: "public" | "private"
  webUrl?: string
  lastUpdated?: string
  healthScore: number | null
  likes?: number
  lastActivityDaysAgo: number
  categoryScores: CategoryScores
  gradeStatus?: GradeStatus
  gradedAt?: string
  missingData?: string[]
  /** Rule-based recommendations computed by grade-service (`grade.advice`). */
  advice?: Recommendation[]
}

/** AI summary produced by llm-service (`raw_response`). */
export interface AiReport {
  status?: GradeStatus
  summary?: string
  strengths: string[]
  weaknesses: string[]
  recommendations: Recommendation[]
}

export type PageId = "ranking" | "my" | "overview"
