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
  /** Score now minus the score a week ago (the ranking's window); undefined for a repo without an older grade. */
  scoreChange?: number
  /** Places gained (positive) or lost (negative) in the ranking over the same window; only when sorted by score. */
  placeChange?: number
  /** Stored public gradings of the repository; the ranking marks it "new" while there is at most one. */
  gradeCount?: number
}

/** The scheduled public regrade as the ranking page shows it. */
export interface RegradeStatus {
  enabled: boolean
  /** ISO time of the next scheduled run; undefined while the schedule is off. */
  nextRunAt?: string
  running: boolean
  /** Repositories of the current run; total is 0 until the list is loaded. */
  total: number
  done: number
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

/** One frequently changed file and who changed it most. */
export interface OwnedFile {
  path: string
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

/** Review habits over the newest sampled pull requests; informational only. */
export interface ReviewStats {
  available: boolean
  /** Why it is unavailable: unavailable | few_merged | imported. */
  reason?: string
  sampled: number
  /** Merged pull requests with readable reviews that were judged. */
  merged: number
  /** Of those, merged with no review by anyone but the author (bots excluded). */
  unreviewed: number
  /** 0–1; undefined unless available. */
  unreviewedShare?: number
  /** Approximate: SourceCraft has no merge time, so this is updated_at - created_at. */
  medianMergeHours?: number
  /** Where reviews wait; its own availability, since it needs no merged pull requests. */
  bottlenecks: ReviewBottlenecks
}

export interface ReviewBottlenecks {
  available: boolean
  /** Why it is unavailable: unavailable | imported | few_reviewed. */
  reason?: string
  /** Pull requests with a review (the sample of the median). */
  reviewed: number
  /** Open pull requests judged, and how many of them have waited too long for a review. */
  open: number
  waiting: number
  /** The waiting threshold in days, as configured in grade-service. */
  waitingDays: number
  /** Median hours from creation to the first review by someone but the author or a bot. */
  medianFirstReviewHours?: number
}

/** How quickly the newest sampled issues get a first reply by someone but the author or a bot. */
export interface IssueRepliesStats {
  available: boolean
  /** Why it is unavailable: unavailable | few_issues | few_authors | imported. */
  reason?: string
  sampled: number
  replied: number
  unanswered: number
  /** 0-1; undefined unless available. */
  repliedShare?: number
  /** Median wait in hours; an unanswered issue counts as the time it waited, at most capDays. */
  medianFirstReplyHours?: number
  capDays: number
}

export interface RepoAnalytics {
  windowDays: number
  busFactor: BusFactor
  reviews: ReviewStats
  issueReplies: IssueRepliesStats
  ownership: {
    available: boolean
    /** Top-level directories; subareas are the busiest directories two levels deep, files the most changed files. */
    areas: OwnershipArea[]
    subareas: OwnershipArea[]
    files: OwnedFile[]
    codeowners: CodeownersInfo
  }
}

/** AI summary produced by llm-service (`raw_response`). */
export interface AiReport {
  status?: GradeStatus
  summary?: string
  /** LLM that produced the summary (`model_used`). */
  modelUsed?: string
  recommendations: Recommendation[]
}

export type PageId = "ranking" | "analytics" | "access" | "my" | "rules" | "search" | "overview"

/** One stored public grading; a null category score means that category had no data. */
export interface ScorePoint {
  gradedAt: string
  score: number
  categories: { id: string; score: number | null }[]
  lowConfidence: boolean | null
}

/** The neighbour of a repository in the ranking; points is the gap between the two scores. */
export interface PositionNeighbour {
  place: number
  org: string
  name: string
  score: number
  points: number
}

/** One category against the median of the top 10; median and gap are absent when too few top repos have it. */
export interface PositionCategory {
  id: string
  score: number
  top10Median?: number
  gap?: number
}

/** Why a repository has its place in the default ranking. */
export interface PositionExplanation {
  place: number
  total: number
  score: number
  above?: PositionNeighbour
  below?: PositionNeighbour
  categories: PositionCategory[]
  weakestCategory?: string
}

/** One side of the compare page: the public grade and history of a repository. */
export interface CompareSide {
  org: string
  repo: string
  state: "loading" | "ready" | "error"
  error?: string
  /** Display name and link, once the grade is known. */
  name?: string
  webUrl?: string
  score: number | null
  level?: string
  gradedAt?: string
  /** Null score = no data for that category. */
  categories: { id: string; score: number | null }[]
  confidence?: Confidence
  history: ScorePoint[]
}
