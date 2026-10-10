import type { AuditEntryResponse } from "./api/contracts"
import type { Tone } from "./metrics"

const ACTOR_LABELS: Record<string, string> = {
  admin: "Администратор",
  user: "Пользователь",
  anonymous: "Аноним",
  system: "Система",
}

const ACTION_LABELS: Record<string, string> = {
  "admin.denied": "Отказ в доступе администратора",
  "admin.unlock": "Вход по паролю администратора",
  "admin.lock": "Снятие прав администратора",
  // Entries written before the rename keep their labels.
  "regrade.batch.start": "Запуск пакетной переоценки",
  "regrade.batch.pause": "Пауза пакетной переоценки",
  "regrade.batch.resume": "Продолжение пакетной переоценки",
  "regrade.batch.cancel": "Отмена пакетной переоценки",
  "regrade.batch.complete": "Пакетная переоценка завершена",
  "batch_grading.start": "Запуск пакетной оценки",
  "batch_grading.pause": "Пауза пакетной оценки",
  "batch_grading.resume": "Продолжение пакетной оценки",
  "batch_grading.cancel": "Отмена пакетной оценки",
  "batch_grading.complete": "Пакетная оценка завершена",
  "catalog.refresh.start": "Запуск обновления списка репозиториев",
  "catalog.refresh.cancel": "Остановка обновления списка репозиториев",
  "catalog.refresh.complete": "Список репозиториев обновлён",
  "catalog.refresh.fail": "Обновление списка репозиториев не удалось",
  "grading.request": "Запрос оценки репозитория",
  "grading.force": "Принудительная переоценка репозитория",
  "recommendations.request": "Запрос рекомендаций от модели",
}

const OUTCOME_LABELS: Record<string, { label: string; tone: Tone }> = {
  ok: { label: "Успешно", tone: "good" },
  denied: { label: "Отказ", tone: "warning" },
  error: { label: "Ошибка", tone: "critical" },
}

const TARGET_LABELS: Record<string, string> = {
  repo: "Репозиторий",
  batch: "Пакет",
  route: "Маршрут",
  catalog: "Обновление списка",
}

const REASON_LABELS: Record<string, string> = {
  no_session: "нет сессии",
  invalid_session: "сессия недействительна",
  not_admin: "не администратор",
  wrong_password: "неверный пароль",
  locked: "вход заблокирован после неверных паролей",
  grading_limit: "достигнут общий лимит оценок",
  no_access: "нет доступа к репозиторию",
  not_found: "репозиторий не найден",
  interrupted: "оценка прервана",
  failed: "сбой при выполнении",
  wrapper_error: "SourceCraft не ответил",
  too_many_pages: "список длиннее допустимого",
  page_loop: "SourceCraft зациклил страницы",
  store_error: "сбой базы данных",
  empty_listing: "SourceCraft вернул пустой список",
}

const STATE_LABELS: Record<string, string> = {
  ready: "оценка готова",
  pending: "оценка ещё считается",
}

const VIA_LABELS: Record<string, string> = {
  password: "по паролю",
  logout: "при выходе из аккаунта",
}

export interface AuditRow {
  id: number
  at: string
  actorKind: string
  actor: string
  action: string
  target: string
  outcome: { label: string; tone: Tone }
  note: string
}

/** The outcome filter of the log: the value sent to the Gateway and its label. */
export const OUTCOME_FILTERS = [
  { value: "", label: "Все результаты" },
  { value: "ok", label: "Успешно" },
  { value: "denied", label: "Отказ" },
  { value: "error", label: "Ошибка" },
]

/** The action filter of the user-action block. */
export const USER_ACTION_FILTERS = [
  { value: "", label: "Все действия" },
  { value: "grading.request", label: "Запросы оценки" },
  { value: "grading.force", label: "Принудительные переоценки" },
  { value: "recommendations.request", label: "Запросы рекомендаций" },
]

export function actionLabel(action: string): string {
  return Object.hasOwn(ACTION_LABELS, action) ? ACTION_LABELS[action] : action
}

/** The `reason` (or, for a success, `via`) of an entry's details as text; other details are not shown. */
export function reasonLabel(details: Record<string, unknown> | null | undefined): string {
  const reason = details?.reason
  if (typeof reason === "string") return Object.hasOwn(REASON_LABELS, reason) ? REASON_LABELS[reason] : reason

  const via = details?.via
  if (typeof via === "string") return Object.hasOwn(VIA_LABELS, via) ? VIA_LABELS[via] : via

  const state = details?.state
  if (typeof state === "string") return Object.hasOwn(STATE_LABELS, state) ? STATE_LABELS[state] : state

  return ""
}

/** A request with an access token may be about a private repository, so the log keeps only a hash of it. */
export function accessNote(details: Record<string, unknown> | null | undefined): string {
  return details?.access === "token" ? "по токену, репозиторий скрыт хешем" : ""
}

export function auditRow(entry: AuditEntryResponse): AuditRow {
  const target = entry.target ? `${TARGET_LABELS[entry.target_kind] ?? entry.target_kind}: ${entry.target}` : ""
  return {
    id: entry.id,
    at: entry.at,
    actorKind: ACTOR_LABELS[entry.actor_kind] ?? entry.actor_kind,
    actor: entry.actor,
    action: actionLabel(entry.action),
    target,
    outcome: OUTCOME_LABELS[entry.outcome] ?? { label: entry.outcome, tone: "neutral" },
    note: [reasonLabel(entry.details), accessNote(entry.details)].filter(Boolean).join(" · "),
  }
}

/** The log is shown ten entries at a time. */
export const AUDIT_PAGE_SIZE = 10

/**
 * Paging state of the log: the `before_id` cursor each visited page was loaded with, oldest visit first
 * (page 1 starts from the newest entry, cursor 0). The Gateway pages by cursor, so "next" needs the
 * cursor its last answer gave and "previous" is a step back through the cursors already used.
 */
export type AuditCursors = readonly number[]

export const FIRST_AUDIT_PAGE: AuditCursors = [0]

/** One page forward; unchanged when the last answer said there are no more entries (cursor 0). */
export function nextAuditPage(cursors: AuditCursors, nextBeforeId: number): AuditCursors {
  return nextBeforeId > 0 ? [...cursors, nextBeforeId] : cursors
}

/** One page back; never before the first page. */
export function previousAuditPage(cursors: AuditCursors): AuditCursors {
  return cursors.length > 1 ? cursors.slice(0, -1) : cursors
}
