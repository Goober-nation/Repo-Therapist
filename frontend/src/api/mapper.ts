import { categoryLabel, PRIORITY_RANK } from "../metrics"
import type {
  AiReport,
  CategoryScores,
  Confidence,
  RepoAnalytics,
  ReviewBottlenecks,
  MetricFact,
  IssueRepliesStats,
  MetricResult,
  OwnershipArea,
  PositionExplanation,
  PositionNeighbour,
  Priority,
  Recommendation,
  RegradeStatus,
  RepositorySummary,
  ScorePoint,
} from "../types"
import type {
  AdvicePayload,
  AnalyticsResponse,
  ConfidenceReasonPayload,
  GatewayRankingEntry,
  GradingResponse,
  HistoryResponse,
  MyOrganization,
  MyOrganizationsResponse,
  MyRepository,
  PositionResponse,
  RecommendationsResponse,
  RegradeStatusResponse,
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

const finiteOr = (value: unknown, fallback: number) =>
  typeof value === "number" && Number.isFinite(value) ? value : fallback

const share = (value: unknown) => Math.min(1, Math.max(0, finiteOr(value, 0)))

/**
 * Reads the analytics response. null means the repo has none (status NO_DATA or an unusable body);
 * a block the backend marked as no data keeps `available: false` rather than showing zeros.
 */
export function analyticsFromResponse(response: AnalyticsResponse | undefined): RepoAnalytics | null {
  const raw = response?.analytics
  if (!raw || typeof raw !== "object") return null

  const bus = raw.bus_factor
  const busAvailable = bus?.status === "ok"
  const ownership = raw.ownership
  const codeowners = ownership?.codeowners
  // Analytics stored before version 2 have no reviews block: show it as unavailable, not as zeros.
  const reviews = raw.reviews
  const reviewsAvailable = reviews?.status === "ok"

  return {
    windowDays: finiteOr(raw.window_days, 0),
    reviews: {
      available: reviewsAvailable,
      reason: typeof reviews?.reason === "string" && reviews.reason ? reviews.reason : "unavailable",
      sampled: finiteOr(reviews?.sampled, 0),
      merged: finiteOr(reviews?.merged, 0),
      unreviewed: finiteOr(reviews?.unreviewed, 0),
      ...(reviewsAvailable && typeof reviews?.unreviewed_share === "number"
        ? { unreviewedShare: share(reviews.unreviewed_share) }
        : {}),
      ...(reviewsAvailable && typeof reviews?.median_merge_hours === "number" && reviews.median_merge_hours >= 0
        ? { medianMergeHours: reviews.median_merge_hours }
        : {}),
      // Analytics stored before version 3 have no bottlenecks: unavailable, never zeros.
      bottlenecks: bottlenecksFromPayload(reviews?.bottlenecks),
    },
    // Analytics stored before version 4 have no issue_replies block: unavailable, never zeros.
    issueReplies: issueRepliesFromPayload(raw.issue_replies),
    busFactor: {
      available: busAvailable,
      value: busAvailable ? finiteOr(bus?.value, 0) : 0,
      commits: finiteOr(bus?.commits, 0),
      authors: finiteOr(bus?.authors, 0),
      topAuthors: (Array.isArray(bus?.top_authors) ? bus.top_authors : []).flatMap((author) =>
        typeof author?.name === "string" && author.name
          ? [{ name: author.name, commits: finiteOr(author.commits, 0), share: share(author.share) }]
          : [],
      ),
    },
    ownership: {
      available: ownership?.status === "ok",
      areas: ownershipAreas(ownership?.areas),
      // Analytics stored before version 3 have neither: both stay empty and the views are not offered.
      subareas: ownershipAreas(ownership?.subareas),
      files: (Array.isArray(ownership?.files) ? ownership.files : []).flatMap((file) =>
        typeof file?.path === "string" && file.path
          ? [
              {
                path: file.path,
                lines: finiteOr(file.lines, 0),
                topOwner: typeof file.top_owner === "string" ? file.top_owner : "—",
                topOwnerShare: share(file.top_owner_share),
                owners: finiteOr(file.owners, 0),
              },
            ]
          : [],
      ),
      codeowners: {
        present: codeowners?.present === true,
        ...(typeof codeowners?.path === "string" && codeowners.path ? { path: codeowners.path } : {}),
        rules: finiteOr(codeowners?.rules, 0),
        ...(typeof codeowners?.covered_share === "number" ? { coveredShare: share(codeowners.covered_share) } : {}),
      },
    },
  }
}

function issueRepliesFromPayload(raw: unknown): IssueRepliesStats {
  const value = isRecord(raw) ? raw : {}
  const available = value.status === "ok"
  const median = value.median_first_reply_hours
  return {
    available,
    reason: typeof value.reason === "string" && value.reason ? value.reason : "unavailable",
    sampled: finiteOr(value.sampled, 0),
    replied: finiteOr(value.replied, 0),
    unanswered: finiteOr(value.unanswered, 0),
    capDays: finiteOr(value.cap_days, 0),
    ...(available && typeof value.replied_share === "number" ? { repliedShare: share(value.replied_share) } : {}),
    ...(available && typeof median === "number" && Number.isFinite(median) && median >= 0
      ? { medianFirstReplyHours: median }
      : {}),
  }
}

function bottlenecksFromPayload(raw: unknown): ReviewBottlenecks {
  const value = isRecord(raw) ? raw : {}
  const available = value.status === "ok"
  const median = value.median_first_review_hours
  return {
    available,
    reason: typeof value.reason === "string" && value.reason ? value.reason : "unavailable",
    reviewed: finiteOr(value.reviewed, 0),
    open: finiteOr(value.open, 0),
    waiting: finiteOr(value.waiting, 0),
    waitingDays: finiteOr(value.waiting_days, 0),
    ...(available && typeof median === "number" && Number.isFinite(median) && median >= 0
      ? { medianFirstReviewHours: median }
      : {}),
  }
}

function ownershipAreas(raw: unknown): OwnershipArea[] {
  return (Array.isArray(raw) ? raw : []).flatMap((area) =>
    isRecord(area) && typeof area.path === "string" && area.path
      ? [
          {
            path: area.path,
            files: finiteOr(area.files, 0),
            lines: finiteOr(area.lines, 0),
            topOwner: typeof area.top_owner === "string" ? area.top_owner : "—",
            topOwnerShare: share(area.top_owner_share),
            owners: finiteOr(area.owners, 0),
          },
        ]
      : [],
  )
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
      ...(entry.graded_at ? { gradedAt: entry.graded_at } : {}),
      healthScore: scoreValue(entry.public_score),
      ...(typeof entry.likes === "number" ? { likes: entry.likes } : {}),
      ...(confidenceFromPayload(entry.low_confidence, entry.confidence_reasons)
        ? { confidence: confidenceFromPayload(entry.low_confidence, entry.confidence_reasons) }
        : {}),
      ...(wholeNumber(entry.score_change) !== undefined ? { scoreChange: wholeNumber(entry.score_change) } : {}),
      ...(wholeNumber(entry.place_change) !== undefined ? { placeChange: wholeNumber(entry.place_change) } : {}),
      ...(wholeNumber(entry.grade_count) !== undefined ? { gradeCount: wholeNumber(entry.grade_count) } : {}),
      lastActivityDaysAgo: daysSince(entry.last_activity_at),
      categoryScores: {},
      gradeStatus: "FINISHED",
      missingData: [],
    }
  } catch {
    return null
  }
}

