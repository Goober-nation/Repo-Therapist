import type { BatchFilterBody, BatchStatus, GradingBatchResponse } from "./api/contracts"
import type { Tone } from "./metrics"
import { parseRepositoryLink } from "./route"

/** What the admin typed into the batch form; every field is raw text. */
export interface BatchForm {
  languages: string
  minScore: string
  maxScore: string
  olderThanDays: string
  repos: string
  /** How many repositories at most; required. */
  limit: string
  /** Only repositories that already have a grade. */
  onlyRegrade: boolean
  /** Grade without the "new activity since the last grade" check. */
  force: boolean
}

export const EMPTY_BATCH_FORM: BatchForm = {
  languages: "",
  minScore: "",
  maxScore: "",
  olderThanDays: "",
  repos: "",
  limit: "",
  onlyRegrade: false,
  force: false,
}

/** Mirrors the Aggregator's limits (docs/admin.md). */
export const MAX_BATCH_LANGUAGES = 20
export const MAX_BATCH_REPOS = 200
const MAX_BATCH_DAYS = 3650
/** The wire format's bound (int32); no business limit applies to the batch size. */
const MAX_BATCH_LIMIT = 2_147_483_647
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

/** An own-list entry as "org/repo": a slug as typed, or a SourceCraft repository link; null when neither. */
function repoSlug(entry: string): string | null {
  if (REPO_PATTERN.test(entry)) return entry
  const link = parseRepositoryLink(entry)
  if (!link) return null
  const slug = `${link.org}/${link.repo}`
  return REPO_PATTERN.test(slug) ? slug : null
}

/**
 * Turns the form into the request body, or says in Russian what is wrong with it. maxLimit is the stand's
 * cap on the limit (BATCH_GRADING_MAX_LIMIT); 0 means none.
 */
export function buildFilter(form: BatchForm, maxLimit = 0): BuiltFilter {
  const limit = wholeNumber(form.limit, "Не больше репозиториев", 1, maxLimit > 0 ? maxLimit : MAX_BATCH_LIMIT)
  if (limit === undefined) return { problem: "Укажите, сколько репозиториев оценить." }
  if (typeof limit === "string") return { problem: limit }

  const filter: BatchFilterBody = { limit }

  const languages = words(form.languages, /[,;\n]+/)
  if (languages.length > MAX_BATCH_LANGUAGES) return { problem: `Языков не больше ${MAX_BATCH_LANGUAGES}.` }
  if (languages.some((language) => language.length > 64)) return { problem: "Название языка слишком длинное." }
  if (languages.length > 0) filter.languages = languages

  const repos: string[] = []
  const seenRepos = new Set<string>()
  for (const entry of words(form.repos, /[\s,;]+/)) {
    const slug = repoSlug(entry)
    if (!slug) return { problem: `«${entry}»: нужен вид организация/репозиторий или ссылка на репозиторий в SourceCraft.` }
    if (seenRepos.has(slug.toLowerCase())) continue
    seenRepos.add(slug.toLowerCase())
    repos.push(slug)
  }
  if (repos.length > MAX_BATCH_REPOS) return { problem: `Репозиториев в списке не больше ${MAX_BATCH_REPOS}.` }
  if (repos.length > 0) filter.repos = repos

  const min = wholeNumber(form.minScore, "Оценка от", 0, 100)
  const max = wholeNumber(form.maxScore, "Оценка до", 0, 100)
  const days = wholeNumber(form.olderThanDays, "Давность оценки", 1, MAX_BATCH_DAYS)
  for (const value of [min, max, days]) if (typeof value === "string") return { problem: value }
  if (typeof min === "number") filter.min_score = min
  if (typeof max === "number") filter.max_score = max
  if (typeof days === "number") filter.older_than_days = days
  if (typeof min === "number" && typeof max === "number" && min > max) return { problem: "Оценка «от» больше оценки «до»." }

  if (form.onlyRegrade) filter.only_regrade = true
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
  invalid_filter: "Условия заданы неверно. Проверьте поля формы.",
  limit_too_high: "Число репозиториев больше, чем разрешено на этом стенде.",
  batch_active: "Пакет уже выполняется. Дождитесь его завершения или отмените его.",
  bad_state: "Это действие сейчас недоступно: состояние пакета изменилось.",
  batch_not_found: "Пакет не найден.",
  refresh_active: "Список репозиториев уже обновляется. Дождитесь окончания или остановите обновление.",
  refresh_not_running: "Обновление списка уже закончилось.",
}

/** Russian wording of a refused batch request, or undefined to keep the generic message. */
export function batchErrorMessage(status: number, code: string): string | undefined {
  if (status === 401 || status === 403) return "Нет доступа. Войдите через Яндекс под учётной записью администратора."
  return Object.hasOwn(REASON_MESSAGES, code) ? REASON_MESSAGES[code] : undefined
}

/** Repositories handled so far (finished, failed or skipped) and the percentage of the batch. */
export function batchProgress(batch: Pick<GradingBatchResponse, "total" | "ok" | "failed" | "skipped">) {
  const done = batch.ok + batch.failed + batch.skipped
  const percent = batch.total > 0 ? Math.min(100, Math.round((done / batch.total) * 100)) : 0
  return { done, percent }
}

export function isActive(batch: Pick<GradingBatchResponse, "status">): boolean {
  return batch.status === "running" || batch.status === "paused"
}

/** A batch is followed while it can still change: active, or cancelled with repositories still in flight. */
export function shouldPoll(batch: Pick<GradingBatchResponse, "status" | "running">): boolean {
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
export function filterSummary(filter: GradingBatchResponse["filter"]): string {
  const forced = filter.force ? "; принудительно" : ""

  const parts: string[] = []
  // Batches started before the limit existed have 0 here.
  if (filter.limit > 0) parts.push(`не больше ${filter.limit} по рейтингу SourceCraft`)
  if (filter.only_regrade) parts.push("только переоценка")
  if (filter.languages?.length) parts.push(`языки: ${filter.languages.join(", ")}`)
  if (filter.min_score != null && filter.max_score != null) parts.push(`оценка ${filter.min_score}–${filter.max_score}`)
  else if (filter.min_score != null) parts.push(`оценка от ${filter.min_score}`)
  else if (filter.max_score != null) parts.push(`оценка до ${filter.max_score}`)
  if (filter.older_than_days != null) parts.push(`оценке больше ${filter.older_than_days} дн. или её нет`)
  if (filter.repos?.length) parts.push(`список: ${filter.repos.length}`)

  return parts.length > 0 ? parts.join("; ") + forced : "—"
}
