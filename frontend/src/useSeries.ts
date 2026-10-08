import { useEffect, useState } from "react"
import type { SeriesResponse } from "./api/contracts"

export interface SeriesState {
  data?: SeriesResponse
  isLoading: boolean
  error?: string
}

/**
 * Loads one graph with `load` whenever `key` changes (the graph, its range, a manual refresh). A failed
 * load keeps the error text and drops the old data, so a stale graph is never shown as current.
 */
export function useSeries(load: () => Promise<SeriesResponse>, key: string): SeriesState {
  const [state, setState] = useState<SeriesState>({ isLoading: true })

  useEffect(() => {
    let cancelled = false
    setState((current) => ({ data: current.data, isLoading: true }))

    load()
      .then((data) => {
        if (!cancelled) setState({ data, isLoading: false })
      })
      .catch((error: unknown) => {
        if (!cancelled) setState({ isLoading: false, error: error instanceof Error ? error.message : "Не удалось загрузить график." })
      })

    return () => {
      cancelled = true
    }
    // `load` is a fresh closure on every render; `key` says when the graph really changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key])

  return state
}
