import { useEffect, useRef, useState } from "react"
import type { HttpRepoHealthApi } from "../api/client"
import type { BatchPreviewResponse, GradingBatchDetailResponse, GradingBatchResponse } from "../api/contracts"
import { formatDateTime } from "../metrics"
import {
  batchProgress,
  buildFilter,
  EMPTY_BATCH_FORM,
  filterSummary,
  formatEstimate,
  isActive,
  itemErrorLabel,
  MAX_BATCH_REPOS,
  statusLabel,
  type BatchForm,
} from "../gradingBatchView"
import { useGradingBatch } from "../useGradingBatch"
import { Badge, Button, Card, Label, ScoreBar } from "./Shell"

/**
 * Batch grading for admins: pick public repositories from the full list by language, score, age of the
 * last grade or a hand-picked list, take at most N of them by SourceCraft rating, optionally only those
 * already graded, check the dry run, start, and follow, pause or cancel the batch. Private repositories
 * are never part of a batch.
 */
export default function GradingPanel({ api, onChanged }: { api: HttpRepoHealthApi; onChanged?: () => void }) {
  const batches = useGradingBatch(api)
  const current = batches.detail
  const active = current && isActive(current.batch)

  // A start, pause, resume, cancel or finish is an audit-log entry: tell the page so it can reload the log.
  const stamp = current ? `${current.batch.id}:${current.batch.status}` : ""
  const seen = useRef<string>(undefined)
  useEffect(() => {
    if (batches.isLoading) return
    if (seen.current !== undefined && seen.current !== stamp) onChanged?.()
    seen.current = stamp
  }, [batches.isLoading, stamp, onChanged])

  return (
    <Card>
      <Label>Пакетная оценка</Label>
      <h2 className="regrade-title">Оценка публичных репозиториев</h2>
      <p className="muted">
        Репозитории выбираются из полного списка публичных репозиториев и оцениваются по очереди, не больше трёх
        одновременно. Приватные репозитории в пакет не попадают.
      </p>

      {batches.error && (
        <div className="empty-state" role="alert">
          <strong>Не удалось загрузить пакеты</strong>
          <p>{batches.error}</p>
          <Button onClick={() => void batches.refresh()}>Повторить</Button>
        </div>
      )}

      {batches.isLoading && !batches.error && <p className="muted" role="status">Загружаем…</p>}

      {!batches.isLoading && !batches.error && active && (
        <ActiveBatch api={api} detail={current} onChange={() => void batches.refresh()} />
      )}

      {!batches.isLoading && !batches.error && !active && (
        <>
          <BatchForm api={api} maxLimit={batches.maxLimit} onStarted={() => void batches.refresh()} />
          {current && <FinishedBatch detail={current} />}
        </>
      )}

      {batches.history.length > 0 && <History batches={batches.history} />}
    </Card>
  )
}

