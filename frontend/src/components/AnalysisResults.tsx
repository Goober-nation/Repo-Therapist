import { Card, Label, ScoreBar } from "./Shell"
import type { CategoryScores, MetricResult } from "../types"

function CriterionCard({
  name,
  result,
}: {
  name: string
  result: MetricResult
}) {
  const label = result.name ?? name
  return (
    <Card className="metric-card">
      <div className="metric-card__topline">
        <Label>{label}</Label>
        {result.trend !== null && (
          <span className={`trend ${result.trend < 0 ? "trend--down" : ""}`}>
            {result.trend > 0 ? "+" : ""}
            {result.trend}
          </span>
        )}
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
      {result.score !== null && (
        <ScoreBar
          score={result.score}
          label={`${label}: ${result.score} из 100`}
        />
      )}
    </Card>
  )
}

export default function AnalysisResults({
  categories,
  status,
}: {
  categories: CategoryScores
  status?: "PENDING" | "FINISHED" | null
}) {
  const entries = Object.entries(categories)
  if (entries.length === 0) {
    return (
      <Card className="empty-note">
        {status === "PENDING"
          ? "Оценки по категориям ещё рассчитываются."
          : "Оценки по категориям отсутствуют."}
      </Card>
    )
  }
  return (
    <div className="metric-grid">
      {entries.map(([name, result]) => (
        <CriterionCard key={name} name={name} result={result} />
      ))}
    </div>
  )
}
