import { useEffect, useState } from "react"
import type { HttpRepoHealthApi } from "./api/client"
import { regradeFromResponse } from "./api/mapper"
import type { RegradeStatus } from "./types"

/** While a run is going (or its start is overdue) the status is refreshed this often. */
const BUSY_POLL_MS = 15_000
/** Otherwise the schedule hardly changes; a refresh keeps the countdown honest after a restart. */
const IDLE_POLL_MS = 300_000

/**
 * The scheduled public regrade for the ranking page. Fetched when `active`, then refreshed on a
 * timer; a failed request keeps the last status and an unavailable endpoint shows nothing.
 */
export function useRegradeStatus(api: HttpRepoHealthApi, active: boolean): RegradeStatus | undefined {
  const [status, setStatus] = useState<RegradeStatus | undefined>()

  useEffect(() => {
    if (!active) return
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined

    const load = () => {
      api
        .getRegradeStatus()
        .then((response) => {
          if (cancelled) return
          const next = regradeFromResponse(response)
          setStatus(next)
          const overdue = next?.nextRunAt !== undefined && Date.parse(next.nextRunAt) <= Date.now()
          timer = setTimeout(load, next?.running || overdue ? BUSY_POLL_MS : IDLE_POLL_MS)
        })
        .catch(() => {
          if (!cancelled) timer = setTimeout(load, IDLE_POLL_MS)
        })
    }

    load()
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [api, active])

  return status
}