function BatchForm({ api, maxLimit, onStarted }: { api: HttpRepoHealthApi; maxLimit: number; onStarted: () => void }) {
  const [form, setForm] = useState<BatchForm>(EMPTY_BATCH_FORM)
  const [preview, setPreview] = useState<BatchPreviewResponse>()
  const [isBusy, setIsBusy] = useState(false)
  const [error, setError] = useState<string>()

  // Any edit makes the dry run stale: it has to be repeated before a start.
  const change = (patch: Partial<BatchForm>) => {
    setForm((value) => ({ ...value, ...patch }))
    setPreview(undefined)
    setError(undefined)
  }

  const built = buildFilter(form, maxLimit)

  const check = () => {
    if (!built.filter) return setError(built.problem)
    setIsBusy(true)
    setError(undefined)
    api
      .previewBatch(built.filter)
      .then(setPreview)
      .catch((failure: unknown) => setError(failure instanceof Error ? failure.message : "Не удалось проверить условия."))
      .finally(() => setIsBusy(false))
  }

  const start = () => {
    if (!built.filter || !preview) return
    setIsBusy(true)
    setError(undefined)
    api
      .startBatch(built.filter)
      .then(onStarted)
      .catch((failure: unknown) => setError(failure instanceof Error ? failure.message : "Не удалось запустить пакет."))
      .finally(() => setIsBusy(false))
  }

  const canStart = Boolean(preview && preview.count > 0 && !isBusy)

  return (
    <form className="access-form regrade-form" onSubmit={(event) => event.preventDefault()}>
      <fieldset className="regrade-form__fields" disabled={isBusy}>
        <label className="access-form__field">
          <span>Не больше репозиториев (обязательно{maxLimit > 0 ? `, до ${maxLimit} на этом стенде` : ""})</span>
          <input inputMode="numeric" required value={form.limit} placeholder="100" onChange={(event) => change({ limit: event.target.value })} />
        </label>
        <label className="access-form__field">
          <span>Языки</span>
          <input value={form.languages} placeholder="Go, Python" onChange={(event) => change({ languages: event.target.value })} />
        </label>
        <div className="regrade-form__row">
          <label className="access-form__field">
            <span>Оценка от</span>
            <input inputMode="numeric" value={form.minScore} placeholder="0" onChange={(event) => change({ minScore: event.target.value })} />
          </label>
          <label className="access-form__field">
            <span>Оценка до</span>
            <input inputMode="numeric" value={form.maxScore} placeholder="100" onChange={(event) => change({ maxScore: event.target.value })} />
          </label>
          <label className="access-form__field">
            <span>Последней оценке больше (дней)</span>
            <input inputMode="numeric" value={form.olderThanDays} placeholder="30" onChange={(event) => change({ olderThanDays: event.target.value })} />
          </label>
        </div>
        <label className="access-form__field">
          <span>Свой список (до {MAX_BATCH_REPOS}): организация/репозиторий или ссылка на репозиторий через пробел, запятую или с новой строки</span>
          <textarea rows={3} value={form.repos} placeholder="org/repo-one https://sourcecraft.dev/org/repo-two" onChange={(event) => change({ repos: event.target.value })} />
        </label>
      </fieldset>
      <p className="muted">
        Условия необязательны и складываются: репозиторий должен подходить под все заданные. Из подошедших берутся
        первые по рейтингу SourceCraft, не больше указанного числа. Репозитории без оценки подходят только под условие
        по давности.
      </p>

      <label className="regrade-form__all">
        <input type="checkbox" checked={form.onlyRegrade} disabled={isBusy} onChange={(event) => change({ onlyRegrade: event.target.checked })} />
        <span>Только переоценка</span>
      </label>
      <p className="muted">
        {form.onlyRegrade
          ? "Берутся только репозитории, у которых уже есть оценка; новые не оцениваются."
          : "Репозитории, которые ещё не оценивались, тоже попадут в пакет и будут оценены впервые."}
      </p>

      <label className="regrade-form__all">
        <input type="checkbox" checked={form.force} disabled={isBusy} onChange={(event) => change({ force: event.target.checked })} />
        <span>Принудительная оценка</span>
      </label>
      <p className="muted">
        {form.force
          ? "Все выбранные репозитории будут оценены заново, даже без изменений."
          : "Перед оценкой каждый уже оценённый репозиторий проверяется на новую активность в SourceCraft (коммиты, задачи и другие изменения) с момента последней оценки; без неё он пропускается."}
      </p>

      {error && (
        <p className="access-form__error" role="alert">
          {error}
        </p>
      )}

      {preview && <Preview preview={preview} />}

      <div className="regrade-actions">
        <Button onClick={check} disabled={isBusy}>
          {isBusy && !preview ? "Проверяем…" : "Проверить"}
        </Button>
        <Button variant="primary" onClick={start} disabled={!canStart}>
          Запустить
        </Button>
      </div>
    </form>
  )
}

function Preview({ preview }: { preview: BatchPreviewResponse }) {
  return (
    <div className="regrade-preview" role="status">
      {preview.count === 0 ? (
        <strong>Под условия не подошёл ни один публичный репозиторий.</strong>
      ) : (
        <strong>
          Будет оценено: {preview.count}
          {preview.matched > preview.count ? ` из ${preview.matched} подошедших (первые по рейтингу)` : ""}
        </strong>
      )}
      {preview.count > 0 && preview.inactive > 0 && (
        <p className="muted">
          Без новой активности, скорее всего, будут пропущены: {preview.inactive}. Проверка повторяется перед оценкой каждого
          репозитория.
        </p>
      )}
      {preview.count > 0 && <p className="muted">Ориентировочное время: {formatEstimate(preview.estimate_seconds)}.</p>}
      {preview.sample.length > 0 && (
        <p className="muted regrade-sample">
          {preview.sample.join(", ")}
          {preview.count > preview.sample.length ? ` и ещё ${preview.count - preview.sample.length}` : ""}
        </p>
      )}
      {preview.unmatched.length > 0 && (
        <p className="access-form__error">Не найдены среди публичных репозиториев: {preview.unmatched.join(", ")}</p>
      )}
    </div>
  )
}

