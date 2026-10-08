import type { StatsResponse } from "./api/contracts"
import { categoryLabel, metricLabel } from "./metrics"

export interface BarItem {
  key: string
  label: string
  value: number
  /** 0–100 share used as the bar width. */
  width: number
  note: string
}

/** Share of `part` in `whole` as a whole percent; 0 when there is nothing to divide by. */
export function percentOf(part: number, whole: number): number {
  return whole > 0 ? Math.round((part / whole) * 100) : 0
}

/** Ten-point score buckets for the distribution chart; a bar's height is relative to the tallest. */
export function distributionBars(stats: StatsResponse): BarItem[] {
  const tallest = Math.max(1, ...stats.distribution.map((bucket) => bucket.repos))
  return stats.distribution.map((bucket) => ({
    key: String(bucket.min),
    label: bucket.max >= 100 ? `${bucket.min}–100` : `${bucket.min}–${bucket.max}`,
    value: bucket.repos,
    width: Math.round((bucket.repos / tallest) * 100),
    note: `${bucket.repos} из ${stats.graded_repos}`,
  }))
}

const LEVEL_LABELS: Record<string, string> = {
  excellent: "Отличное состояние",
  healthy: "Здоровый",
  attention: "Требует внимания",
  at_risk: "Под риском",
}

export function levelItems(stats: StatsResponse) {
  return stats.levels.map((level) => ({
    key: level.level,
    label: LEVEL_LABELS[level.level] ?? level.level,
    repos: level.repos,
    share: percentOf(level.repos, stats.graded_repos),
  }))
}

/** Average score per language; the bar is the score itself (0–100), so bars compare across languages. */
export function languageBars(stats: StatsResponse): (BarItem & { color: string })[] {
  return stats.languages.map((language) => ({
    key: language.name,
    label: language.name,
    value: language.avg_score,
    width: Math.max(0, Math.min(100, Math.round(language.avg_score))),
    note: `${language.repos} репозиториев`,
    color: language.color,
  }))
}

export function categoryBars(stats: StatsResponse): BarItem[] {
  return stats.categories.map((category) => ({
    key: category.id,
    label: categoryLabel(category.id),
    value: category.avg_score,
    width: Math.max(0, Math.min(100, Math.round(category.avg_score))),
    note: `данные есть у ${category.repos} из ${stats.graded_repos}`,
  }))
}

/** Documentation deduction codes (docs/documentation-codes.md) as short Russian labels. */
const DOC_LABELS: Record<string, string> = {
  DOC_README_MISSING: "Нет README",
  DOC_README_EMPTY: "README пуст",
  DOC_PURPOSE_UNCLEAR: "Не ясно назначение проекта",
  DOC_CAPABILITIES_INCOMPLETE: "Возможности описаны неполно",
  DOC_LICENSE_MISSING: "Нет лицензии",
  DOC_LICENSE_EMPTY: "Файл лицензии пуст",
  DOC_CONTRIBUTING_MISSING: "Нет CONTRIBUTING",
  DOC_CODEOWNERS_MISSING: "Нет CODEOWNERS",
  DOC_LOCAL_RUN_MISSING: "Нет инструкции локального запуска",
  DOC_LOCAL_RUN_PREREQUISITES_INCOMPLETE: "Не описаны требования к запуску",
  DOC_LOCAL_RUN_DEPENDENCIES_INCOMPLETE: "Не описаны зависимости запуска",
  DOC_LOCAL_RUN_CONFIGURATION_INCOMPLETE: "Не описана конфигурация запуска",
  DOC_LOCAL_RUN_COMMANDS_INCOMPLETE: "Не хватает команд запуска",
  DOC_LOCAL_RUN_VERIFICATION_INCOMPLETE: "Нет проверки, что запуск удался",
  DOC_BUILD_INSTRUCTIONS_MISSING: "Нет описания сборки",
  DOC_BUILD_INSTRUCTIONS_INCOMPLETE: "Описание сборки неполное",
  DOC_TESTING_COMMANDS_INCOMPLETE: "Не описан запуск тестов",
  DOC_TESTING_SCOPE_UNCLEAR: "Не ясно, что покрывают тесты",
  DOC_STRUCTURE_OVERVIEW_MISSING: "Нет описания структуры проекта",
  DOC_STRUCTURE_OVERVIEW_INCOMPLETE: "Структура проекта описана неполно",
  DOC_USAGE_EXAMPLE_MISSING: "Нет примера использования",
  DOC_USAGE_EXAMPLE_INCOMPLETE: "Пример использования неполный",
  DOC_USAGE_INPUTS_UNCLEAR: "Не ясны входные данные",
  DOC_COMMAND_INCONSISTENT: "Команды в документах расходятся",
  DOC_COMPONENT_DESCRIPTION_INCONSISTENT: "Описание компонентов расходится с кодом",
  DOC_REFERENCE_NOT_FOUND: "Ссылка в документации не найдена",
}

/** Internal notices that are not recommendations and so never count as "common advice". */
const NOT_ADVICE = new Set(["DOC_ALGORITHMIC_NOTICE"])

export function adviceLabel(metricId: string): string {
  return DOC_LABELS[metricId] ?? metricLabel(metricId)
}

/** The most common advice items: label, how many repos carry it and what share of all graded repos. */
export function adviceItems(stats: StatsResponse) {
  return stats.top_advice.filter((item) => !NOT_ADVICE.has(item.metric_id)).map((item) => ({
    key: `${item.category_id}/${item.metric_id}`,
    category: categoryLabel(item.category_id),
    metric: adviceLabel(item.metric_id),
    priority: item.priority,
    repos: item.repos,
    share: percentOf(item.repos, stats.graded_repos),
  }))
}

/** Weekly averages as chart points; a week without a start time is dropped. */
export function trendPoints(stats: StatsResponse): { at: string; value: number }[] {
  return stats.trend.flatMap((point) => (point.week_start ? [{ at: point.week_start, value: point.avg_score }] : []))
}
