import {
  DEFAULT_REPOSITORY_ID,
  DEMO_RECOMMENDATIONS,
  METRIC_LABELS,
  getHealthLabel,
  getLastActivityLabel,
} from "./demoData"
import type { RepositorySummary } from "../types"

export function downloadReport(repository: RepositorySummary): void {
  const metricLines = Object.entries(repository.categoryScores)
    .map(
      ([metricId, result]) =>
        `- ${METRIC_LABELS[(metricId as keyof typeof METRIC_LABELS)]}: ${result.score}/100`,
    )
    .join("\n")
  const recommendations =
    repository.id === DEFAULT_REPOSITORY_ID
      ? DEMO_RECOMMENDATIONS.map(
          (item) =>
            `- ${item.title} (+${item.projectedScore - item.currentScore} балла)`,
        ).join("\n")
      : "Подробные рекомендации для этого репозитория отсутствуют."
  const report = [
    `# Отчёт о здоровье ${repository.fullName}`,
    "",
    "> Отчёт Repo Health.",
    "",
    `- Repo Health Score: ${repository.healthScore}/100 (${getHealthLabel(repository.healthScore)})`,
    `- Последняя активность: ${getLastActivityLabel(repository.lastActivityDaysAgo)}`,
    `- Язык: ${repository.language}`,
    "",
    "## Категории",
    metricLines,
    "",
    "## Рекомендации",
    recommendations,
  ].join("\n")

  const file = new Blob([report], { type: "text/markdown;charset=utf-8" })
  const fileUrl = URL.createObjectURL(file)
  const link = document.createElement("a")
  link.href = fileUrl
  link.download = `${repository.id}-repo-health.md`
  link.click()
  window.setTimeout(() => URL.revokeObjectURL(fileUrl), 0)
}
