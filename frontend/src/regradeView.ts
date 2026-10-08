import type { BatchFilterBody, BatchStatus, RegradeBatchResponse } from "./api/contracts"
import type { Tone } from "./metrics"

/** What the admin typed into the batch form; every field is raw text. */
export interface BatchForm {
  mode: "criteria" | "all"
  languages: string
  minScore: string
  maxScore: string
  olderThanDays: string
  repos: string
  topRated: string
  /** Regrade without the "new activity since the last grade" check. */
  force: boolean
}

export const EMPTY_BATCH_FORM: BatchForm = {
  mode: "criteria",
  languages: "",
  minScore: "",
  maxScore: "",
  olderThanDays: "",
  repos: "",
  topRated: "",
  force: false,
}

/** Mirrors the Aggregator's limits (docs/admin.md). */
export const MAX_BATCH_LANGUAGES = 20
export const MAX_BATCH_REPOS = 200
const MAX_BATCH_DAYS = 3650
export const MAX_BATCH_TOP_RATED = 1000
const REPO_PATTERN = /^[A-Za-z0-9._-]{1,100}\/[A-Za-z0-9._-]{1,100}$/

export type BuiltFilter = { filter: BatchFilterBody; problem?: undefined } | { filter?: undefined; problem: string }

function words(text: string, separators: RegExp): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const part of text.split(separators)) {
    const value = part.trim()
    if (!value || seen.has(value.toLowerCase())) continue
    seen.add(value.toLowerCase())
    out.push(value)
  }
  return out
}

function wholeNumber(text: string, label: string, min: number, max: number): number | string | undefined {
  const value = text.trim()
  if (!value) return undefined
  if (!/^\d+$/.test(value)) return `${label}: нужно целое число.`
  const number = Number(value)
  if (number < min || number > max) return `${label}: от ${min} до ${max}.`
  return number
}

/** Turns the form into the request body, or says in Russian what is wrong with it. */
export function buildFilter(form: BatchForm): BuiltFilter {
  if (form.mode === "all") return { filter: form.force ? { all: true, force: true } : { all: true } }

  const filter: BatchFilterBody = {}

  const languages = words(form.languages, /[,;\n]+/)
  if (languages.length > MAX_BATCH_LANGUAGES) return { problem: `Языков не больше ${MAX_BATCH_LANGUAGES}.` }
  if (languages.some((language) => language.length > 64)) return { problem: "Название языка слишком длинное." }
  if (languages.length > 0) filter.languages = languages

  const repos = words(form.repos, /[\s,;]+/)
  if (repos.length > MAX_BATCH_REPOS) return { problem: `Репозиториев в списке не больше ${MAX_BATCH_REPOS}.` }
  const bad = repos.find((repo) => !REPO_PATTERN.test(repo))
  if (bad) return { problem: `«${bad}»: репозиторий записывается как организация/репозиторий.` }
  if (repos.length > 0) filter.repos = repos

  const min = wholeNumber(form.minScore, "Оценка от", 0, 100)
  const max = wholeNumber(form.maxScore, "Оценка до", 0, 100)
  const days = wholeNumber(form.olderThanDays, "Давность оценки", 1, MAX_BATCH_DAYS)
  const top = wholeNumber(form.topRated, "Топ по рейтингу", 1, MAX_BATCH_TOP_RATED)
  for (const value of [min, max, days, top]) if (typeof value === "string") return { problem: value }
  if (typeof top === "number") filter.top_rated = top
  if (typeof min === "number") filter.min_score = min
  if (typeof max === "number") filter.max_score = max
  if (typeof days === "number") filter.older_than_days = days
  if (typeof min === "number" && typeof max === "number" && min > max) return { problem: "Оценка «от» больше оценки «до»." }

  if (Object.keys(filter).length === 0) {
    return { problem: "Выберите хотя бы одно условие или отметьте все публичные репозитории." }
  }

  if (form.force) filter.force = true

  return { filter }
}

