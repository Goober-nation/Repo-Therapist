import type { AdminMeResponse } from "./api/contracts"

/**
 * True when the interface still shows a signed-in user but the Gateway no longer knows their session
 * (it expired, or the server lost it). The saved profile is only display data kept in the tab, so it can
 * outlive the session; this is how the interface notices. An unknown status (not loaded, or the request
 * failed) is never a lost session.
 */
export function sessionLost(profile: unknown, status: AdminMeResponse | undefined): boolean {
  return Boolean(profile) && status !== undefined && !status.signed_in
}

/**
 * A value that was loaded for one identity (the signed-in profile, or none), returned only while that is
 * still the current identity. Right after a sign-in or sign-out the value of the previous identity is
 * stale, so it reads as "unknown" until the new one arrives.
 */
export function forIdentity<T>(loaded: { identity: unknown; value: T } | undefined, identity: unknown): T | undefined {
  return loaded !== undefined && loaded.identity === identity ? loaded.value : undefined
}
