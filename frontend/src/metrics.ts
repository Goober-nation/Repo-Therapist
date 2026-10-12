import type { CategoryScores, Priority, RepositorySummary } from "./types"

export type Tone = "good" | "warning" | "critical" | "neutral"

/** Score at which a repository counts as healthy (ranking, stats, strengths, summary copy). */
export const HEALTHY_SCORE = 70

// One set of bands for the ranking, analytics, badges and the repository page; the gateway
// (healthLevel, badge colours) and rating-service (analytics counts) use the same boundaries.
const LEVELS: { min: number; label: string; tone: Tone }[] = [
  { min: 80, label: "Отличное состояние", tone: "good" },
  { min: HEALTHY_SCORE, label: "Здоровый", tone: "good" },
  { min: 40, label: "Требует внимания", tone: "warning" },
  { min: 0, label: "Под риском", tone: "critical" },
]

export function healthLevel(score: number | null): { label: string; tone: Tone } {
  if (score === null) return { label: "Нет данных", tone: "neutral" }
  return LEVELS.find((level) => score >= level.min) ?? LEVELS[LEVELS.length - 1]
}

/** Kept for the ranking's callers: the ranking uses the same bands as everything else. */
export const RANKING_HEALTHY_SCORE = HEALTHY_SCORE

/** healthLevel under its ranking name. */
export const rankingLevel = healthLevel

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

/**
 * Token for grading and recommendation requests. A public repository opened from "my
 * repositories" can be graded publicly on request: then no token is sent.
 */
export function gradingToken(
  pat: string | undefined,
  privateRoute: boolean,
  visibility: RepositorySummary["visibility"],
  publicView: boolean,
): string | undefined {
  if (!privateRoute) return undefined
  return publicView && visibility === "public" ? undefined : pat
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
          ? "Анализ безопасности для приватных репозиториев доступен только с Security Add-on"
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
  unused_private_share: { label: "Доля неиспользуемых приватных функций", percent: true },
  abandoned_share: { label: "Доля заброшенных issues", percent: true },
  median_closure_days: { label: "Медиана закрытия issue, дней" },
  growth_ratio: { label: "Открыто issues на одно закрытое" },
  share_resolved: { label: "Доля решённых issues", percent: true },
  median_first_reply_hours: { label: "Медиана до первого ответа на issue, часов" },
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
  required_files: { label: "Обязательные файлы" },
  purpose: { label: "Назначение проекта" },
  local_run: { label: "Локальный запуск" },
  build: { label: "Сборка" },
  usage: { label: "Использование и примеры" },
  structure: { label: "Структура проекта" },
  testing: { label: "Тесты" },
  consistency: { label: "Согласованность документов" },
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
  critical: "Критический приоритет",
  high: "Высокий приоритет",
  medium: "Средний приоритет",
  low: "Низкий приоритет",
}

export const PRIORITY_RANK: Record<Priority, number> = { critical: 0, high: 1, medium: 2, low: 3 }

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

/** A grade older than this is shown as outdated (docs/grading/frontend-results.md). */
export const STALE_GRADE_DAYS = 7

/** True when the grade was computed more than STALE_GRADE_DAYS ago; false for a missing or invalid date. */
export function isGradeStale(gradedAt?: string, now: number = Date.now()): boolean {
  const time = gradedAt ? Date.parse(gradedAt) : Number.NaN
  return Number.isFinite(time) && now - time > STALE_GRADE_DAYS * 24 * 60 * 60 * 1000
}

/** "4 окт." for a chart axis or a history row; undefined for a missing or invalid date. */
export function formatShortDate(value?: string): string | undefined {
  const time = value ? new Date(value) : undefined
  return time && Number.isFinite(time.getTime())
    ? time.toLocaleDateString("ru-RU", { day: "numeric", month: "short" })
    : undefined
}

/** A change with an explicit sign: +4, −2 (a real minus sign) or 0. */
export function signed(value: number): string {
  if (value > 0) return `+${value}`
  if (value < 0) return `\u2212${Math.abs(value)}`
  return "0"
}

/** Time left until the scheduled regrade: "47 ч 12 мин", "9 мин", or null once the time has come. */
export function formatCountdown(milliseconds: number): string | null {
  if (milliseconds <= 0) return null
  const minutes = Math.ceil(milliseconds / 60000)
  if (minutes < 60) return `${minutes} мин`
  const hours = Math.floor(minutes / 60)
  const rest = minutes % 60
  return rest === 0 ? `${hours} ч` : `${hours} ч ${rest} мин`
}
