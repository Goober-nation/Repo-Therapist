export type PageId = "overview" | "ranking" | "security"

export interface MetricResult {
  score: number | null
  trend: number | null
  name?: string
  description?: string
}

export type CategoryScores = Record<string, MetricResult>

export interface RepositorySummary {
  id: string
  sourceCraftUuid?: string
  orgSlug?: string
  repoSlug?: string
  name: string
  fullName: string
  language: string
  defaultBranch?: string
  visibility?: "public" | "internal" | "private"
  description?: string
  webUrl?: string
  lastUpdated?: string
  healthScore: number
  trend: number
  likes: number
  lastActivityDaysAgo: number
  categoryScores: CategoryScores
  gradeStatus?: "PENDING" | "FINISHED"
  gradedAt?: string
  missingData?: string[]
}

export interface AttentionItem {
  id: string
  severity: "critical" | "warning"
  category: string
  title: string
  description: string
  impact: number
}

export interface Recommendation {
  id: string
  priority: "high" | "medium"
  title: string
  description: string
  currentScore: number
  projectedScore: number
}

export interface SecurityFinding {
  id: string
  severity: "critical" | "high"
  packageName: string
  cve: string
  installedVersion: string
  fixAvailable: boolean
}
