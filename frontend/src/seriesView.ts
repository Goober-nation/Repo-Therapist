import type { SeriesLineResponse } from "./api/contracts"

export type SeriesUnit = "count" | "seconds" | "per_second" | string

export interface SeriesInfo {
  name: string
  title: string
  description: string
}

/** The graphs of the Analytics tab: built from public gradings only. */
export const PUBLIC_SERIES: SeriesInfo[] = [
  {
    name: "gradings_per_day",
    title: "Оценок в сутки",
    description: "Сколько публичных репозиториев оценено за 24 часа до каждой точки; последняя точка — последние 24 часа.",
  },
  {
    name: "grading_avg_seconds",
    title: "Среднее время оценки",
    description: "Сколько в среднем проходит от запроса оценки до результата за 24 часа до каждой точки.",
  },
]

/** The graphs of the admin tab, in display order. Only counts and timings: no repositories, no people. */
export const ADMIN_SERIES: SeriesInfo[] = [
  { name: "http_rate", title: "Запросы к сайту", description: "Запросов в секунду по классам ответа." },
  { name: "http_latency_p95", title: "Время ответа сайта", description: "95% запросов быстрее этого времени." },
  { name: "grpc_errors", title: "Сбои между сервисами", description: "Вызовов с ошибкой в секунду, по сервису." },
  { name: "gradings_by_trigger", title: "Запуски оценок", description: "Сколько оценок запущено за час: пользователем, по расписанию, пакетом." },
  { name: "grading_outcomes", title: "Результаты оценок", description: "Сколько оценок завершилось за час, успешно или с ошибкой." },
  { name: "grading_p95_seconds", title: "Время оценки", description: "95% оценок завершаются быстрее этого времени." },
  { name: "limiter_rejections", title: "Отказы по лимиту оценок", description: "Сколько оценок отклонено общим лимитом за час." },
  { name: "llm_requests", title: "Запросы к LLM", description: "Запросы рекомендаций и ответы за час." },
  { name: "batch_items", title: "Пакетная переоценка", description: "Репозиториев пакетов по итогу за час." },
  { name: "admin_denied", title: "Отказы администратору", description: "Попытки зайти в админские маршруты без прав, за час." },
]

/**
 * Use of the language model, from the tokens the provider reports for each call. Tokens, not money: no
 * prices are configured or guessed. `fixedRange` graphs have their own window and ignore the period choice.
 */
export const LLM_SERIES: (SeriesInfo & { fixedRange?: boolean })[] = [
  {
    name: "llm_tokens_per_day",
    title: "Токены за сутки",
    description: "Сколько токенов израсходовано за 24 часа до каждой точки, последние две недели.",
    fixedRange: true,
  },
  { name: "llm_tokens", title: "Токены за час", description: "Входные и выходные токены за час; «из кэша» — часть входных." },
  { name: "llm_tokens_by_purpose", title: "На что уходят токены", description: "Рекомендации по репозиторию и оценка документации." },
  { name: "llm_calls", title: "Вызовы модели", description: "Вызовы за час по итогу; повтор после неверного формата — отдельный вызов." },
  { name: "llm_call_p95_seconds", title: "Время вызова модели", description: "95% вызовов быстрее этого времени." },
]

export const ADMIN_RANGES = [
  { value: "1h", label: "Час" },
  { value: "6h", label: "6 часов" },
  { value: "24h", label: "Сутки" },
  { value: "7d", label: "Неделя" },
]

export const DEFAULT_ADMIN_RANGE = "24h"

/** Russian names of the label values of the groupings; unknown values are shown as they are. */
const LABELS: Record<string, Record<string, string>> = {
  gradings_by_trigger: { user: "по запросу пользователя", schedule: "по расписанию", batch: "пакетом" },
  grading_outcomes: { ok: "успешно", error: "с ошибкой" },
  llm_requests: {
    requested: "запросов",
    publish_failed: "не отправлено",
    received_ok: "ответов",
    received_error: "ответов с ошибкой",
  },
  batch_items: { ok: "успешно", failed: "с ошибкой", skipped: "пропущено" },
  admin_denied: {
    no_session: "нет сессии",
    invalid_session: "сессия недействительна",
    not_admin: "не администратор",
  },
  grpc_errors: { aggregator: "Aggregator", rating: "Rating" },
  llm_tokens: { input: "входные", output: "выходные", cached_input: "из них из кэша" },
  llm_tokens_per_day: { input: "входные", output: "выходные", cached_input: "из них из кэша" },
  llm_tokens_by_purpose: { summary: "рекомендации", documentation: "документация" },
  llm_calls: {
    ok: "успешно",
    rate_limited: "лимит провайдера",
    content_filtered: "отклонено фильтром",
    timeout: "не уложился во время",
    error: "ошибка",
  },
}

/** The legend text of a curve; an ungrouped query is named by its graph. */
export function lineLabel(name: string, label: string, fallback: string): string {
  if (label === "") return fallback

  const names = Object.hasOwn(LABELS, name) ? LABELS[name] : undefined

  return names && Object.hasOwn(names, label) ? names[label] : label
}

const decimal = (value: number, digits: number) => value.toFixed(digits).replace(".", ",")

