import {
  categoryLabel,
  formatDateTime,
  formatMetricValue,
  getLastActivityLabel,
  healthLevel,
  isPrivateAnalysis,
  metricLabel,
  missingCategories,
  PRIORITY_LABELS,
  splitStrengths,
} from "../metrics"
import type { AiReport, Recommendation, RepositorySummary } from "../types"

/** Everything a report shows, shared by the Markdown and PDF exports. */
export function reportContent(repository: RepositorySummary, recommendations: Recommendation[], ai?: AiReport) {
  const { strengths, weaknesses } = splitStrengths(repository.categoryScores)
  return {
    title: `Repo Therapist: отчёт о здоровье ${repository.fullName}`,
    link: repository.webUrl,
    gradedAt: formatDateTime(repository.gradedAt) ?? "ещё не выполнялся",
    mode: isPrivateAnalysis(repository) ? "приватный (все 6 категорий)" : "публичный",
    score: repository.healthScore === null ? "Нет данных" : `${repository.healthScore}/100`,
    level: healthLevel(repository.healthScore).label,
    activity: getLastActivityLabel(repository.lastActivityDaysAgo),
    summary: ai?.summary,
    strengths,
    weaknesses,
    categories: Object.entries(repository.categoryScores).map(([id, result]) => ({
      name: result.name ?? categoryLabel(id),
      score: result.score === null ? "Нет данных" : `${result.score}/100`,
      description: result.description,
      facts: (result.facts ?? []).map(
        (fact) =>
          `${metricLabel(fact.id)}: ${formatMetricValue(fact.id, fact.value)}${
            fact.score === null ? "" : ` (оценка ${Math.round(fact.score * 100)}/100)`
          }`,
      ),
    })),
    missing: missingCategories(repository),
    recommendations: recommendations.map((item) => ({
      title: item.title,
      priority: PRIORITY_LABELS[item.priority],
      description: item.description,
      location: item.location,
    })),
  }
}

export function reportFileName(repository: RepositorySummary, extension: "md" | "pdf") {
  const date = new Date().toISOString().slice(0, 10)
  return `${repository.fullName.replace(/[^\w.-]+/g, "-")}-repo-therapist-${date}.${extension}`
}

const bulletList = (items: string[], empty: string) => (items.length ? items.map((item) => `- ${item}`) : [empty])

export function buildReport(
  repository: RepositorySummary,
  recommendations: Recommendation[] = [],
  ai?: AiReport,
): string {
  const report = reportContent(repository, recommendations, ai)
  return [
    `# ${report.title}`,
    "",
    ...(report.link ? [`Репозиторий: ${report.link}`, ""] : []),
    `- Repo Health Score: ${report.score} (${report.level})`,
    `- Режим анализа: ${report.mode}`,
    `- Последний анализ: ${report.gradedAt}`,
    `- Последняя активность: ${report.activity}`,
    `- Язык: ${repository.language}`,
    ...(report.summary ? ["", "## Summary AI", "", report.summary] : []),
    "",
    "## Сильные стороны",
    ...bulletList(report.strengths, "Нет категорий с высокой оценкой."),
    "",
    "## Требует внимания",
    ...bulletList(report.weaknesses, "Слабых категорий не найдено."),
    "",
    "## Категории",
    ...(report.categories.length
      ? report.categories.flatMap((item) => [
          `- ${item.name}: ${item.score}${item.description ? ` — ${item.description}` : ""}`,
          ...item.facts.map((fact) => `  - ${fact}`),
        ])
      : ["Оценки по категориям отсутствуют."]),
    ...(report.missing.length
      ? ["", "## Нет данных", ...report.missing.map((item) => `- ${item.label} — ${item.reason}`)]
      : []),
    "",
    "## Рекомендации",
    ...(report.recommendations.length
      ? report.recommendations.map(
          (item) =>
            `- **${item.title}** (${item.priority.toLocaleLowerCase()})${item.location ? `, ${item.location}` : ""}: ${item.description}`,
        )
      : ["Рекомендаций для этого репозитория нет."]),
    "",
  ].join("\n")
}

export function downloadReport(repository: RepositorySummary, recommendations: Recommendation[], ai?: AiReport): void {
  const file = new Blob([buildReport(repository, recommendations, ai)], { type: "text/markdown;charset=utf-8" })
  const fileUrl = URL.createObjectURL(file)
  const link = document.createElement("a")
  link.href = fileUrl
  link.download = reportFileName(repository, "md")
  link.click()
  window.setTimeout(() => URL.revokeObjectURL(fileUrl), 0)
}
