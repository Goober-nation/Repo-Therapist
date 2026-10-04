import { categoryLabel, PRIORITY_RANK } from "../metrics"
import type {
  AiReport,
  CategoryScores,
  Confidence,
  MetricFact,
  MetricResult,
  Priority,
  Recommendation,
  RepositorySummary,
} from "../types"
import type {
  AdvicePayload,
  ConfidenceReasonPayload,
  GatewayRankingEntry,
  GradingResponse,
  MyRepository,
  RecommendationsResponse,
} from "./contracts"

const DAY_MS = 86_400_000

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

function optionalText(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value : undefined
}

function finiteNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined
}

/** A 0–100 score rounded for display; anything else is not a score. */
function scoreValue(value: unknown): number | null {
  const score = finiteNumber(value)
  return score !== undefined && score >= 0 && score <= 100 ? Math.round(score) : null
}

function daysSince(isoDate?: string): number {
  const time = isoDate ? new Date(isoDate).getTime() : NaN
  return Number.isFinite(time) ? Math.max(0, Math.floor((Date.now() - time) / DAY_MS)) : -1
}

function metricFacts(metrics: unknown): MetricFact[] {
  if (!Array.isArray(metrics)) return []
  return metrics.flatMap((metric) => {
    if (!isRecord(metric) || typeof metric.id !== "string") return []
    const noData = metric.no_data === true
    const value = noData ? undefined : finiteNumber(metric.raw_value)
    const score = noData ? undefined : finiteNumber(metric.score)
    return [{ id: metric.id, value: value ?? null, score: score !== undefined && score >= 0 && score <= 1 ? score : null }]
  })
}

export function normalizeCategoryScores(payload?: unknown): CategoryScores {
  const groups = Array.isArray(payload) ? payload : [payload]
  const entries = groups.flatMap((group) => (isRecord(group) ? Object.entries(group) : []))

  return Object.fromEntries(
    entries.map(([name, value]) => {
      const details = isRecord(value) ? value : undefined
      const isNoData = details?.no_data === true || details?.status === "NO_DATA"
      const rawScore = isNoData ? null : details ? details.score : value
      const score = scoreValue(rawScore)
      const result: MetricResult = { score, trend: finiteNumber(details?.trend) ?? null }
      const displayName = optionalText(details?.name)
      const description = optionalText(details?.description)
      const facts = metricFacts(details?.metrics)
      const method = details?.method
      if (method === "llm" || method === "algorithmic") result.method = method
      if (displayName) result.name = displayName
      if (description) result.description = description
      if (facts.length > 0) result.facts = facts
      return [name, result]
    }),
  )
}

function categoryPayload(grading?: GradingResponse) {
  if (grading?.grade) return grading.grade.category_scores
  return grading?.category_scores !== undefined ? grading.category_scores : grading?.criteria
}

function gradeScore(grading?: GradingResponse): number | null | undefined {
  const score = grading?.grade?.score !== undefined ? grading.grade.score : grading?.health_score
  if (grading?.status === "NO_DATA" || (grading?.status === "FINISHED" && score === undefined)) {
    return null
  }
  if (score === null) return null
  if (finiteNumber(score) === undefined) return undefined
  return scoreValue(score)
}

const PRIORITIES: readonly Priority[] = ["critical", "high", "medium", "low"]

function isPriority(value: string | undefined): value is Priority {
  return PRIORITIES.includes(value as Priority)
}

function toPriority(value?: string): Priority {
  const normalized = value?.toLowerCase()
  return isPriority(normalized) ? normalized : "medium"
}

function byPriority(left: Recommendation, right: Recommendation) {
  return PRIORITY_RANK[left.priority] - PRIORITY_RANK[right.priority]
}

/**
 * grade-service advice → recommendations. The `priority` field wins; without it the
 * transitional "[high] Text" tag is used. The tag is always stripped from the text.
 */
export function adviceToRecommendations(advice: AdvicePayload[] | null | undefined): Recommendation[] {
  if (!Array.isArray(advice)) return []
  return advice
    .flatMap((item, index) => {
      const text = optionalText(item?.text)
      if (!text) return []
      const [, tag, body] = /^\s*\[(\w+)\]\s*(.*)$/s.exec(text) ?? [, undefined, text]
      const field = item.priority?.toLowerCase()
      return [
        {
          id: `advice-${index}`,
          priority: isPriority(field) ? field : toPriority(tag),
          title: item.category_id ? categoryLabel(item.category_id) : "Рекомендация",
          description: body.trim(),
          ...(item.category_id ? { category: item.category_id } : {}),
        },
      ]
    })
    .sort(byPriority)
}

/**
 * Reads the low-confidence flag from the wire fields. A missing or null flag is "unknown" and
 * stays undefined: it must never be shown as low, and not as confirmed-reliable either.
 */
export function confidenceFromPayload(
  low: boolean | null | undefined,
  reasons: ConfidenceReasonPayload[] | undefined,
): Confidence | undefined {
  if (typeof low !== "boolean") return undefined
  const texts = (Array.isArray(reasons) ? reasons : []).flatMap((reason) =>
    typeof reason?.text === "string" && reason.text.trim() ? [reason.text.trim()] : [],
  )
  return { low, reasons: low ? texts : [] }
}

