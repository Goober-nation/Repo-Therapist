import { useCallback, useEffect, useRef, useState } from "react"
import type { HttpRepoHealthApi } from "./api/client"
import type { GradingBatchDetailResponse, GradingBatchResponse } from "./api/contracts"
import { shouldPoll } from "./gradingBatchView"

/** How often the active batch is refreshed while it runs. */
const POLL_MS = 3000

export interface GradingBatchState {
  /** The active batch, or the latest finished one; undefined when there has never been one. */
  detail?: GradingBatchDetailResponse
  history: GradingBatchResponse[]
  /** The stand's cap on one batch's limit; 0 means none. */
  maxLimit: number
  isLoading: boolean
  error?: string
}

/**
 * The batch grading state of the admin tab: the current (or latest) batch, polled while it is active,
 * and the recent history. `refresh` reloads both, e.g. after a start or a control action.
 */
export function useGradingBatch(api: HttpRepoHealthApi) {
  const [state, setState] = useState<GradingBatchState>({ history: [], maxLimit: 0, isLoading: true })
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
  const alive = useRef(true)

  const load = useCallback(async () => {
    let detail: GradingBatchDetailResponse | undefined
    let history: GradingBatchResponse[] = []
    let maxLimit = 0
    let error: string | undefined

    try {
      // A fresh stand has no batch yet: the answer is then 200 with a null batch.
      const answer = await api.getCurrentBatch()
      detail = answer.batch ? { batch: answer.batch, failures: answer.failures } : undefined
    } catch (failure) {
      error = failure instanceof Error ? failure.message : "Не удалось загрузить пакеты."
    }

    if (!error) {
      try {
        const list = await api.listBatches(10)
        history = list.batches
        maxLimit = list.max_limit ?? 0
      } catch (failure) {
        error = failure instanceof Error ? failure.message : "Не удалось загрузить историю пакетов."
      }
    }

    if (!alive.current) return
    setState({ detail, history, maxLimit, isLoading: false, error })

    clearTimeout(timer.current)
    if (!error && detail && shouldPoll(detail.batch)) timer.current = setTimeout(load, POLL_MS)
  }, [api])

  useEffect(() => {
    alive.current = true
    void load()
    return () => {
      alive.current = false
      clearTimeout(timer.current)
    }
  }, [load])

  return { ...state, refresh: load }
}
