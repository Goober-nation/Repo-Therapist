import type { CatalogRefreshResponse, CatalogRefreshStatus } from "./api/contracts"
import type { Tone } from "./metrics"

const STATUS_LABELS: Record<CatalogRefreshStatus, { label: string; tone: Tone }> = {
  running: { label: "Идёт", tone: "good" },
  completed: { label: "Готово", tone: "good" },
  failed: { label: "Ошибка", tone: "critical" },
  cancelled: { label: "Остановлено", tone: "neutral" },
}

export function catalogStatusLabel(status: string): { label: string; tone: Tone } {
  return Object.hasOwn(STATUS_LABELS, status) ? STATUS_LABELS[status as CatalogRefreshStatus] : { label: status, tone: "neutral" }
}

const ERRORS: Record<string, string> = {
  wrapper_error: "SourceCraft не ответил на запрос страницы.",
  too_many_pages: "Список оказался длиннее допустимого числа страниц.",
  page_loop: "SourceCraft вернул уже пройденную страницу.",
  store_error: "Не удалось сохранить список в базе данных.",
  interrupted: "Сервис перезапустился во время обновления.",
  empty_listing: "SourceCraft вернул пустой список.",
}

/** Why a refresh failed, in Russian; the code itself when it is unknown. */
export function catalogErrorLabel(code: string): string {
  return Object.hasOwn(ERRORS, code) ? ERRORS[code] : code
}

/** Who started a refresh: the service itself for the first fill of an empty catalog, otherwise an admin. */
export function catalogTriggerLabel(refresh: Pick<CatalogRefreshResponse, "trigger">): string {
  return refresh.trigger === "startup" ? "автоматически при запуске" : "администратором"
}

export function isRefreshRunning(refresh: Pick<CatalogRefreshResponse, "status"> | null | undefined): boolean {
  return refresh?.status === "running"
}
