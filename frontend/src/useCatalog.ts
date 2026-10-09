import { useCallback, useEffect, useRef, useState } from "react"
import type { HttpRepoHealthApi } from "./api/client"
import type { CatalogStatusResponse } from "./api/contracts"
import { isRefreshRunning } from "./catalogView"

/** How often the catalog is reloaded while a refresh runs. */
const POLL_MS = 3000

export interface CatalogState {
  status?: CatalogStatusResponse
  isLoading: boolean
  error?: string
}

/** The public repository catalog of the admin tab, polled while a refresh runs. */
export function useCatalog(api: HttpRepoHealthApi) {
  const [state, setState] = useState<CatalogState>({ isLoading: true })
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
  const alive = useRef(true)

  const load = useCallback(async () => {
    let status: CatalogStatusResponse | undefined
    let error: string | undefined

    try {
      status = await api.getCatalog()
    } catch (failure) {
      error = failure instanceof Error ? failure.message : "Не удалось загрузить список репозиториев."
    }

    if (!alive.current) return
    setState({ status, isLoading: false, error })

    clearTimeout(timer.current)
    if (!error && isRefreshRunning(status?.current)) timer.current = setTimeout(load, POLL_MS)
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
