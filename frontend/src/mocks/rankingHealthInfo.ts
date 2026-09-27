import type { CategoryScores } from "../types"

export function getLowestCategoryScores(categories: CategoryScores) {
  return Object.entries(categories)
    .filter(
      ([, metric]) => metric.score !== null && Number.isFinite(metric.score),
    )
    .map(([key, metric]) => ({
      name: metric.name || key,
      score: metric.score as number,
    }))
    .sort(
      (left, right) =>
        left.score - right.score || left.name.localeCompare(right.name, "ru"),
    )
    .slice(0, 3)
}
