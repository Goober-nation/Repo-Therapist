import { useState } from "react"
import type { HttpRepoHealthApi } from "../api/client"
import { ADMIN_RANGES, ADMIN_SERIES, DEFAULT_ADMIN_RANGE } from "../seriesView"
import SeriesCard from "./SeriesCard"
import { Button, Card, Label } from "./Shell"

/**
 * Work of the service as graphs, for admins: requests, response time, gradings, LLM requests, batches,
 * refused admin attempts. Counts and timings only, read from Prometheus through the Gateway.
 */
export default function MetricsPanel({ api }: { api: HttpRepoHealthApi }) {
  const [range, setRange] = useState(DEFAULT_ADMIN_RANGE)
  const [refresh, setRefresh] = useState(0)

  return (
    <div className="metrics-panel">
      <Card>
        <div className="audit-head">
          <div>
            <Label>Мониторинг</Label>
            <h2 className="regrade-title">Работа сервиса</h2>
            <p className="muted">Только числа и время: ни репозиториев, ни пользователей на графиках нет. Данные хранятся 15 дней.</p>
          </div>
          <div className="metrics-panel__controls">
            <label className="filter-field">
              <span>Период</span>
              <select value={range} onChange={(event) => setRange(event.target.value)}>
                {ADMIN_RANGES.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </label>
            <Button onClick={() => setRefresh((value) => value + 1)}>Обновить</Button>
          </div>
        </div>
      </Card>

      <div className="metric-grid">
        {ADMIN_SERIES.map((info) => (
          <SeriesCard
            key={info.name}
            info={info}
            loadKey={`${info.name}|${range}|${refresh}`}
            load={() => api.getAdminSeries(info.name, range)}
            emptyText="За этот период данных пока нет."
          />
        ))}
      </div>
    </div>
  )
}
