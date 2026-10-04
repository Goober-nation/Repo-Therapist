export type GradeStatus = "PENDING" | "FINISHED" | "NO_DATA"

export type Priority = "critical" | "high" | "medium" | "low"

/**
 * A measured input behind a category score. `value` is the raw measurement and `score` the
 * metric's own grade (0–1) from grade-service; both are null when the metric had no data.
 */
export interface MetricFact {
  id: string
  value: number | null
  score: number | null
}

/** How the Documentation grade was computed (repo.docs.graded `method`). */
export type GradeMethod = "llm" | "algorithmic"

export interface MetricResult {
  score: number | null
  trend: number | null
  name?: string
  description?: string
  facts?: MetricFact[]
  /** Set only for Documentation; "algorithmic" is the simplified grade. */
  method?: GradeMethod
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

/**
 * Low-confidence flag of a public grade: the score rests on little data. It never changes the
 * score or the ranking order; the reasons are generic pre-written texts.
 */
export interface Confidence {
  low: boolean
  reasons: string[]
}

export interface RepositorySummary {
  id: string
  sourceCraftUuid?: string
  orgSlug?: string
  repoSlug?: string
  name: string
  fullName: string
  language: string
  /** Language color from SourceCraft (a validated hex value), shown as a dot next to the name. */
  languageColor?: string
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
  /** Public grades only; undefined when unknown. */
  confidence?: Confidence
}

/** One author's share of the commits in the analytics window. */
export interface AuthorShare {
  name: string
  commits: number
  /** 0–1. */
  share: number
}

/** Smallest group of authors with at least half of the commits; informational, never scored. */
export interface BusFactor {
  /** false = not enough commits to say (a "нет данных" state, not a bad one). */
  available: boolean
  value: number
  commits: number
  authors: number
  topAuthors: AuthorShare[]
}

/** A top-level directory and who changed it. */
export interface OwnershipArea {
  path: string
  files: number
  lines: number
  topOwner: string
  /** 0–1. */
  topOwnerShare: number
  owners: number
}

export interface CodeownersInfo {
  present: boolean
  path?: string
  rules: number
  /** 0–1; undefined when there is no file or nothing to compare with. */
  coveredShare?: number
}

export interface RepoAnalytics {
  windowDays: number
  busFactor: BusFactor
  ownership: { available: boolean; areas: OwnershipArea[]; codeowners: CodeownersInfo }
}

/** AI summary produced by llm-service (`raw_response`). */
export interface AiReport {
  status?: GradeStatus
  summary?: string
  /** LLM that produced the summary (`model_used`). */
  modelUsed?: string
  recommendations: Recommendation[]
}

export type PageId = "ranking" | "my" | "rules" | "overview"