export function repositoryFromRanking(entry: GatewayRankingEntry): RepositorySummary | null {
  if (
    (entry.public_score !== undefined && entry.public_score !== null && typeof entry.public_score !== "number") ||
    !entry.web_url
  ) {
    return null
  }
  try {
    const segments = new URL(entry.web_url).pathname.split("/").filter(Boolean)
    if (segments.length < 2) return null
    const repoSlug = segments.at(-1)!
    const orgSlug = segments.at(-2)!
    return {
      id: entry.sourcecraft_uuid,
      sourceCraftUuid: entry.sourcecraft_uuid,
      orgSlug,
      repoSlug,
      name: entry.name || repoSlug,
      fullName: `${orgSlug}/${repoSlug}`,
      language: entry.language?.name || "Не указан",
      ...languageColor(entry.language),
      ...(finiteNumber(entry.place) !== undefined ? { place: entry.place } : {}),
      visibility: "public",
      analysisMode: "public",
      webUrl: entry.web_url,
      lastUpdated: entry.last_activity_at,
      healthScore: scoreValue(entry.public_score),
      ...(typeof entry.likes === "number" ? { likes: entry.likes } : {}),
      ...(confidenceFromPayload(entry.low_confidence, entry.confidence_reasons)
        ? { confidence: confidenceFromPayload(entry.low_confidence, entry.confidence_reasons) }
        : {}),
      lastActivityDaysAgo: daysSince(entry.last_activity_at),
      categoryScores: {},
      gradeStatus: "FINISHED",
      missingData: [],
    }
  } catch {
    return null
  }
}

/** Only a hex color is kept, so a malformed value can never reach a style attribute. */
function languageColor(language: { name?: string; color?: string } | undefined): { languageColor?: string } {
  const color = language?.name ? language.color?.trim() : undefined
  return color && /^#(?:[0-9a-f]{3}|[0-9a-f]{6})$/i.test(color) ? { languageColor: color } : {}
}

/** A repository from GET /repos, analysed with the owner's token. */
export function repositoryFromMyRepos(repository: MyRepository): RepositorySummary {
  const fullName = `${repository.org_slug}/${repository.repo_slug}`
  return {
    id: repository.sourcecraft_uuid || fullName,
    sourceCraftUuid: repository.sourcecraft_uuid,
    orgSlug: repository.org_slug,
    repoSlug: repository.repo_slug,
    name: repository.repo_slug,
    fullName,
    language: repository.language?.name || "Не указан",
    ...languageColor(repository.language),
    visibility: repository.visibility,
    analysisMode: "private",
    webUrl: repository.web_url,
    lastUpdated: repository.last_activity_at,
    healthScore: null,
    lastActivityDaysAgo: daysSince(repository.last_activity_at),
    categoryScores: {},
  }
}

/** A repository known only by its slugs (pasted link, direct URL). */
export function repositoryStub(orgSlug: string, repoSlug: string, isPrivate: boolean): RepositorySummary {
  return {
    id: `sourcecraft:${orgSlug}/${repoSlug}`,
    orgSlug,
    repoSlug,
    name: repoSlug,
    fullName: `${orgSlug}/${repoSlug}`,
    language: "Не указан",
    analysisMode: isPrivate ? "private" : "public",
    webUrl: `https://sourcecraft.dev/${encodeURIComponent(orgSlug)}/${encodeURIComponent(repoSlug)}`,
    healthScore: null,
    lastActivityDaysAgo: -1,
    categoryScores: {},
  }
}

export function normalizeMissingData(missingData?: Record<string, unknown> | string[]): string[] {
  if (Array.isArray(missingData)) return missingData.map(String)
  return Object.entries(missingData ?? {})
    .filter(([, value]) => Boolean(value))
    .map(([key]) => key)
}

export function recommendationsFromResponse(response: RecommendationsResponse): Recommendation[] {
  const items = response.raw_response?.recommendations
  if (!Array.isArray(items)) return []
  return items
    .flatMap((item, index) => {
      const text = optionalText(item?.text)
      if (!text) return []
      const location = item.location ?? item.place
      return [
        {
          id: `ai-${index}`,
          priority: toPriority(item.priority),
          title: item.category ? categoryLabel(item.category) : "Рекомендация",
          description: text,
          ...(item.category ? { category: item.category } : {}),
          ...(location ? { location } : {}),
        },
      ]
    })
    .sort(byPriority)
}

export function aiReportFromResponse(response: RecommendationsResponse): AiReport {
  return {
    status: response.status,
    summary: optionalText(response.raw_response?.summary)?.trim(),
    modelUsed: optionalText(response.model_used)?.trim() || undefined,
    recommendations: recommendationsFromResponse(response),
  }
}

export function mergeGradingIntoRepository(
  repository: RepositorySummary,
  grading: GradingResponse,
): RepositorySummary {
  const score = gradeScore(grading)
  const payload = categoryPayload(grading)
  const missing = grading.grade?.missing_data ?? grading.missing_data
  const completed = grading.status === "FINISHED" || grading.status === "NO_DATA"
  return {
    ...repository,
    healthScore: score !== undefined ? score : repository.healthScore,
    categoryScores:
      payload !== undefined || completed ? normalizeCategoryScores(payload) : repository.categoryScores,
    gradeStatus: grading.status,
    gradedAt: grading.grade?.graded_at ?? grading.graded_at ?? repository.gradedAt,
    missingData: missing ? normalizeMissingData(missing) : repository.missingData,
    advice: grading.grade ? adviceToRecommendations(grading.grade.advice) : repository.advice,
  }
}
