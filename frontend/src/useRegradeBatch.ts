import { useCallback, useEffect, useRef, useState } from "react"
import type { HttpRepoHealthApi } from "./api/client"
import type { RegradeBatchDetailResponse, RegradeBatchResponse } from "./api/contracts"
import { shouldPoll } from "./regradeView"

/** How often the active batch is refreshed while it runs. */
const POLL_MS = 3000

export interface RegradeBatchState {
  /** The active batch, or the latest finished one; undefined when there has never been one. */
  detail?: RegradeBatchDetailResponse
  history: RegradeBatchResponse[]
  isLoading: boolean
  error?: string
}

/**
 * The batch regrade state of the admin tab: the current (or latest) batch, polled while it is active,
 * and the recent history. `refresh` reloads both, e.g. after a start or a control action.
 */
export function useRegradeBatch(api: HttpRepoHealthApi) {
  const [state, setState] = useState<RegradeBatchState>({ history: [], isLoading: true })
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
  const alive = useRef(true)

  const load = useCallback(async () => {
    let detail: RegradeBatchDetailResponse | undefined
    let history: RegradeBatchResponse[] = []
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
        history = (await api.listBatches(10)).batches
      } catch (failure) {
        error = failure instanceof Error ? failure.message : "Не удалось загрузить историю пакетов."
      }
    }

    if (!alive.current) return
    setState({ detail, history, isLoading: false, error })

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
