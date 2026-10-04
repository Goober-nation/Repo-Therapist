import { formatShortDate } from "../metrics"

export interface ChartSeries {
  id: string
  label: string
  /** 1-5 picks a series colour from the theme tokens (see .history-series--N in index.css). */
  tone: 1 | 2 | 3 | 4 | 5
  points: { at: string; value: number }[]
}

const WIDTH = 640
const HEIGHT = 260
const PAD = { left: 38, right: 16, top: 14, bottom: 30 }
const TICKS = [0, 25, 50, 75, 100]

/**
 * A 0-100 line chart over time (SVG, no dependency). Points sit at their real time, so an uneven
 * grading history is not stretched; a series with one point is drawn as a dot, and each point has
 * a title with its exact value and date.
 */
export default function HistoryChart({ series, label }: { series: ChartSeries[]; label: string }) {
  const times = series.flatMap((line) => line.points.map((point) => Date.parse(point.at))).filter(Number.isFinite)
  if (times.length === 0) return null

  const min = Math.min(...times)
  const max = Math.max(...times)
  const plotWidth = WIDTH - PAD.left - PAD.right
  const plotHeight = HEIGHT - PAD.top - PAD.bottom
  const x = (time: number) => PAD.left + (max === min ? plotWidth / 2 : ((time - min) / (max - min)) * plotWidth)
  const y = (value: number) => PAD.top + plotHeight - (Math.max(0, Math.min(100, value)) / 100) * plotHeight

  const start = formatShortDate(new Date(min).toISOString())
  const end = formatShortDate(new Date(max).toISOString())

  return (
    <figure className="history-chart">
      <svg viewBox={`0 0 ${WIDTH} ${HEIGHT}`} role="img" aria-label={label} preserveAspectRatio="xMidYMid meet">
        {TICKS.map((tick) => (
          <g key={tick}>
            <line className="history-chart__grid" x1={PAD.left} x2={WIDTH - PAD.right} y1={y(tick)} y2={y(tick)} />
            <text className="history-chart__tick" x={PAD.left - 8} y={y(tick) + 4} textAnchor="end">
              {tick}
            </text>
          </g>
        ))}
        <text className="history-chart__tick" x={x(min)} y={HEIGHT - 8} textAnchor={max === min ? "middle" : "start"}>
          {start}
        </text>
        {max !== min && (
          <text className="history-chart__tick" x={x(max)} y={HEIGHT - 8} textAnchor="end">
            {end}
          </text>
        )}
        {series.map((line) => {
          const coords = line.points.map((point) => ({ ...point, cx: x(Date.parse(point.at)), cy: y(point.value) }))
          return (
            <g key={line.id} className={`history-series history-series--${line.tone}`}>
              {coords.length > 1 && (
                <polyline className="history-series__line" points={coords.map((c) => `${c.cx},${c.cy}`).join(" ")} />
              )}
              {coords.map((c) => (
                <circle className="history-series__dot" key={c.at} cx={c.cx} cy={c.cy} r={4}>
                  <title>{`${line.label}: ${c.value} · ${formatShortDate(c.at) ?? c.at}`}</title>
                </circle>
              ))}
            </g>
          )
        })}
      </svg>
      <figcaption className="history-legend">
        {series.map((line) => (
          <span className="history-legend__item" key={line.id}>
            <span className={`history-legend__swatch history-series--${line.tone}`} aria-hidden="true" />
            {line.label}
          </span>
        ))}
      </figcaption>
    </figure>
  )
}
