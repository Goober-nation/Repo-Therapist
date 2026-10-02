import { categoryLabel, categoryWeight, formatMetricValue, metricLabel } from "../metrics"
import type { CategoryScores, GradeStatus, MetricResult } from "../types"
import { Card, Label, ScoreBar } from "./Shell"

function CriterionCard({ id, result, weight }: { id: string; result: MetricResult; weight?: number }) {
  const label = result.name ?? categoryLabel(id)
  return (
    <Card className={`metric-card ${result.score === null ? "metric-card--no-data" : ""}`}>
      <div className="metric-card__topline">
        <Label>{label}</Label>
        {weight !== undefined && <span className="metric-card__weight">вес {weight}%</span>}
      </div>
      {result.score === null ? (
        <p className="metric-card__empty">Нет данных</p>
      ) : (
        <div className="metric-card__score">
          {result.score}
          <span>/100</span>
        </div>
      )}
      {result.description && <p>{result.description}</p>}
      {result.score !== null && result.method === "algorithmic" && (
        <p className="metric-card__method">Упрощённая оценка: проверено только наличие разделов</p>
      )}
      {result.score !== null && <ScoreBar score={result.score} label={`${label}: ${result.score} из 100`} />}
      {result.facts && result.facts.length > 0 && (
        <details className="fact-list">
          <summary>На чём основана оценка</summary>
          <dl>
            {result.facts.map((fact) => (
              <div key={fact.id}>
                <dt>{metricLabel(fact.id)}</dt>
                <dd className={fact.value === null ? "fact-list__empty" : ""}>
                  {formatMetricValue(fact.id, fact.value)}
                  {fact.score !== null && (
                    <span className="fact-list__score" title="Оценка метрики">
                      {" "}
                      · {Math.round(fact.score * 100)}/100
                    </span>
                  )}
                </dd>
              </div>
            ))}
          </dl>
        </details>
      )}
    </Card>
  )
}

export default function AnalysisResults({
  categories,
  status,
  isPrivate = false,
}: {
  categories: CategoryScores
  status?: GradeStatus | null
  isPrivate?: boolean
}) {
  const entries = Object.entries(categories)
  if (entries.length === 0) {
    return (
      <Card className="empty-note">
        {status === "PENDING" ? "Оценки по категориям ещё рассчитываются." : "Оценки по категориям отсутствуют."}
      </Card>
    )
  }
  return (
    <div className="metric-grid">
      {entries.map(([id, result]) => (
        <CriterionCard key={id} id={id} result={result} weight={categoryWeight(id, isPrivate, categories)} />
      ))}
    </div>
  )
}