const STATUS_LABELS: Record<BatchStatus, { label: string; tone: Tone }> = {
  running: { label: "Идёт", tone: "good" },
  paused: { label: "На паузе", tone: "warning" },
  completed: { label: "Завершён", tone: "good" },
  cancelled: { label: "Отменён", tone: "neutral" },
}

export function statusLabel(status: string): { label: string; tone: Tone } {
  return Object.hasOwn(STATUS_LABELS, status) ? STATUS_LABELS[status as BatchStatus] : { label: status, tone: "neutral" }
}

const ITEM_ERRORS: Record<string, string> = {
  request_failed: "не удалось запустить оценку",
  grading_failed: "оценка завершилась ошибкой",
  timeout: "результат не пришёл вовремя",
  no_new_grade: "новая оценка не появилась",
  no_activity: "нет новой активности с последней оценки",
}

export function itemErrorLabel(code: string): string {
  return Object.hasOwn(ITEM_ERRORS, code) ? ITEM_ERRORS[code] : code
}

const REASON_MESSAGES: Record<string, string> = {
  nothing_selected: "Под условия не подошёл ни один публичный репозиторий.",
  too_many: "Подошло больше репозиториев, чем допускает один пакет. Сузьте условия.",
  invalid_filter: "Условия заданы неверно. Проверьте поля формы.",
  batch_active: "Пакет уже выполняется. Дождитесь его завершения или отмените его.",
  bad_state: "Это действие сейчас недоступно: состояние пакета изменилось.",
  batch_not_found: "Пакет не найден.",
}

/** Russian wording of a refused batch request, or undefined to keep the generic message. */
export function batchErrorMessage(status: number, code: string): string | undefined {
  if (status === 401 || status === 403) return "Нет доступа. Войдите через Яндекс под учётной записью администратора."
  return Object.hasOwn(REASON_MESSAGES, code) ? REASON_MESSAGES[code] : undefined
}

/** Repositories handled so far (finished, failed or skipped) and the percentage of the batch. */
export function batchProgress(batch: Pick<RegradeBatchResponse, "total" | "ok" | "failed" | "skipped">) {
  const done = batch.ok + batch.failed + batch.skipped
  const percent = batch.total > 0 ? Math.min(100, Math.round((done / batch.total) * 100)) : 0
  return { done, percent }
}

export function isActive(batch: Pick<RegradeBatchResponse, "status">): boolean {
  return batch.status === "running" || batch.status === "paused"
}

/** A batch is followed while it can still change: active, or cancelled with repositories still in flight. */
export function shouldPoll(batch: Pick<RegradeBatchResponse, "status" | "running">): boolean {
  return isActive(batch) || batch.running > 0
}

/** "≈ 45 мин" for a rough duration; seconds below a minute read as "меньше минуты". */
export function formatEstimate(seconds: number): string {
  if (seconds < 60) return "меньше минуты"
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `≈ ${minutes} мин`
  const hours = Math.floor(minutes / 60)
  const rest = minutes % 60
  return rest === 0 ? `≈ ${hours} ч` : `≈ ${hours} ч ${rest} мин`
}

/** The chosen conditions as one line, for the history. */
export function filterSummary(filter: RegradeBatchResponse["filter"]): string {
  const forced = filter.force ? "; принудительно" : ""
  if (filter.all) return `Все публичные репозитории${forced}`

  const parts: string[] = []
  if (filter.top_rated != null) parts.push(`топ ${filter.top_rated} по рейтингу SourceCraft`)
  if (filter.languages?.length) parts.push(`языки: ${filter.languages.join(", ")}`)
  if (filter.min_score != null && filter.max_score != null) parts.push(`оценка ${filter.min_score}–${filter.max_score}`)
  else if (filter.min_score != null) parts.push(`оценка от ${filter.min_score}`)
  else if (filter.max_score != null) parts.push(`оценка до ${filter.max_score}`)
  if (filter.older_than_days != null) parts.push(`оценке больше ${filter.older_than_days} дн. или её нет`)
  if (filter.repos?.length) parts.push(`список: ${filter.repos.length}`)

  return parts.length > 0 ? parts.join("; ") + forced : "—"
}