/** A value with its unit, in Russian number style: "12", "45 с", "2,5 мин", "0,12 /с". */
export function formatValue(unit: SeriesUnit, value: number): string {
  if (!Number.isFinite(value)) return "—"

  switch (unit) {
    case "seconds":
      if (value < 1) return `${Math.round(value * 1000)} мс`
      if (value < 90) return `${decimal(value, value < 10 ? 1 : 0)} с`
      if (value < 5400) return `${decimal(value / 60, 1)} мин`
      return `${decimal(value / 3600, 1)} ч`
    case "per_second":
      // Rates span orders of magnitude: keep enough digits that neighbouring axis ticks stay distinct.
      if (value === 0) return "0 /с"
      if (value >= 10) return `${Math.round(value)} /с`
      return `${decimal(value, value >= 1 ? 1 : value >= 0.1 ? 2 : 3)} /с`
    case "tokens":
      // Whole tokens, grouped by thousands with non-breaking spaces: 1 234 567.
      return String(Math.round(value)).replace(/\B(?=(\d{3})+(?!\d))/g, "\u00a0")
    default:
      return Number.isInteger(value) || value >= 100 ? String(Math.round(value)) : decimal(value, 1)
  }
}

/** The smallest of 1, 2, 2.5, 5, 10 × 10^k that is at least `value`: a round step between axis ticks. */
export function niceStep(value: number): number {
  if (!(value > 0) || !Number.isFinite(value)) return 1

  const power = Math.pow(10, Math.floor(Math.log10(value)))
  const unit = value / power

  return ([1, 2, 2.5, 5, 10].find((step) => unit <= step) ?? 10) * power
}

/** Step between ticks so that four steps cover `max`; counts of events and tokens move in whole numbers. */
export function tickStep(unit: SeriesUnit, max: number): number {
  const step = niceStep(max / 4)

  return unit === "count" || unit === "tokens" ? Math.max(1, step) : step
}

export interface ChartGeometry {
  width: number
  height: number
  pad: { left: number; right: number; top: number; bottom: number }
}

export interface ChartModel {
  yMax: number
  yTicks: { value: number; y: number }[]
  tMin: number
  tMax: number
  lines: { label: string; coords: { x: number; y: number; value: number; at: number }[] }[]
}

/**
 * Positions the curves: x by real time (so a gap is a gap), y from 0 in four round steps. Returns
 * undefined when there is nothing to draw.
 */
export function chartModel(
  lines: { label: string; points: [number, number][] }[],
  geometry: ChartGeometry,
  unit: SeriesUnit = "",
): ChartModel | undefined {
  const all = lines.flatMap((line) => line.points)
  if (all.length === 0) return undefined

  const times = all.map(([at]) => at)
  const tMin = Math.min(...times)
  const tMax = Math.max(...times)
  const yMax = tickStep(unit, Math.max(...all.map(([, value]) => value))) * 4

  const { width, height, pad } = geometry
  const plotWidth = width - pad.left - pad.right
  const plotHeight = height - pad.top - pad.bottom
  const x = (at: number) => pad.left + (tMax === tMin ? plotWidth / 2 : ((at - tMin) / (tMax - tMin)) * plotWidth)
  const y = (value: number) => pad.top + plotHeight - (Math.max(0, Math.min(yMax, value)) / yMax) * plotHeight

  return {
    yMax,
    yTicks: [0, 1, 2, 3, 4].map((step) => ({ value: (yMax / 4) * step, y: y((yMax / 4) * step) })),
    tMin,
    tMax,
    lines: lines.map((line) => ({
      label: line.label,
      coords: line.points.map(([at, value]) => ({ x: x(at), y: y(value), value, at })),
    })),
  }
}

/** The label under the x axis: a date for a daily graph, a time of day otherwise. */
export function formatAxisTime(unixSeconds: number, stepSeconds: number): string {
  const date = new Date(unixSeconds * 1000)

  if (stepSeconds >= 86400) return date.toLocaleDateString("ru-RU", { day: "2-digit", month: "2-digit" })

  const time = date.toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" })

  return stepSeconds >= 3600 ? `${date.toLocaleDateString("ru-RU", { day: "2-digit", month: "2-digit" })} ${time}` : time
}

/** Wording of a refused graph request, or undefined to keep the generic message. */
export function seriesErrorMessage(status: number, code: string, scope: "public" | "admin"): string | undefined {
  if (code === "metrics_off") {
    return scope === "admin"
      ? "Графики выключены на этом стенде: не задан адрес хранилища метрик (PROMETHEUS_URL)."
      : "Графики работы сервиса сейчас недоступны."
  }

  if (code === "metrics_unavailable") {
    return scope === "admin" ? "Хранилище метрик не отвечает. Проверьте, что Prometheus запущен." : "Графики работы сервиса сейчас недоступны."
  }

  if (scope === "admin" && (status === 401 || status === 403)) {
    return "Нет доступа. Войдите через Яндекс под учётной записью администратора."
  }

  if (status === 404) return "Такого графика нет."

  return undefined
}

/**
 * The curves of a response with their Russian legend text. Curves without points are dropped, and so are
 * curves that are zero throughout when another curve has data: a legend of five outcomes of which four
 * never happened says nothing. If everything is zero the curves stay, so "nothing happened" still shows.
 */
export function chartLines(name: string, fallback: string, lines: SeriesLineResponse[]) {
  const withPoints = lines.filter((line) => line.points.length > 0)
  const active = withPoints.filter((line) => line.points.some(([, value]) => value !== 0))
  const shown = active.length > 0 ? active : withPoints

  return shown.map((line) => ({ label: lineLabel(name, line.label, fallback), points: line.points }))
}
