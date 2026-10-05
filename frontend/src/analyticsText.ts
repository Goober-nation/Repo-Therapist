/** Wording of the analytics blocks, shared by the page and the report exports. */

export function plural(value: number, forms: [string, string, string]): string {
  const tens = value % 100
  const ones = value % 10
  if (tens >= 11 && tens <= 14) return forms[2]
  if (ones === 1) return forms[0]
  if (ones >= 2 && ones <= 4) return forms[1]
  return forms[2]
}

export const REVIEW_NO_DATA: Record<string, string> = {
  few_merged: "Нет данных: среди последних pull request'ов слишком мало влитых, чтобы судить о ревью.",
  imported: "Нет данных: pull request'ы созданы почти одновременно, похоже на импорт истории.",
  unavailable: "Нет данных: pull request'ы этого репозитория получить не удалось.",
  few_reviewed: "Нет данных: слишком мало pull request'ов с ревью, чтобы назвать типичное время ожидания.",
}

export const ISSUE_NO_DATA: Record<string, string> = {
  few_authors: "Нет данных: задачи открыли слишком мало разных людей — похоже, команда ведёт трекер для себя, а не принимает обращения.",
  few_issues: "Нет данных: среди последних задач слишком мало, чтобы судить о скорости ответа.",
  imported: "Нет данных: задачи созданы почти одновременно, похоже на импорт трекера.",
  unavailable: "Нет данных: комментарии к задачам получить не удалось.",
}

export function formatHours(hours: number): string {
  if (hours < 1) return "менее часа"
  if (hours < 10) return `${(Math.round(hours * 10) / 10).toLocaleString("ru-RU")} ч`
  if (hours < 48) return `${Math.round(hours)} ч`
  return `${Math.round(hours / 24)} дн.`
}

