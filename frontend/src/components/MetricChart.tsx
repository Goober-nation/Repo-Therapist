import { chartModel, formatAxisTime, formatValue, type ChartGeometry } from "../seriesView"

const GEOMETRY: ChartGeometry = { width: 520, height: 220, pad: { left: 52, right: 14, top: 14, bottom: 28 } }

/**
 * A line chart over time for metric curves (SVG, no dependency): x by real time, y from 0 to a round
 * maximum. A curve with one point is a dot; every point has a title with its exact value and time.
 * `lines` come with their legend text already in Russian.
 */
export default function MetricChart({
  lines,
  unit,
  stepSeconds,
  label,
}: {
  lines: { label: string; points: [number, number][] }[]
  unit: string
  stepSeconds: number
  label: string
}) {
  const model = chartModel(lines, GEOMETRY, unit)
  if (!model) return null

  const { width, height, pad } = GEOMETRY

  return (
    <figure className="history-chart metric-chart">
      <svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label={label} preserveAspectRatio="xMidYMid meet">
        {model.yTicks.map((tick) => (
          <g key={tick.value}>
            <line className="history-chart__grid" x1={pad.left} x2={width - pad.right} y1={tick.y} y2={tick.y} />
            <text className="history-chart__tick" x={pad.left - 8} y={tick.y + 4} textAnchor="end">
              {formatValue(unit, tick.value)}
            </text>
          </g>
        ))}
        <text className="history-chart__tick" x={pad.left} y={height - 8} textAnchor={model.tMax === model.tMin ? "middle" : "start"}>
          {formatAxisTime(model.tMin, stepSeconds)}
        </text>
        {model.tMax !== model.tMin && (
          <text className="history-chart__tick" x={width - pad.right} y={height - 8} textAnchor="end">
            {formatAxisTime(model.tMax, stepSeconds)}
          </text>
        )}
        {model.lines.map((line, index) => (
          <g key={line.label} className={`history-series history-series--${(index % 5) + 1}`}>
            {line.coords.length > 1 && <polyline className="history-series__line" points={line.coords.map((c) => `${c.x},${c.y}`).join(" ")} />}
            {line.coords.map((c) => (
              <circle className="history-series__dot" key={c.at} cx={c.x} cy={c.y} r={line.coords.length > 40 ? 0 : 3.5}>
                <title>{`${line.label}: ${formatValue(unit, c.value)} · ${formatAxisTime(c.at, stepSeconds)}`}</title>
              </circle>
            ))}
          </g>
        ))}
      </svg>
      {model.lines.length > 1 && (
        <figcaption className="history-legend">
          {model.lines.map((line, index) => (
            <span className="history-legend__item" key={line.label}>
              <span className={`history-legend__swatch history-series--${(index % 5) + 1}`} aria-hidden="true" />
              {line.label}
            </span>
          ))}
        </figcaption>
      )}
    </figure>
  )
}
