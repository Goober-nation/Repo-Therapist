import { useEffect, useRef, useState } from "react"
import {
  devSignIn,
  initializeYandexButton,
  loadYandexSdk,
  profileFromYandexResult,
  type YandexProfile,
} from "../api/yandexAuth"
import { usePublicConfig } from "../publicConfig"

export default function YandexSignIn({
  theme,
  onSignIn,
}: {
  theme: "light" | "dark"
  onSignIn: (profile: YandexProfile) => void
}) {
  const container = useRef<HTMLDivElement>(null)
  const [attempt, setAttempt] = useState(0)
  const [status, setStatus] =
    useState<"loading" | "ready" | "verifying" | "error">("loading")
  // Runtime settings from GET /api/config; until they arrive the button shows as loading.
  const { config, loaded } = usePublicConfig()
  const clientId = config.yandexClientId
  const devBypass = config.devAuthBypass
  const [devError, setDevError] = useState(false)

  useEffect(() => {
    if (!clientId || !container.current) return
    let active = true
    let initializing = false
    const controller = new AbortController()
    const host = document.createElement("div")
    // crypto.randomUUID exists only in secure contexts; plain http must still render.
    host.id = "yandex-sign-in-" + Math.random().toString(36).slice(2)
    container.current.appendChild(host)
    setStatus("loading")
    // Loading is asynchronous, so StrictMode's discarded effect never calls init.
    void loadYandexSdk()
      .then(async (sdk) => {
        if (!active) return
        initializing = true
        let handler: () => Promise<unknown>
        try {
          handler = await initializeYandexButton(
            sdk,
            clientId,
            window.location.origin,
            host.id,
            theme,
          )
        } finally {
          initializing = false
          if (!active) host.remove()
        }
        if (!active) return
        setStatus("ready")
        const data = await handler()
        if (!active) return
        setStatus("verifying")
        const profile = await profileFromYandexResult(
          data,
          clientId,
          controller.signal,
        )
        if (active) onSignIn(profile)
      })
      .catch(() => {
        if (active) {
          host.replaceChildren()
          setStatus("error")
        }
      })
    return () => {
      active = false
      controller.abort()
      host.hidden = true
      // Keep the target until init settles: a missing parentId makes the SDK
      // fall back to document.body, outside the component's ownership.
      if (!initializing) host.remove()
    }
  }, [clientId, theme, attempt, onSignIn])

  if (devBypass) {
    return (
      <div className="yandex-sign-in">
        <button
          className="auth-button"
          type="button"
          onClick={() => {
            setDevError(false)
            devSignIn().then(onSignIn, () => setDevError(true))
          }}
        >
          Войти (dev)
        </button>
        {devError && (
          <small role="alert">
            Dev-вход не удался: включён ли DEV_AUTH_BYPASS_USER_ID на шлюзе?
          </small>
        )}
      </div>
    )
  }

  return (
    <div className="yandex-sign-in">
      <div ref={container} aria-label="Вход через Яндекс ID" />
      {!clientId ? (
        <>
          <button className="auth-button" type="button" disabled>
            Войти через Яндекс ID
          </button>
          <small role="status">{loaded ? "Вход через Яндекс пока не настроен." : "Загружаем настройки входа…"}</small>
        </>
      ) : (
        <>
          {status === "verifying" && (
            <small role="status">Подтверждаем вход…</small>
          )}
          {status === "error" && (
            <>
              <small role="alert">
                Не удалось войти через Яндекс. Проверьте соединение и разрешение
                на всплывающие окна.
              </small>
              <button
                type="button"
                className="auth-action"
                onClick={() => setAttempt((value) => value + 1)}
              >
                Повторить вход
              </button>
            </>
          )}
        </>
      )}
    </div>
  )
}