/** The scheduled regrade; a missing response or a malformed time counts as "no schedule shown". */
export function regradeFromResponse(response: RegradeStatusResponse | undefined): RegradeStatus | undefined {
  if (!response || typeof response.enabled !== "boolean") return undefined
  const next = typeof response.next_run_at === "string" && !Number.isNaN(Date.parse(response.next_run_at)) ? response.next_run_at : undefined
  const count = (value: unknown) => (typeof value === "number" && Number.isInteger(value) && value >= 0 ? value : 0)
  return {
    enabled: response.enabled,
    ...(response.enabled && next ? { nextRunAt: next } : {}),
    running: response.running === true,
    total: count(response.total),
    done: count(response.done),
  }
}

/** A change shown in the ranking: a finite whole number, anything else (null, text) is "no change data". */
function wholeNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isInteger(value) ? value : undefined
}

/**
 * Stored gradings from GET /history, oldest first. A point without a valid time or score is dropped;
 * a category without a valid score becomes null ("no data"), never 0.
 */
export function historyFromResponse(response: HistoryResponse | undefined): ScorePoint[] {
  if (!response || response.status !== "FINISHED" || !Array.isArray(response.points)) return []
  const points = response.points.flatMap((point) => {
    const score = scoreValue(point?.score)
    if (!isRecord(point) || score === null || typeof point.graded_at !== "string" || !Number.isFinite(Date.parse(point.graded_at))) {
      return []
    }
    return [
      {
        gradedAt: point.graded_at,
        score,
        categories: (Array.isArray(point.categories) ? point.categories : []).flatMap((category) =>
          isRecord(category) && typeof category.id === "string"
            ? [{ id: category.id, score: category.no_data ? null : scoreValue(category.score) }]
            : [],
        ),
        lowConfidence: typeof point.low_confidence === "boolean" ? point.low_confidence : null,
      },
    ]
  })
  return points.sort((a, b) => Date.parse(a.gradedAt) - Date.parse(b.gradedAt))
}