function ActiveBatch({ api, detail, onChange }: { api: HttpRepoHealthApi; detail: GradingBatchDetailResponse; onChange: () => void }) {
  const { batch } = detail
  const [isBusy, setIsBusy] = useState(false)
  const [error, setError] = useState<string>()
  const [confirmCancel, setConfirmCancel] = useState(false)

  // A paused or finished batch has nothing left to confirm.
  useEffect(() => setConfirmCancel(false), [batch.status])

  const control = (action: "pause" | "resume" | "cancel") => {
    setIsBusy(true)
    setError(undefined)
    api
      .controlBatch(batch.id, action)
      .then(onChange)
      .catch((failure: unknown) => {
        setError(failure instanceof Error ? failure.message : "Не удалось выполнить действие.")
        onChange()
      })
      .finally(() => {
        setIsBusy(false)
        setConfirmCancel(false)
      })
  }

  return (
    <div className="regrade-active">
      <BatchSummary batch={batch} />
      {batch.status === "paused" && (
        <p className="muted">Пауза: репозитории, которые уже оцениваются, будут дооценены, новые не берутся.</p>
      )}
      {error && (
        <p className="access-form__error" role="alert">
          {error}
        </p>
      )}
      <div className="regrade-actions">
        {batch.status === "running" ? (
          <Button onClick={() => control("pause")} disabled={isBusy}>
            Пауза
          </Button>
        ) : (
          <Button variant="primary" onClick={() => control("resume")} disabled={isBusy}>
            Продолжить
          </Button>
        )}
        {confirmCancel ? (
          <>
            <Button onClick={() => control("cancel")} disabled={isBusy}>
              Да, отменить пакет
            </Button>
            <Button onClick={() => setConfirmCancel(false)} disabled={isBusy}>
              Не отменять
            </Button>
          </>
        ) : (
          <Button onClick={() => setConfirmCancel(true)} disabled={isBusy}>
            Отменить
          </Button>
        )}
      </div>
      <Failures failures={detail.failures} />
    </div>
  )
}

function FinishedBatch({ detail }: { detail: GradingBatchDetailResponse }) {
  return (
    <div className="regrade-finished">
      <Label>Последний пакет</Label>
      <BatchSummary batch={detail.batch} />
      <Failures failures={detail.failures} />
    </div>
  )
}

function BatchSummary({ batch }: { batch: GradingBatchResponse }) {
  const { done, percent } = batchProgress(batch)
  const status = statusLabel(batch.status)

  return (
    <div className="regrade-summary">
      <div className="regrade-summary__head">
        <strong>Пакет № {batch.id}</strong>
        <Badge tone={status.tone}>{status.label}</Badge>
      </div>
      <p className="muted">{filterSummary(batch.filter)}</p>
      <ScoreBar score={percent} label={`Пакет ${batch.id}: обработано ${done} из ${batch.total}`} />
      <p className="regrade-counts">
        Обработано {done} из {batch.total}: успешно {batch.ok}, с ошибкой {batch.failed}
        {batch.skipped > 0 ? `, пропущено ${batch.skipped}` : ""}
        {batch.running > 0 ? `; сейчас оценивается ${batch.running}` : ""}
      </p>
    </div>
  )
}

function Failures({ failures }: { failures: GradingBatchDetailResponse["failures"] }) {
  if (failures.length === 0) return null

  return (
    <div className="regrade-failures">
      <strong>С ошибкой ({failures.length})</strong>
      <ul>
        {failures.map((failure) => (
          <li key={failure.repo}>
            <span>{failure.repo}</span>
            <small className="muted">{itemErrorLabel(failure.error)}</small>
          </li>
        ))}
      </ul>
    </div>
  )
}

function History({ batches }: { batches: GradingBatchResponse[] }) {
  return (
    <div className="regrade-history">
      <Label>История пакетов</Label>
      <div className="table-scroll">
        <table className="analytics-table">
          <thead>
            <tr>
              <th>№</th>
              <th>Запущен</th>
              <th>Условия</th>
              <th>Итог</th>
              <th>Статус</th>
            </tr>
          </thead>
          <tbody>
            {batches.map((batch) => {
              const status = statusLabel(batch.status)
              return (
                <tr key={batch.id}>
                  <td>{batch.id}</td>
                  <td>{formatDateTime(batch.created_at) ?? batch.created_at}</td>
                  <td>{filterSummary(batch.filter)}</td>
                  <td>
                    {batch.ok}/{batch.total}
                    {batch.failed > 0 && <small className="muted"> ошибок: {batch.failed}</small>}
                  </td>
                  <td>
                    <Badge tone={status.tone}>{status.label}</Badge>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}
