import type { CategoryScores, Priority, RepositorySummary } from "./types"

export type Tone = "good" | "warning" | "critical" | "neutral"

/** Score at which a repository counts as healthy (stats, strengths, summary copy). */
export const HEALTHY_SCORE = 75

const LEVELS: { min: number; label: string; tone: Tone }[] = [
  { min: 90, label: "Отличное состояние", tone: "good" },
  { min: HEALTHY_SCORE, label: "Здоровый", tone: "good" },
  { min: 60, label: "Требует внимания", tone: "warning" },
  { min: 0, label: "Под риском", tone: "critical" },
]

export function healthLevel(score: number | null): { label: string; tone: Tone } {
  if (score === null) return { label: "Нет данных", tone: "neutral" }
  return LEVELS.find((level) => score >= level.min) ?? LEVELS[LEVELS.length - 1]
}

/** Mirrors config/grading.yaml. Keep in sync when weights change. */
const CATEGORIES: Record<string, { label: string; weight: number; privateOnly?: boolean }> = {
  activity: { label: "Activity", weight: 20 },
  issues: { label: "Issues", weight: 20 },
  documentation: { label: "Documentation", weight: 15 },
  code_health: { label: "Code health", weight: 10 },
  security: { label: "Security", weight: 20, privateOnly: true },
  ci_cd: { label: "CI/CD", weight: 15, privateOnly: true },
}

function category(id: string) {
  return Object.hasOwn(CATEGORIES, id) ? CATEGORIES[id] : undefined
}

export function categoryLabel(id: string): string {
  return category(id)?.label ?? id
}

/**
 * Share of the overall score each known category carries, in percent. Categories not in
 * `withData` are dropped and the rest renormalized, exactly as grade-service combines them.
 * Without `withData` every category of the mode counts (the configured split).
 */
export function scoredCategories(isPrivate: boolean, withData?: CategoryScores) {
  const ids = Object.keys(CATEGORIES).filter(
    (id) =>
      (isPrivate || !CATEGORIES[id].privateOnly) &&
      (!withData || (Object.hasOwn(withData, id) && withData[id].score !== null)),
  )
  const total = ids.reduce((sum, id) => sum + CATEGORIES[id].weight, 0)
  // Largest-remainder rounding, so the shown percentages always add up to exactly 100.
  const exact = ids.map((id) => (CATEGORIES[id].weight / total) * 100)
  const weights = exact.map(Math.floor)
  const byRemainder = exact.map((value, index) => index).sort((a, b) => exact[b] % 1 - exact[a] % 1)
  const missing = ids.length ? 100 - weights.reduce((sum, value) => sum + value, 0) : 0
  for (let index = 0; index < missing; index += 1) weights[byRemainder[index]] += 1
  return ids.map((id, index) => ({ id, label: categoryLabel(id), weight: weights[index] }))
}

export function categoryWeight(id: string, isPrivate: boolean, withData?: CategoryScores): number | undefined {
  return scoredCategories(isPrivate, withData).find((item) => item.id === id)?.weight
}

export function isPrivateAnalysis(repository: RepositorySummary): boolean {
  return repository.analysisMode === "private"
}

/** Categories that did not contribute to the score, with a human reason. */
export function missingCategories(repository: RepositorySummary) {
  const isPrivate = isPrivateAnalysis(repository)
  const graded =
    Boolean(repository.gradedAt) || Object.keys(repository.categoryScores).length > 0
  if (!graded) return []
  const ids = new Set(repository.missingData ?? [])
  for (const [id, result] of Object.entries(repository.categoryScores)) {
    if (result.score === null) ids.add(id)
  }
  if (!isPrivate) {
    for (const [id, meta] of Object.entries(CATEGORIES)) {
      if (meta.privateOnly && !(id in repository.categoryScores)) ids.add(id)
    }
  }
  return [...ids].map((id) => ({
    id,
    label: repository.categoryScores[id]?.name ?? categoryLabel(id),
    reason:
      !isPrivate && category(id)?.privateOnly
        ? "доступно только владельцу: данные AppSec и CI/CD SourceCraft отдаёт по токену владельца"
        : id === "security" && repository.visibility === "private"
          ? "AppSec не вернул данные: для закрытого репозитория нужен подключённый Security Add-on SourceCraft"
          : "источник данных не вернул информацию для этого репозитория",
  }))
}

