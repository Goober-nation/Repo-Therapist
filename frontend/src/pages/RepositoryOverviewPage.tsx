import {
  Badge,
  Button,
  Card,
  Divider,
  Label,
  PageHeading,
  ScoreBar,
  SectionHeading,
} from "../components/Shell"
import {
  DEMO_ATTENTION_ITEMS,
  DEMO_RECOMMENDATIONS,
  DEFAULT_REPOSITORY_ID,
  DEMO_SCORE_HISTORY,
  METRIC_LABELS,
  getHealthLabel,
  getLastActivityLabel,
} from "../mocks/demoData"
import { downloadReport } from "../mocks/downloadReport"
import type {
  MetricId,
  MetricResult,
  Recommendation,
  RepositorySummary,
} from "../types"

const metricDescriptions: Record<MetricId, string> = {
  activity: "Активность проекта по истории изменений.",
  codeHealth: "Признаки сопровождаемости и качества кодовой базы.",
  security: "Результаты анализа безопасности и зависимостей.",
  ciCd: "Наличие и стабильность CI/CD-процессов.",
  documentation: "README, лицензия и инструкции для участников.",
  issues: "Состояние задач и работа с обращениями.",
}

function getTone(score: number): "good" | "warning" | "critical" {
  if (score >= 85) return "good"
  if (score >= 60) return "warning"
  return "critical"
}

function MetricCard({
  metricId,
  result,
}: {
  metricId: MetricId
  result: MetricResult
}) {
  return (
    <Card className="metric-card">
      <div className="metric-card__topline">
        <Label>{METRIC_LABELS[metricId]}</Label>
        {result.trend !== null && (
          <span className={`trend ${result.trend < 0 ? "trend--down" : ""}`}>
            {result.trend > 0 ? "+" : ""}
            {result.trend}
          </span>
        )}
      </div>
      <div className="metric-card__score">
        {result.score}
        <span>/100</span>
      </div>
      <p>{metricDescriptions[metricId]}</p>
      <ScoreBar
        score={result.score}
        label={`${METRIC_LABELS[metricId]}: ${result.score} из 100`}
      />
    </Card>
  )
}

function ScoreHistoryChart() {
  const chartWidth = 660
  const chartHeight = 176
  const minScore = 65
  const maxScore = 100
  const points = DEMO_SCORE_HISTORY.map((entry, index) => ({
    x: 30 + (index / (DEMO_SCORE_HISTORY.length - 1)) * (chartWidth - 60),
    y:
      chartHeight -
      28 -
      ((entry.score - minScore) / (maxScore - minScore)) * (chartHeight - 52),
  }))
  const linePath = points
    .map((point, index) => `${index === 0 ? "M" : "L"} ${point.x} ${point.y}`)
    .join(" ")

  return (
    <Card className="history-card">
      <SectionHeading
        title="Repo Health Score во времени"
        description="Последние шесть месяцев"
      />
      <div className="history-chart">
        <svg
          viewBox={`0 0 ${chartWidth} ${chartHeight}`}
          role="img"
          aria-label="График изменения Repo Health Score"
        >
          {[70, 80, 90, 100].map((score) => {
            const y =
              chartHeight -
              28 -
              ((score - minScore) / (maxScore - minScore)) * (chartHeight - 52)
            return (
              <line
                key={score}
                x1="28"
                x2={chartWidth - 28}
                y1={y}
                y2={y}
                className="chart-gridline"
              />
            )
          })}
          <path d={linePath} className="chart-line" />
          {points.map((point, index) => (
            <circle
              key={DEMO_SCORE_HISTORY[index].month}
              cx={point.x}
              cy={point.y}
              r="4"
              className="chart-point"
            >
              <title>{`${DEMO_SCORE_HISTORY[index].month}: ${DEMO_SCORE_HISTORY[index].score}`}</title>
            </circle>
          ))}
        </svg>
        <div className="history-chart__labels">
          {DEMO_SCORE_HISTORY.map((entry) => (
            <span key={entry.month}>
              {entry.month}
              <strong>{entry.score}</strong>
            </span>
          ))}
        </div>
      </div>
    </Card>
  )
}

interface RepositoryOverviewPageProps {
  repository: RepositorySummary
  isAnalysisRunning: boolean
  analysisRefreshed: boolean
  analysisStatus?: "PENDING" | "FINISHED" | null
  recommendations?: Recommendation[]
  onOpenSecurity: () => void
  onRunAnalysis: () => void
}

