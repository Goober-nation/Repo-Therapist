import { useSyncExternalStore } from "react"
import type { PublicConfigResponse } from "./api/contracts"

/**
 * The SPA's runtime settings: the `frontend` section of config/app.yaml, served by the Gateway at
 * GET /api/config (docs/config.md). Read once per page load, so an app.yaml or admin change applies on
 * the next load without rebuilding the frontend. Until the answer arrives (or when it fails) the
 * defaults below apply.
 */
export interface PublicConfig {
  /** Origin for badge links; "" means the page origin. */
  publicUrl: string
  /** Public Yandex ID OAuth client ID; "" turns the Yandex button off. */
  yandexClientId: string
  /** Testing stands: the dev sign-in button instead of Yandex. */
  devAuthBypass: boolean
  /** How long a grading is polled before the page gives up. */
  pollTimeoutMs: number
}

export const DEFAULT_PUBLIC_CONFIG: PublicConfig = {
  publicUrl: "",
  yandexClientId: "",
  devAuthBypass: false,
  pollTimeoutMs: 15 * 60_000,
}

interface Snapshot {
  config: PublicConfig
  loaded: boolean
}

let snapshot: Snapshot = { config: DEFAULT_PUBLIC_CONFIG, loaded: false }
let pending: Promise<PublicConfig> | undefined
const listeners = new Set<() => void>()

function publish(next: Snapshot) {
  snapshot = next
  for (const listener of listeners) listener()
}

export function fromResponse(body: Partial<PublicConfigResponse>): PublicConfig {
  const poll = Number(body.poll_timeout_ms)
  return {
    publicUrl: typeof body.public_url === "string" ? body.public_url.trim() : "",
    yandexClientId: typeof body.yandex_client_id === "string" ? body.yandex_client_id.trim() : "",
    devAuthBypass: body.dev_auth_bypass === true,
    pollTimeoutMs: Number.isFinite(poll) && poll > 0 ? poll : DEFAULT_PUBLIC_CONFIG.pollTimeoutMs,
  }
}

/** Fetches /api/config once; a failure keeps the defaults and lets a later call try again. */
export function loadPublicConfig(fetcher: typeof fetch = (...args) => fetch(...args)): Promise<PublicConfig> {
  pending ??= fetcher("/api/config", { headers: { Accept: "application/json" } })
    .then(async (response) => {
      if (!response.ok) throw new Error(`config ${response.status}`)
      const config = fromResponse(await response.json())
      publish({ config, loaded: true })
      return config
    })
    .catch(() => {
      pending = undefined
      publish({ config: snapshot.config, loaded: true })
      return snapshot.config
    })
  return pending
}

/** The current settings, for code outside React (the grading poll). */
export function publicConfig(): PublicConfig {
  return snapshot.config
}

/** The settings and whether /api/config has answered; re-renders when it does. */
export function usePublicConfig(): Snapshot {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
    () => snapshot,
    () => snapshot,
  )
}