function neighbourFromPayload(value: unknown): PositionNeighbour | undefined {
  if (!isRecord(value)) return undefined
  const place = wholeNumber(value.place)
  const score = scoreValue(value.score)
  const points = wholeNumber(value.points)
  if (place === undefined || score === null || points === undefined || typeof value.name !== "string") return undefined
  return { place, org: typeof value.org === "string" ? value.org : "", name: value.name, score, points }
}

/** The explanation from GET /position, or null when the repository is not ranked or the answer is malformed. */
export function positionFromResponse(response: PositionResponse | undefined): PositionExplanation | null {
  if (!response || response.status !== "FINISHED") return null
  const place = wholeNumber(response.place)
  const total = wholeNumber(response.total)
  const score = scoreValue(response.score)
  if (place === undefined || place < 1 || total === undefined || total < place || score === null) return null
  const above = neighbourFromPayload(response.above)
  const below = neighbourFromPayload(response.below)
  const categories = (Array.isArray(response.categories) ? response.categories : []).flatMap((category) => {
    const categoryScore = scoreValue(category?.score)
    if (!isRecord(category) || typeof category.id !== "string" || categoryScore === null) return []
    const median = scoreValue(category.top10_median)
    const gap = wholeNumber(category.gap)
    return [{ id: category.id, score: categoryScore, ...(median !== null && gap !== undefined ? { top10Median: median, gap } : {}) }]
  })
  return {
    place,
    total,
    score,
    ...(above ? { above } : {}),
    ...(below ? { below } : {}),
    categories,
    ...(typeof response.weakest_category === "string" && response.weakest_category
      ? { weakestCategory: response.weakest_category }
      : {}),
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

/** The org to open first: the server's choice if listed, else the personal org, else the first. */
export function defaultOrganization(response: MyOrganizationsResponse): string | undefined {
  const organizations = response.organizations ?? []
  const preferred = response.default_org_slug
  if (preferred && organizations.some((org) => org.slug === preferred)) return preferred
  return (organizations.find((org) => org.personal) ?? organizations[0])?.slug
}

/** Dropdown label for an organization. */
export function organizationLabel(organization: MyOrganization): string {
  if (organization.personal) return `${organization.slug} (личная)`
  const name = organization.display_name?.trim()
  return name && name !== organization.slug ? `${name} (${organization.slug})` : organization.slug
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
