import {
  DEFAULT_REPOSITORY_ID,
  DEMO_RECOMMENDATIONS,
  getHealthLabel,
  getLastActivityLabel,
} from "./demoData"
import type { Recommendation, RepositorySummary } from "../types"

export function buildReport(
  repository: RepositorySummary,
  recommendations: Recommendation[] = repository.id === DEFAULT_REPOSITORY_ID
    ? DEMO_RECOMMENDATIONS
    : [],
): string {
  const metricLines = Object.entries(repository.categoryScores)
    .map(
      ([metricId, result]) =>
        `- ${result.name ?? metricId}: ${
          result.score === null ? "Нет данных" : `${result.score}/100`
        }`,
    )
    .join("\n")
  const recommendationLines =
    recommendations.length > 0
      ? recommendations
          .map(
            (item) =>
              `- ${item.title} (+${item.projectedScore - item.currentScore} балла)`,
          )
          .join("\n")
      : "Подробные рекомендации для этого репозитория отсутствуют."
  return [
    `# Отчёт о здоровье ${repository.fullName}`,
    "",
    "> Отчёт Repo Health.",
    "",
    `- Repo Health Score: ${repository.healthScore}/100 (${getHealthLabel(repository.healthScore)})`,
    `- Последняя активность: ${getLastActivityLabel(repository.lastActivityDaysAgo)}`,
    `- Язык: ${repository.language}`,
    "",
    "## Категории",
    metricLines || "Оценки по категориям отсутствуют.",
    "",
    "## Рекомендации",
    recommendationLines,
  ].join("\n")
}

export function downloadReport(
  repository: RepositorySummary,
  recommendations?: Recommendation[],
): void {
  const file = new Blob([buildReport(repository, recommendations)], {
    type: "text/markdown;charset=utf-8",
  })
  const fileUrl = URL.createObjectURL(file)
  const link = document.createElement("a")
  link.href = fileUrl
  link.download = `${repository.id}-repo-health.md`
  link.click()
  window.setTimeout(() => URL.revokeObjectURL(fileUrl), 0)
}