const METRICS: Record<string, { label: string; percent?: boolean }> = {
  commits_per_week: { label: "Коммитов в неделю (180 дней)" },
  days_since_last_activity: { label: "Дней с последнего коммита" },
  unique_contributors: { label: "Авторов коммитов (180 дней)" },
  issue_rate_per_week: { label: "Новых issues в неделю" },
  milestones_per_year: { label: "Milestones в год" },
  days_since_last_milestone: { label: "Дней с последнего milestone" },
  top_contributor_share: { label: "Доля коммитов главного автора", percent: true },
  median_marker_age: { label: "Медианный возраст TODO/FIXME, дней" },
  density_per_kloc: { label: "TODO/FIXME на 1000 строк" },
  share_stale_markers: { label: "Доля устаревших TODO/FIXME", percent: true },
  worst_file_concentration: { label: "Доля TODO/FIXME в одном файле", percent: true },
  abandoned_share: { label: "Доля заброшенных issues", percent: true },
  median_closure_days: { label: "Медиана закрытия issue, дней" },
  growth_ratio: { label: "Открыто issues на одно закрытое" },
  share_resolved: { label: "Доля решённых issues", percent: true },
  has_ci: { label: "CI настроен (1 — да)" },
  ci_success_rate: { label: "Успешных прогонов CI", percent: true },
  ci_stability_trend: { label: "Тренд стабильности CI" },
  ci_median_duration_min: { label: "Медианная длительность CI, мин" },
  ci_noop_pipeline: { label: "CI выполняет только заглушки (1 — да)" },
  open_critical: { label: "Открытых critical-уязвимостей" },
  open_high: { label: "Открытых high-уязвимостей" },
  open_medium: { label: "Открытых medium-уязвимостей" },
  open_low: { label: "Открытых low-уязвимостей" },
  days_since_last_scan: { label: "Дней с последнего скана AppSec" },
}

export function metricLabel(id: string): string {
  return Object.hasOwn(METRICS, id) ? METRICS[id].label : id
}

export function formatMetricValue(id: string, value: number | null): string {
  if (value === null) return "нет данных"
  if (Object.hasOwn(METRICS, id) && METRICS[id].percent) return `${Math.round(value * 100)}%`
  return String(Math.round(value * 100) / 100)
}

/** Strong/weak categories by score, used when the AI summary is unavailable. */
export function splitStrengths(categories: CategoryScores) {
  const scored = Object.entries(categories).flatMap(([id, result]) =>
    result.score === null ? [] : [{ label: result.name ?? categoryLabel(id), score: result.score }],
  )
  const describe = ({ label, score }: { label: string; score: number }) => `${label}: ${score}/100`
  return {
    strengths: scored.filter((item) => item.score >= HEALTHY_SCORE).map(describe),
    weaknesses: scored.filter((item) => item.score < HEALTHY_SCORE).map(describe),
  }
}

export const PRIORITY_LABELS: Record<Priority, string> = {
  high: "Высокий приоритет",
  medium: "Средний приоритет",
  low: "Низкий приоритет",
}

export const PRIORITY_RANK: Record<Priority, number> = { high: 0, medium: 1, low: 2 }

export function getLastActivityLabel(daysAgo: number): string {
  if (daysAgo < 0) return "нет данных об активности"
  if (daysAgo === 0) return "Сегодня"
  if (daysAgo === 1) return "Вчера"
  if (daysAgo < 7) return `${daysAgo} дн. назад`
  if (daysAgo < 30) return `${Math.floor(daysAgo / 7)} нед. назад`
  return `${Math.floor(daysAgo / 30)} мес. назад`
}

export function formatDateTime(value?: string): string | undefined {
  if (!value) return undefined
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? undefined : date.toLocaleString("ru-RU")
}
