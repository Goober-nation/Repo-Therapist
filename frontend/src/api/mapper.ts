import type { CategoryScores, MetricResult, RepositorySummary } from "../types"
import type {
  GradingResponse,
  RecommendationItemResponse,
  RecommendationsResponse,
  SourceCraftRepository,
} from "./contracts"

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

function optionalText(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value : undefined
}

export function normalizeCategoryScores(payload?: unknown): CategoryScores {
  const groups = Array.isArray(payload) ? payload : [payload]
  const entries = groups.flatMap((group) =>
    isRecord(group) ? Object.entries(group) : [],
  )

  return Object.fromEntries(
    entries.map(([name, value]) => {
      const details = isRecord(value) ? value : undefined
      const rawScore = details ? details.score : value
      const score =
        typeof rawScore === "number" &&
        Number.isFinite(rawScore) &&
        rawScore >= 0 &&
        rawScore <= 100
          ? Math.round(rawScore)
          : null
      const result: MetricResult = {
        score,
        trend:
          typeof details?.trend === "number" && Number.isFinite(details.trend)
            ? details.trend
            : null,
      }
      const displayName = optionalText(details?.name)
      const description = optionalText(details?.description)
      if (displayName) result.name = displayName
      if (description) result.description = description
      return [name, result]
    }),
  )
}

function categoryPayload(grading?: GradingResponse) {
  return grading?.category_scores !== undefined
    ? grading.category_scores
    : grading?.criteria
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
  const categoryScores = normalizeCategoryScores(categoryPayload(grading))
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
    categoryScores:
      categoryPayload(grading) !== undefined || grading.status === "FINISHED"
        ? normalizeCategoryScores(categoryPayload(grading))
        : repository.categoryScores,
    gradeStatus: grading.status,
    gradedAt: grading.graded_at ?? repository.gradedAt,
    missingData: grading.missing_data
      ? normalizeMissingData(grading.missing_data)
      : repository.missingData,
  }
}
