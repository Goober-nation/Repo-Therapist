import { HttpRepoHealthApi } from "./client"

export const YANDEX_SDK_URL =
  "https://yastatic.net/s3/passport-sdk/autofill/v1/sdk-suggest-with-polyfills-latest.js"
export const YANDEX_TOKEN_PAGE = "/auth/yandex/token.html"

export interface YandexProfile {
  id: string
  name: string
}

export interface YandexSdk {
  init(
    oauth: YandexOAuthOptions,
    tokenPageOrigin: string,
    appearance: YandexButtonAppearance,
  ): Promise<YandexSdkInitResult>
}

interface YandexOAuthOptions {
  client_id: string;
  response_type: "token";
  redirect_uri: string;
}

interface YandexButtonAppearance {
  view: "button";
  parentId: string;
  buttonView: "main";
  buttonTheme: "light" | "dark";
  buttonSize: "s";
  buttonBorderRadius: number;
}

interface YandexSdkInitResult {
  status: string;
  handler?: () => Promise<unknown>;
}

declare global {
  interface Window {
    YaAuthSuggest?: YandexSdk
  }
}

const PROFILE_KEY = "repo-health-yandex-profile"

// Only display data is kept (per tab), never the OAuth token.
export function loadSavedProfile(): YandexProfile | null {
  try {
    const value = JSON.parse(sessionStorage.getItem(PROFILE_KEY) ?? "null")
    return value &&
      typeof value.id === "string" &&
      value.id &&
      typeof value.name === "string"
      ? { id: value.id, name: value.name }
      : null
  } catch {
    return null
  }
}

export function saveProfile(profile: YandexProfile | null): void {
  try {
    if (profile) sessionStorage.setItem(PROFILE_KEY, JSON.stringify(profile))
    else sessionStorage.removeItem(PROFILE_KEY)
  } catch {
    // Storage may be unavailable (private mode); sign-in still works in memory.
  }
}

let sdkPromise: Promise<YandexSdk> | undefined

export function loadYandexSdk(): Promise<YandexSdk> {
  if (window.YaAuthSuggest) return Promise.resolve(window.YaAuthSuggest)
  if (sdkPromise) return sdkPromise
  sdkPromise = new Promise<YandexSdk>((resolve, reject) => {
    const script = document.createElement("script")
    script.src = YANDEX_SDK_URL
    script.async = true
    script.referrerPolicy = "no-referrer"
    const timer = setTimeout(fail, 15000)
    function fail() {
      clearTimeout(timer)
      script.onload = null
      script.onerror = null
      script.remove()
      reject(
        new Error(
          "Не удалось загрузить вход через Яндекс. Попробуйте ещё раз.",
        ),
      )
    }
    script.onerror = fail
    script.onload = () => {
      if (!window.YaAuthSuggest) {
        fail()
        return
      }
      clearTimeout(timer)
      resolve(window.YaAuthSuggest)
    }
    document.head.appendChild(script)
  }).catch((error) => {
    sdkPromise = undefined
    throw error
  })
  return sdkPromise
}

export async function initializeYandexButton(
  sdk: YandexSdk,
  clientId: string,
  origin: string,
  parentId: string,
  theme: "light" | "dark",
): Promise<() => Promise<unknown>> {
  if (!clientId.trim()) throw new Error("Вход через Яндекс пока не настроен.")
  const canonicalOrigin = new URL(origin).origin
  const result = await sdk.init(
    {
      client_id: clientId.trim(),
      response_type: "token",
      redirect_uri: canonicalOrigin + YANDEX_TOKEN_PAGE,
    },
    canonicalOrigin,
    {
      view: "button",
      parentId,
      buttonView: "main",
      buttonTheme: theme,
      buttonSize: "s",
      buttonBorderRadius: 8,
    },
  )
  if (result.status !== "ok" || typeof result.handler !== "function") {
    throw new Error("Не удалось открыть вход через Яндекс. Попробуйте ещё раз.")
  }
  return result.handler
}

export const DEV_PROFILE: YandexProfile = { id: "dev", name: "Dev (bypass)" }

// Testing stands only: the gateway (DEV_AUTH_BYPASS_USER_ID) accepts any token, so no Yandex call.
export async function devSignIn(
  fetcher: typeof fetch = fetch.bind(globalThis),
): Promise<YandexProfile> {
  await new HttpRepoHealthApi({ baseUrl: "/api", fetcher }).loginWithYandex("dev")
  return DEV_PROFILE
}

export async function profileFromYandexResult(
  data: unknown,
  clientId: string,
  signal?: AbortSignal,
  fetcher: typeof fetch = fetch.bind(globalThis),
): Promise<YandexProfile> {
  if (
    !data ||
    typeof data !== "object" ||
    "error" in data ||
    !("access_token" in data) ||
    typeof data.access_token !== "string" ||
    !data.access_token
  ) {
    throw new Error("Вход через Яндекс отменён или не завершён.")
  }
  const controller = new AbortController()
  const abort = () => controller.abort()
  signal?.addEventListener("abort", abort, { once: true })
  if (signal?.aborted) controller.abort()
  const timer = setTimeout(abort, 15000)
  try {
    const response = await fetcher("/yandex-profile/info?format=json", {
      signal: controller.signal,
      credentials: "omit",
      headers: { Authorization: "OAuth " + data.access_token },
    })
    if (!response.ok) throw new Error("Не удалось получить профиль Яндекс ID.")
    const profile = await response.json()
    if (
      !profile ||
      typeof profile.id !== "string" ||
      !profile.id ||
      profile.client_id !== clientId.trim()
    ) {
      throw new Error("Яндекс вернул некорректный профиль.")
    }
    const name = [profile.display_name, profile.real_name, profile.login].find(
      (value) => typeof value === "string" && value.trim(),
    )
    const apiBaseUrl = "/api"
    await new HttpRepoHealthApi({
      baseUrl: apiBaseUrl,
      fetcher,
    }).loginWithYandex(data.access_token)
    // Keep only display data. The token is sent to Gateway only to establish its session.
    return { id: profile.id, name: name ?? "Пользователь Яндекса" }
  } catch {
    throw new Error(
      "Не удалось подтвердить вход через Яндекс. Попробуйте ещё раз.",
    )
  } finally {
    clearTimeout(timer)
    signal?.removeEventListener("abort", abort)
  }
}
