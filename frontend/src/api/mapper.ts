import type { MetricId, MetricResult, RepositorySummary } from "../types"
import type {
  CategoryScoresPayload,
  GradingResponse,
  RecommendationItemResponse,
  RecommendationsResponse,
  SourceCraftRepository,
} from "./contracts"

const metricAliases: Record<string, MetricId> = {
  activity: "activity",
  code: "codeHealth",
  code_health: "codeHealth",
  codehealth: "codeHealth",
  security: "security",
  appsec: "security",
  ci: "ciCd",
  cicd: "ciCd",
  ci_cd: "ciCd",
  documentation: "documentation",
  docs: "documentation",
  issues: "issues",
  community: "issues",
}

const defaultMetricIds: MetricId[] = [
  "activity",
  "codeHealth",
  "security",
  "ciCd",
  "documentation",
  "issues",
]

function emptyMetricResults(): Record<MetricId, MetricResult> {
  return Object.fromEntries(
    defaultMetricIds.map((metricId) => [metricId, { score: 0, trend: null }]),
  ) as Record<MetricId, MetricResult>
}

export function normalizeCategoryScores(
  payload?: CategoryScoresPayload,
): Record<MetricId, MetricResult> {
  const result = emptyMetricResults()
  if (!payload) return result

  const entries = Array.isArray(payload)
    ? payload.flatMap((item) => Object.entries(item))
    : Object.entries(payload)
  for (const [rawName, rawScore] of entries) {
    const metricId =
      metricAliases[rawName.trim().toLocaleLowerCase().replace(/\s+/g, "_")]
    if (!metricId || typeof rawScore !== "number" || !Number.isFinite(rawScore))
      continue
    result[metricId] = {
      score: Math.max(0, Math.min(100, Math.round(rawScore))),
      trend: null,
    }
  }
  return result
}

function parseCount(value?: string): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : 0
}

function getLikes(repository: SourceCraftRepository): number {
  const reactionCounts = repository.rating?.reaction_counts ?? []
  const likes = reactionCounts.find((reaction) =>
    ["like", "likes", "upvote", "positive"].includes(
      reaction.type?.toLocaleLowerCase() ?? "",
    ),
  )
  return parseCount(likes?.count)
}

export function repositoryFromSourceCraft(
  repository: SourceCraftRepository,
  grading?: GradingResponse,
): RepositorySummary {
  const categoryScores = normalizeCategoryScores(grading?.category_scores)
  const healthScore =
    typeof grading?.health_score === "number"
      ? Math.round(grading.health_score)
      : 0
  const lastUpdated = repository.last_updated
  const lastActivityDaysAgo = lastUpdated
    ? Math.max(
        0,
        Math.floor((Date.now() - new Date(lastUpdated).getTime()) / 86_400_000),
      )
    : 0

  return {
    id: repository.id,
    sourceCraftUuid: repository.id,
    orgSlug: repository.organization.slug,
    repoSlug: repository.slug,
    name: repository.name,
    fullName: `${repository.organization.slug}/${repository.slug}`,
    language: repository.language?.name ?? "Не указан",
    defaultBranch: repository.default_branch,
    visibility: repository.visibility,
    description: repository.description,
    webUrl: repository.web_url,
    lastUpdated,
    healthScore,
    trend: 0,
    likes: getLikes(repository),
    lastActivityDaysAgo,
    categoryScores,
    gradeStatus: grading?.status ?? "PENDING",
    gradedAt: grading?.graded_at,
    missingData: normalizeMissingData(grading?.missing_data),
  }
}

export function normalizeMissingData(
  missingData?: Record<string, unknown> | string[],
): string[] {
  if (Array.isArray(missingData)) return missingData.map(String)
  return Object.entries(missingData ?? {})
    .filter(([, value]) => Boolean(value))
    .map(([key]) => key)
}

export function recommendationsFromResponse(
  response: RecommendationsResponse,
): RecommendationItemResponse[] {
  return response.recommendations ?? response.reccomendations ?? []
}

export function mergeGradingIntoRepository(
  repository: RepositorySummary,
  grading: GradingResponse,
): RepositorySummary {
  const healthScore =
    typeof grading.health_score === "number"
      ? Math.round(grading.health_score)
      : repository.healthScore
  return {
    ...repository,
    healthScore,
    categoryScores: grading.category_scores
      ? normalizeCategoryScores(grading.category_scores)
      : repository.categoryScores,
    gradeStatus: grading.status,
    gradedAt: grading.graded_at ?? repository.gradedAt,
    missingData: grading.missing_data
      ? normalizeMissingData(grading.missing_data)
      : repository.missingData,
  }
}
