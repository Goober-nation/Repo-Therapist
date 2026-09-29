const PAT_KEY = "repo-health-pat"
const PAT_TTL_MS = 60 * 60 * 1000

interface StoredPat {
  pat: string
  expiresAt: number
}

/** Returns the saved SourceCraft token if it has not expired; drops an expired one. */
export function loadPat(): string | undefined {
  try {
    const raw = window.localStorage.getItem(PAT_KEY)
    if (!raw) return undefined
    const stored = JSON.parse(raw) as StoredPat
    if (typeof stored.pat === "string" && stored.pat && stored.expiresAt > Date.now()) return stored.pat
    window.localStorage.removeItem(PAT_KEY)
  } catch {
    // Unavailable or corrupt storage: the token lives in memory only.
  }
  return undefined
}

/** Saves a new token for one hour from now, or clears it when undefined. Re-saving the
 * already stored token keeps its original expiry, so reloads do not extend it. */
export function savePat(pat: string | undefined) {
  try {
    if (pat && pat === loadPat()) return
    if (pat) window.localStorage.setItem(PAT_KEY, JSON.stringify({ pat, expiresAt: Date.now() + PAT_TTL_MS }))
    else window.localStorage.removeItem(PAT_KEY)
  } catch {
    // Storage can be unavailable (private mode); the token still works for this visit.
  }
}

/** Milliseconds until the saved token expires, or undefined when nothing is saved. */
export function patExpiresIn(): number | undefined {
  try {
    const raw = window.localStorage.getItem(PAT_KEY)
    if (!raw) return undefined
    return Math.max(0, (JSON.parse(raw) as StoredPat).expiresAt - Date.now())
  } catch {
    return undefined
  }
}
