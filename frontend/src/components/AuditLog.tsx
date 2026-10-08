import { useState } from "react"
import type { HttpRepoHealthApi } from "../api/client"
import { auditRow, OUTCOME_FILTERS } from "../auditView"
import { formatDateTime } from "../metrics"
import { useAudit } from "../useAudit"
import AuditPager from "./AuditPager"
import { Badge, Button, Card, Label } from "./Shell"

/**
 * The audit log of admin and user actions, newest first, with an outcome filter and previous/next paging. A new
 * `reloadKey` reloads it from the newest entry (the page raises it when an admin action has just been logged).
 */
export default function AuditLog({ api, reloadKey = 0 }: { api: HttpRepoHealthApi; reloadKey?: number }) {
  const [outcome, setOutcome] = useState("")
  const [attempt, setAttempt] = useState(0)
  const audit = useAudit(api, outcome, attempt + reloadKey)

  return (
    <Card>
      <div className="audit-head">
        <div>
          <Label>Журнал действий</Label>
          <h2>Входы и действия администраторов</h2>
        </div>
        <label className="filter-field">
          <span>Результат</span>
          <select value={outcome} onChange={(event) => setOutcome(event.target.value)}>
            {OUTCOME_FILTERS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </label>
      </div>

      {audit.error && (
        <div className="empty-state" role="alert">
          <strong>Не удалось загрузить журнал</strong>
          <p>{audit.error}</p>
          <Button onClick={() => setAttempt((value) => value + 1)}>Повторить</Button>
        </div>
      )}

      {!audit.error && audit.entries.length === 0 && !audit.isLoading && (
        <p className="muted">Записей пока нет. Журнал хранит последние 30 дней.</p>
      )}

      {audit.entries.length > 0 && (
        <div className="table-scroll">
          <table className="analytics-table audit-table">
            <thead>
              <tr>
                <th>Время</th>
                <th>Кто</th>
                <th>Действие</th>
                <th>Объект</th>
                <th>Результат</th>
              </tr>
            </thead>
            <tbody>
              {audit.entries.map((entry) => {
                const row = auditRow(entry)
                return (
                  <tr key={row.id}>
                    <td>{formatDateTime(row.at) ?? row.at}</td>
                    <td>
                      {row.actorKind}
                      {row.actor && <small className="muted audit-hash"> {row.actor}</small>}
                    </td>
                    <td>
                      {row.action}
                      {row.note && <small className="muted audit-note">{row.note}</small>}
                    </td>
                    <td>{row.target || "—"}</td>
                    <td>
                      <Badge tone={row.outcome.tone}>{row.outcome.label}</Badge>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      {audit.isLoading && <p className="muted" role="status">Загружаем…</p>}

      <AuditPager
        page={audit.page}
        hasPrevious={audit.hasPrevious}
        hasNext={audit.hasNext}
        isLoading={audit.isLoading}
        onPrevious={audit.previous}
        onNext={audit.next}
      />
    </Card>
  )
}
