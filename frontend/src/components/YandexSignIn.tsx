import { useEffect, useRef, useState } from "react"
import {
  initializeYandexButton,
  loadYandexSdk,
  profileFromYandexResult,
  type YandexProfile,
} from "../api/yandexAuth"

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
  const clientId = (import.meta.env.VITE_YANDEX_CLIENT_ID ?? "").trim()

  useEffect(() => {
    if (!clientId || !container.current) return
    let active = true
    let initializing = false
    const controller = new AbortController()
    const host = document.createElement("div")
    host.id = "yandex-sign-in-" + crypto.randomUUID()
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

  return (
    <div className="yandex-sign-in">
      <div ref={container} aria-label="Вход через Яндекс ID" />
      {!clientId ? (
        <>
          <button className="auth-button" type="button" disabled>
            Войти через Яндекс ID
          </button>
          <small role="status">Вход через Яндекс пока не настроен.</small>
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
