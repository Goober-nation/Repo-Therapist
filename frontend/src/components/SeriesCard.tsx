import type { SeriesResponse } from "../api/contracts"
import { chartLines, type SeriesInfo } from "../seriesView"
import { useSeries } from "../useSeries"
import MetricChart from "./MetricChart"
import { Card, Label } from "./Shell"

/** One titled graph: loading, error and "no data yet" states around the chart. */
export default function SeriesCard({
  info,
  load,
  loadKey,
  emptyText,
}: {
  info: SeriesInfo
  load: () => Promise<SeriesResponse>
  loadKey: string
  emptyText: string
}) {
  const series = useSeries(load, loadKey)
  const lines = series.data ? chartLines(info.name, info.title, series.data.series) : []

  return (
    <Card className="metric-card">
      <Label>{info.title}</Label>
      <p className="muted">{info.description}</p>

      {series.isLoading && !series.data && <p className="muted" role="status">Загружаем…</p>}

      {series.error && (
        <p className="access-form__error" role="alert">
          {series.error}
        </p>
      )}

      {!series.error && series.data && lines.length === 0 && <p className="muted">{emptyText}</p>}

      {!series.error && series.data && lines.length > 0 && (
        <MetricChart lines={lines} unit={series.data.unit} stepSeconds={series.data.step_seconds} label={info.title} />
      )}
    </Card>
  )
}
