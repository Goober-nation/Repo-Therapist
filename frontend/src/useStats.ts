import { useEffect, useState } from "react"
import type { HttpRepoHealthApi } from "./api/client"
import type { StatsResponse } from "./api/contracts"

export interface StatsState {
  data?: StatsResponse
  isLoading: boolean
  error?: string
}

/** Loads the platform statistics when `active` (the analytics page is open); `attempt` forces a reload. */
export function useStats(api: HttpRepoHealthApi, active: boolean, attempt: number): StatsState {
  const [state, setState] = useState<StatsState>({ isLoading: false })

  useEffect(() => {
    if (!active) return
    let cancelled = false
    setState((current) => ({ ...current, isLoading: true, error: undefined }))

    api
      .getStats()
      .then((data) => {
        if (!cancelled) setState({ data, isLoading: false })
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setState((current) => ({
            data: current.data,
            isLoading: false,
            error: error instanceof Error ? error.message : "Не удалось загрузить аналитику.",
          }))
        }
      })

    return () => {
      cancelled = true
    }
  }, [api, active, attempt])

  return state
}
