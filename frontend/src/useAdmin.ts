import { useCallback, useEffect, useState } from "react"
import type { HttpRepoHealthApi } from "./api/client"
import type { AdminMeResponse } from "./api/contracts"
import { forIdentity } from "./session"

export interface AdminState {
  /** undefined while unknown: still loading for the current identity, or the request failed (see `error`). */
  status?: AdminMeResponse
  /** Why the status could not be loaded; cleared by a retry. */
  error?: string
  /** Asks the Gateway again, e.g. after a failure. */
  retry: () => void
  /** Replaces the status, e.g. with the answer of an unlock or lock. */
  setStatus: (status: AdminMeResponse) => void
}

/**
 * The signed-in user's admin rights. `identity` changes after a sign-in or sign-out, so the answer is
 * asked again then, and an answer loaded for another identity is never returned: right after a sign-in
 * the status is "unknown" until the new answer arrives, not the stale one of the visitor. An unlock or
 * lock sets the answer directly from its own response. A failed request is reported in `error` rather
 * than leaving the status "loading" forever.
 */
export function useAdmin(api: HttpRepoHealthApi, identity: unknown): AdminState {
  const [loaded, setLoaded] = useState<{ identity: unknown; value: AdminMeResponse } | undefined>()
  const [failure, setFailure] = useState<{ identity: unknown; value: string } | undefined>()
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let cancelled = false
    setFailure(undefined)

    api
      .getAdminMe()
      .then((response) => {
        if (!cancelled) setLoaded({ identity, value: response })
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setLoaded(undefined)
          setFailure({ identity, value: error instanceof Error ? error.message : "Не удалось проверить доступ." })
        }
      })

    return () => {
      cancelled = true
    }
  }, [api, identity, attempt])

  const setStatus = useCallback(
    (next: AdminMeResponse) => {
      setFailure(undefined)
      setLoaded({ identity, value: next })
    },
    [identity],
  )
  const retry = useCallback(() => setAttempt((value) => value + 1), [])

  return {
    status: forIdentity(loaded, identity),
    error: forIdentity(failure, identity),
    retry,
    setStatus,
  }
}
