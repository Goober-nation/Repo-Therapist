import { useCallback, useEffect, useState } from "react"
import type { HttpRepoHealthApi } from "./api/client"
import type { AuditEntryResponse } from "./api/contracts"
import { AUDIT_PAGE_SIZE, FIRST_AUDIT_PAGE, nextAuditPage, previousAuditPage, type AuditCursors } from "./auditView"

export interface AuditState {
  entries: AuditEntryResponse[]
  /** The cursor of the next page; 0 on the last page. */
  nextBeforeId: number
  isLoading: boolean
  error?: string
}

/**
 * One page of the audit log, newest first, AUDIT_PAGE_SIZE entries at a time. `scope` is the admin
 * journal ("admin") or the user actions ("users"). Changing `outcome`, `action` or `attempt` (retry, or
 * a reload after a new entry) goes back to the first page; `next` and `previous` move between pages.
 */
export function useAudit(api: HttpRepoHealthApi, outcome: string, attempt: number, scope = "", action = "") {
  const key = `${scope}|${action}|${outcome}|${attempt}`
  const [nav, setNav] = useState<{ scope: string; cursors: AuditCursors }>({ scope: key, cursors: FIRST_AUDIT_PAGE })
  const [state, setState] = useState<AuditState>({ entries: [], nextBeforeId: 0, isLoading: true })

  // A new scope starts again from the first page, without an extra render of the old one.
  const cursors = nav.scope === key ? nav.cursors : FIRST_AUDIT_PAGE
  const beforeId = cursors[cursors.length - 1]

  useEffect(() => {
    let cancelled = false
    setState((current) => ({ ...current, isLoading: true, error: undefined }))

    api
      .getAudit({ outcome, beforeId, limit: AUDIT_PAGE_SIZE, scope, action })
      .then((page) => {
        if (!cancelled) setState({ entries: page.entries, nextBeforeId: page.next_before_id, isLoading: false })
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setState({
            entries: [],
            nextBeforeId: 0,
            isLoading: false,
            error: error instanceof Error ? error.message : "Не удалось загрузить журнал.",
          })
        }
      })

    return () => {
      cancelled = true
    }
  }, [api, outcome, beforeId, scope, action, key])

  const next = useCallback(() => {
    if (state.nextBeforeId > 0 && !state.isLoading) setNav({ scope: key, cursors: nextAuditPage(cursors, state.nextBeforeId) })
  }, [key, cursors, state.nextBeforeId, state.isLoading])

  const previous = useCallback(() => {
    if (!state.isLoading) setNav({ scope: key, cursors: previousAuditPage(cursors) })
  }, [key, cursors, state.isLoading])

  return { ...state, page: cursors.length, hasPrevious: cursors.length > 1, hasNext: state.nextBeforeId > 0, next, previous }
}
