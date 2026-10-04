import { useState } from "react"
import { categoryLabel, formatShortDate, signed } from "../metrics"
import type { ScorePoint } from "../types"
import HistoryChart, { type ChartSeries } from "./HistoryChart"
import { Card, Label } from "./Shell"

/** Category lines that can be drawn next to the Score line, in a stable order. */
const CATEGORY_ORDER = ["activity", "issues", "documentation", "code_health"]

/**
 * The public score history of a repository: a line chart (Score, plus any categories ticked) and the
 * latest gradings with their change. undefined = loading, null or empty = no history yet.
 */
export default function ScoreHistorySection({ points }: { points?: ScorePoint[] | null }) {
  const [shown, setShown] = useState<string[]>([])

  const header = (
    <>
      <Label>История оценки</Label>
      <h2>Как менялся Repo Health Score</h2>
    </>
  )

  if (points === undefined) {
    return (
      <Card className="history-card">
        {header}
        <p className="muted" role="status">
          Загружаем историю оценки…
        </p>
      </Card>
    )
  }

  if (!points || points.length === 0) {
    return (
      <Card className="history-card">
        {header}
        <p className="muted">История пока пуста: точка появляется после каждой публичной оценки репозитория.</p>
      </Card>
    )
  }

  const available = CATEGORY_ORDER.filter((id) => points.some((point) => point.categories.some((c) => c.id === id && c.score !== null)))
  const series: ChartSeries[] = [
    { id: "score", label: "Repo Health Score", tone: 5, points: points.map((point) => ({ at: point.gradedAt, value: point.score })) },
    ...available
      .filter((id) => shown.includes(id))
      .map((id): ChartSeries => ({
        id,
        label: categoryLabel(id),
        tone: ((CATEGORY_ORDER.indexOf(id) + 1) as 1 | 2 | 3 | 4),
        points: points.flatMap((point) => {
          const value = point.categories.find((category) => category.id === id)?.score
          return value === null || value === undefined ? [] : [{ at: point.gradedAt, value }]
        }),
      })),
  ]

  const latest = [...points].reverse().slice(0, 5)
  const toggle = (id: string) =>
    setShown((current) => (current.includes(id) ? current.filter((item) => item !== id) : [...current, id]))

  return (
    <Card className="history-card">
      {header}
      {points.length === 1 && (
        <p className="muted">
          Пока одна оценка от {formatShortDate(points[0].gradedAt)}: линия появится после следующей.
        </p>
      )}
      {available.length > 0 && (
        <div className="history-toggles" role="group" aria-label="Категории на графике">
          {available.map((id) => (
            <label className="filter-check" key={id}>
              <input type="checkbox" checked={shown.includes(id)} onChange={() => toggle(id)} />
              <span>{categoryLabel(id)}</span>
            </label>
          ))}
        </div>
      )}
      <HistoryChart series={series} label={`История оценки: ${points.length} оценок, последняя ${points.at(-1)?.score} из 100`} />
      <div className="history-table" role="table" aria-label="Последние оценки">
        <div className="history-table__row history-table__row--header" role="row">
          <span role="columnheader">Дата</span>
          <span role="columnheader">Score</span>
          <span role="columnheader">Изменение</span>
        </div>
        {latest.map((point) => {
          const previous = points[points.indexOf(point) - 1]
          const change = previous ? point.score - previous.score : undefined
          return (
            <div className="history-table__row" role="row" key={point.gradedAt}>
              <span role="cell">{formatShortDate(point.gradedAt) ?? point.gradedAt}</span>
              <strong role="cell">{point.score}</strong>
              <span
                role="cell"
                className={change === undefined || change === 0 ? "" : change > 0 ? "delta delta--up" : "delta delta--down"}
              >
                {change === undefined ? "—" : signed(change)}
              </span>
            </div>
          )
        })}
      </div>
    </Card>
  )
}