export default function RepositoryOverviewPage({
  repository,
  isAnalysisRunning,
  analysisRefreshed,
  analysisStatus,
  recommendations: loadedRecommendations,
  onOpenSecurity,
  onRunAnalysis,
}: RepositoryOverviewPageProps) {
  const hasDetailedSample = repository.id === DEFAULT_REPOSITORY_ID
  const categoryMetrics = Object.entries(
    repository.categoryScores,
  ) as [MetricId, MetricResult][]
  const attentionItems = hasDetailedSample ? DEMO_ATTENTION_ITEMS : []
  const recommendations =
    loadedRecommendations ?? (hasDetailedSample ? DEMO_RECOMMENDATIONS : [])

  return (
    <div className="page-stack">
      <PageHeading
        eyebrow={repository.fullName}
        title={repository.name}
        description={`${repository.language} · открытый репозиторий · активность ${getLastActivityLabel(repository.lastActivityDaysAgo).toLocaleLowerCase()}`}
        actions={
          <>
            <Button
              variant="secondary"
              onClick={() => downloadReport(repository)}
            >
              Скачать отчёт .md
            </Button>
            <Button
              variant="primary"
              onClick={onRunAnalysis}
              disabled={isAnalysisRunning}
            >
              {isAnalysisRunning ? "Обновляем…" : "Обновить оценку"}
            </Button>
          </>
        }
      />

      {isAnalysisRunning && (
        <div className="inline-notice inline-notice--progress" role="status">
          Получаем оценку…
        </div>
      )}
      {analysisRefreshed && (
        <div className="inline-notice inline-notice--success" role="status">
          {analysisStatus === "PENDING"
            ? "Запрос принят. Оценка ещё рассчитывается, обновите данные позже."
            : "Оценка обновлена из источника данных."}
        </div>
      )}

      <div className="overview-grid">
        <Card className="health-card">
          <div className="health-card__score-block">
            <Label>Repo Health Score</Label>
            <div className="health-score">
              {repository.healthScore}
              <span>/100</span>
            </div>
            <div className="health-card__status">
              <Badge tone={getTone(repository.healthScore)}>
                {getHealthLabel(repository.healthScore)}
              </Badge>
              <span
                className={`trend ${repository.trend < 0 ? "trend--down" : ""}`}
              >
                {repository.trend > 0 ? "+" : ""}
                {repository.trend} за месяц
              </span>
            </div>
            <ScoreBar
              score={repository.healthScore}
              label={`Repo Health Score: ${repository.healthScore} из 100`}
            />
            <p className="muted-copy">
              Последний анализ:{" "}
              {analysisRefreshed ? "только что" : "3 часа назад"}
            </p>
          </div>
          <div className="health-card__summary">
            <Label>Краткий разбор</Label>
            <h2>
              {repository.healthScore >= 80
                ? "Проект в хорошем состоянии"
                : "Есть области для улучшения"}
            </h2>
            <p>
              {repository.id === "awesome-project"
                ? "Активность проекта и CI/CD выглядят стабильно. Безопасность — главный фактор, который снижает общий результат."
                : "Оценка собрана по доступным показателям. Откройте категории ниже, чтобы увидеть разбивку."}
            </p>
            {repository.id === "awesome-project" && (
              <div className="potential-score">
                <span>Возможный результат после исправлений</span>
                <strong>
                  {repository.healthScore} <span>→</span> 89
                </strong>
                <small>
                  До +7 баллов после обновления уязвимых зависимостей.
                </small>
                <Button variant="quiet" onClick={onOpenSecurity}>
                  Посмотреть безопасность <span aria-hidden="true">→</span>
                </Button>
              </div>
            )}
          </div>
        </Card>

        <div className="metric-grid">
          {categoryMetrics.map(([metricId, result]) => (
            <MetricCard key={metricId} metricId={metricId} result={result} />
          ))}
        </div>
      </div>

      <section className="section-block">
        <SectionHeading
          title="Требует внимания"
          description="Факты и проблемы, на которые стоит посмотреть в первую очередь."
          action={
            hasDetailedSample && (
              <Button variant="quiet" onClick={onOpenSecurity}>
                Открыть безопасность
              </Button>
            )
          }
        />
        {attentionItems.length > 0 ? (
          <Card className="list-card">
            {attentionItems.map((item, index) => (
              <div className="attention-item" key={item.id}>
                {index > 0 && <Divider />}
                <div className="attention-item__main">
                  <div className="attention-item__meta">
                    <Badge tone={item.severity}>
                      {item.severity === "critical"
                        ? "Критично"
                        : "Рекомендация"}
                    </Badge>
                    <span>{item.category}</span>
                  </div>
                  <strong>{item.title}</strong>
                  <p>{item.description}</p>
                </div>
                <div className="attention-item__impact">
                  <span>Потенциальный эффект</span>
                  <strong>+{item.impact}</strong>
                </div>
                {item.severity === "critical" && (
                  <Button variant="secondary" onClick={onOpenSecurity}>
                    Посмотреть
                  </Button>
                )}
              </div>
            ))}
          </Card>
        ) : (
          <Card className="empty-note">
            Детали проблем для этого репозитория пока недоступны.
          </Card>
        )}
      </section>

      <section className="section-block">
        <SectionHeading
          title="Рекомендации"
          description="Действия отсортированы по ожидаемому влиянию на здоровье проекта."
        />
        {recommendations.length > 0 ? (
          <div className="recommendation-list">
            {recommendations.map((recommendation, index) => (
              <Card className="recommendation-card" key={recommendation.id}>
                <span className="recommendation-card__number">
                  {String(index + 1).padStart(2, "0")}
                </span>
                <div className="recommendation-card__body">
                  <div className="recommendation-card__title">
                    <strong>{recommendation.title}</strong>
                    <Badge
                      tone={
                        recommendation.priority === "high"
                          ? "critical"
                          : "neutral"
                      }
                    >
                      {recommendation.priority === "high"
                        ? "Высокий приоритет"
                        : "Средний приоритет"}
                    </Badge>
                  </div>
                  <p>{recommendation.description}</p>
                </div>
                <div className="recommendation-card__score">
                  <span>Score</span>
                  <strong>
                    {recommendation.currentScore} →{" "}
                    {recommendation.projectedScore}
                  </strong>
                </div>
                {recommendation.priority === "high" && (
                  <Button variant="quiet" onClick={onOpenSecurity}>
                    Как исправить
                  </Button>
                )}
              </Card>
            ))}
          </div>
        ) : (
          <Card className="empty-note">
            Подробные рекомендации для этого репозитория пока недоступны.
          </Card>
        )}
      </section>

      {repository.id === DEFAULT_REPOSITORY_ID && <ScoreHistoryChart />}
      <p className="data-caption">
        Оценка и рекомендации приходят из подключённого источника данных.
      </p>
    </div>
  )
}
