import { useEffect, useRef, useState } from "react"
import type { HttpRepoHealthApi } from "../api/client"
import type { CatalogRefreshResponse } from "../api/contracts"
import { catalogErrorLabel, catalogStatusLabel, catalogTriggerLabel, isRefreshRunning } from "../catalogView"
import { formatDateTime } from "../metrics"
import { useCatalog } from "../useCatalog"
import { Badge, Button, Card, Label } from "./Shell"

/**
 * The stored list of public repositories for admins: how big and how old it is, and a refresh that
 * fetches the full list from SourceCraft again. The scheduled and batch regrades select from it.
 */
export default function CatalogPanel({ api, onChanged }: { api: HttpRepoHealthApi; onChanged?: () => void }) {
  const catalog = useCatalog(api)
  const status = catalog.status
  const current = status?.current ?? null
  const running = isRefreshRunning(current)

  const [isBusy, setIsBusy] = useState(false)
  const [error, setError] = useState<string>()
  const [confirmCancel, setConfirmCancel] = useState(false)

  // A start, stop, finish or failure is an audit-log entry: tell the page so it can reload the log.
  const stamp = current ? `${current.id}:${current.status}` : ""
  const seen = useRef<string>(undefined)
  useEffect(() => {
    if (catalog.isLoading) return
    if (seen.current !== undefined && seen.current !== stamp) onChanged?.()
    seen.current = stamp
  }, [catalog.isLoading, stamp, onChanged])

  useEffect(() => setConfirmCancel(false), [running])

  const act = (call: () => Promise<unknown>, fallback: string) => {
    setIsBusy(true)
    setError(undefined)
    call()
      .catch((failure: unknown) => setError(failure instanceof Error ? failure.message : fallback))
      .finally(() => {
        setIsBusy(false)
        setConfirmCancel(false)
        void catalog.refresh()
      })
  }

  return (
    <Card>
      <Label>Список репозиториев</Label>
      <h2 className="regrade-title">Публичные репозитории SourceCraft</h2>
      <p className="muted">
        Плановая переоценка и пакетная оценка выбирают репозитории из этого списка. Он обновляется только по запросу
        администратора; пока идёт обновление, используется прежний список.
      </p>

      {catalog.error && (
        <div className="empty-state" role="alert">
          <strong>Не удалось загрузить список</strong>
          <p>{catalog.error}</p>
          <Button onClick={() => void catalog.refresh()}>Повторить</Button>
        </div>
      )}

      {catalog.isLoading && !catalog.error && (
        <p className="muted" role="status">
          Загружаем…
        </p>
      )}

      {status && !catalog.error && (
        <div className="regrade-summary">
          {status.size > 0 ? (
            <p className="regrade-counts">
              В списке: {status.size.toLocaleString("ru-RU")}
              {status.updated_at && <> · обновлён {formatDateTime(status.updated_at) ?? status.updated_at}</>}
              {status.last_completed && <> {catalogTriggerLabel(status.last_completed)}</>}
            </p>
          ) : (
            <p className="regrade-counts">
              Список ещё не загружен: до первого обновления репозитории запрашиваются у SourceCraft напрямую, не больше
              первой тысячи.
            </p>
          )}

          {current && <RefreshState refresh={current} />}

          {error && (
            <p className="access-form__error" role="alert">
              {error}
            </p>
          )}

          <div className="regrade-actions">
            {!running && (
              <Button variant="primary" disabled={isBusy} onClick={() => act(() => api.refreshCatalog(), "Не удалось запустить обновление.")}>
                {isBusy ? "Запускаем…" : "Обновить список"}
              </Button>
            )}
            {running &&
              (confirmCancel ? (
                <>
                  <Button disabled={isBusy} onClick={() => act(() => api.cancelCatalogRefresh(), "Не удалось остановить обновление.")}>
                    Да, остановить
                  </Button>
                  <Button disabled={isBusy} onClick={() => setConfirmCancel(false)}>
                    Не останавливать
                  </Button>
                </>
              ) : (
                <Button disabled={isBusy} onClick={() => setConfirmCancel(true)}>
                  Остановить
                </Button>
              ))}
          </div>
          {!running && <p className="muted">Обновление занимает около двух-трёх минут.</p>}
        </div>
      )}
    </Card>
  )
}

function RefreshState({ refresh }: { refresh: CatalogRefreshResponse }) {
  const status = catalogStatusLabel(refresh.status)
  const started = formatDateTime(refresh.started_at) ?? refresh.started_at

  return (
    <div className="regrade-summary__head catalog-refresh" role="status">
      <strong>Обновление № {refresh.id}</strong>
      <Badge tone={status.tone}>{status.label}</Badge>
      <small className="muted">
        {refresh.status === "running"
          ? `Загружено страниц: ${refresh.pages}, репозиториев: ${refresh.repos.toLocaleString("ru-RU")}`
          : `Запущено ${started} ${catalogTriggerLabel(refresh)}`}
        {refresh.status === "failed" && refresh.error && ` · ${catalogErrorLabel(refresh.error)} Прежний список сохранён.`}
        {refresh.status === "cancelled" && " · Прежний список сохранён."}
      </small>
    </div>
  )
}
