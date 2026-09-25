import type { CategoryScores } from "./types"

/** Optional lookups for screens with a specific meaning, without renaming categories. */
export function getCategoryScore(
  scores: CategoryScores,
  ...names: string[]
): number | null {
  const entry = Object.entries(scores).find(([name]) =>
    names.includes(name.trim().toLowerCase()),
  )
  return entry?.[1].score ?? null
}
