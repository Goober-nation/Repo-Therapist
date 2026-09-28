import { useState } from "react"
import AnalysisResults from "../components/AnalysisResults"
import ReportDownloadMenu from "../components/ReportDownloadMenu"
import { Badge, Button, Card, Label, PageHeading, ScoreBar, SectionHeading } from "../components/Shell"
import {
  categoryWeight,
  formatDateTime,
  getLastActivityLabel,
  HEALTHY_SCORE,
  healthLevel,
  isPrivateAnalysis,
  missingCategories,
  PRIORITY_LABELS,
  scoredCategories,
  splitStrengths,
} from "../metrics"
import type { Priority, RepositorySummary } from "../types"
import type { RepositoryAnalysis } from "../useRepositoryAnalysis"

const PRIORITY_TONES = { high: "critical", medium: "warning", low: "neutral" } as const satisfies Record<Priority, string>

function TextList({ title, items, empty }: { title: string; items: string[]; empty: string }) {
  return (
    <Card className="text-list">
      <Label>{title}</Label>
      {items.length > 0 ? (
        <ul>
          {items.map((item) => (
            <li key={item}>{item}</li>
          ))}
        </ul>
      ) : (
        <p>{empty}</p>
      )}
    </Card>
  )
}

function RunNotice({ analysis }: { analysis: RepositoryAnalysis }) {
  if (analysis.isRunning) {
    return (
      <div className="inline-notice inline-notice--progress" role="status">
        Собираем данные и считаем оценку. Крупный репозиторий может анализироваться несколько минут.
      </div>
    )
  }
  if (!analysis.rerun) return null
  const text = analysis.rerun.stillPending
    ? "Анализ ещё выполняется. Запустите проверку повторно через пару минут."
    : analysis.rerun.gradeChanged
      ? "Анализ завершён, оценка обновлена."
      : "Новых изменений с прошлого анализа нет, оценка актуальна."
  return (
    <div className="inline-notice inline-notice--success" role="status">
      {text}
    </div>
  )
}

export default function RepositoryOverviewPage({
  repository,
  analysis,
  onBackToRanking,
}: {
  repository: RepositorySummary
  analysis: RepositoryAnalysis
  onBackToRanking: () => void
}) {
  const isPrivate = isPrivateAnalysis(repository)
  const summaryKey = `${repository.sourceCraftUuid ?? repository.id}:${isPrivate ? "private" : "public"}`
  const [summaryState, setSummaryState] = useState({ key: summaryKey, open: false })
  const summaryOpen = summaryState.key === summaryKey && summaryState.open
  const level = healthLevel(repository.healthScore)
  const ai = analysis.ai
  const fallback = splitStrengths(repository.categoryScores)
  const strengths = ai?.strengths.length ? ai.strengths : fallback.strengths
  const weaknesses = ai?.weaknesses.length ? ai.weaknesses : fallback.weaknesses
  const recommendations = ai?.recommendations.length ? ai.recommendations : (repository.advice ?? [])
  const missing = missingCategories(repository)
  const gradedAt = formatDateTime(repository.gradedAt)
  const modeLabel = isPrivate ? "приватный анализ (все 6 категорий)" : "публичный анализ"
  const actualWeights = repository.healthScore === null ? [] : scoredCategories(isPrivate, repository.categoryScores)
  const weights = actualWeights.length > 0 ? actualWeights : scoredCategories(isPrivate)

  return (
    <div className="page-stack">
      <PageHeading
        eyebrow={
          repository.webUrl ? (
            <a href={repository.webUrl} target="_blank" rel="noopener noreferrer">
              {repository.fullName} ↗
            </a>
          ) : (
            repository.fullName
          )
        }
        title={repository.name}
        description={`${repository.language} · ${modeLabel} · активность ${getLastActivityLabel(
          repository.lastActivityDaysAgo,
        ).toLocaleLowerCase()}`}
        actions={
          <>
            <Button variant="quiet" onClick={onBackToRanking}>
              К рейтингу
            </Button>
            <ReportDownloadMenu repository={repository} recommendations={recommendations} ai={ai} />
            <Button
              variant="primary"
              onClick={analysis.run}
              disabled={analysis.isRunning}
              title="Оценка пересчитывается, если в репозитории была активность после прошлого анализа. Иначе вернётся текущая."
            >
              {analysis.isRunning ? "Анализ идёт…" : "Запустить анализ"}
            </Button>
          </>
        }
      />

      <RunNotice analysis={analysis} />
      {analysis.error && (
        <div className="inline-notice inline-notice--error" role="alert">
          {analysis.error}
        </div>
      )}

      <div className="overview-grid">
        <Card className={`health-card ${repository.healthScore === null ? "health-card--no-data" : ""}`}>
          <div className="health-card__score-block">
            <Label>Repo Health Score</Label>
            <div className={`health-score ${repository.healthScore === null ? "health-score--no-data" : ""}`}>
              {repository.healthScore ?? "Нет данных"}
              {repository.healthScore !== null && <span>/100</span>}
            </div>
            <div className="health-card__status">
              <Badge tone={level.tone}>{level.label}</Badge>
            </div>
            {repository.healthScore !== null && (
              <ScoreBar score={repository.healthScore} label={`Repo Health Score: ${repository.healthScore} из 100`} />
            )}
            <p className="muted-copy">Последний анализ: {gradedAt ?? "ещё не выполнялся"}</p>
          </div>
          <div className="health-card__summary">
            <button
              type="button"
              className="eyebrow health-card__summary-toggle"
              aria-expanded={summaryOpen}
              aria-controls="ai-summary-content"
              onClick={() => {
                const nextOpen = !summaryOpen
                setSummaryState({ key: summaryKey, open: nextOpen })
                if (nextOpen) analysis.loadAiSummary()
              }}
            >
              Summary AI
              <span className="health-card__summary-chevron" aria-hidden="true">▾</span>
            </button>
            <h2>
              {repository.healthScore === null
                ? "Оценка пока недоступна"
                : repository.healthScore >= HEALTHY_SCORE
                  ? "Проект в хорошем состоянии"
                  : "Есть области для улучшения"}
            </h2>
            <div
              id="ai-summary-content"
              className="health-card__summary-content"
              aria-live="polite"
              aria-busy={analysis.aiLoading}
              hidden={!summaryOpen}
            >
              {summaryOpen && (
                <>
                  <p>
                    {analysis.aiLoading
                      ? "Готовим AI-сводку…"
                      : analysis.aiError ??
                        ai?.summary ??
                        (ai?.status === "PENDING"
                          ? "AI-сводка ещё рассчитывается. Попробуйте запросить её позже."
                          : "AI-сводка пока недоступна. Ниже показаны оценки и рекомендации, рассчитанные по правилам.")}
                  </p>
                  {analysis.aiError && (
                    <button
                      type="button"
                      className="button button--quiet health-card__summary-retry"
                      onClick={analysis.loadAiSummary}
                    >
                      Повторить запрос
                    </button>
                  )}
                </>
              )}
            </div>
          </div>
        </Card>
      </div>

      <div className="two-column">
        <TextList title="Сильные стороны" items={strengths} empty="Пока нет категорий с высокой оценкой." />
        <TextList title="Требует внимания" items={weaknesses} empty="Слабых категорий не найдено." />
      </div>

      <section className="section-block">
        <SectionHeading
          title="Оценки по категориям"
          description="Каждая категория оценивается от 0 до 100. Раскройте карточку, чтобы увидеть факты, на которых основана оценка."
        />
        <AnalysisResults categories={repository.categoryScores} status={repository.gradeStatus} isPrivate={isPrivate} />
      </section>

      {missing.length > 0 && (
        <section className="section-block">
          <SectionHeading
            title="Нет данных"
            description="Эти категории не участвуют в итоговой оценке, а веса остальных пересчитаны. Отсутствие данных не считается плохим результатом."
          />
          <Card className="text-list">
            <ul>
              {missing.map((item) => (
                <li key={item.id}>
                  <strong>{item.label}</strong> — {item.reason}
                </li>
              ))}
            </ul>
          </Card>
        </section>
      )}

      <details className="card explanation">
        <summary>Как рассчитан Repo Health Score</summary>
        <p>
          Итог — взвешенное среднее категорий, по которым есть данные.{" "}
          {actualWeights.length > 0
            ? "Веса в этой оценке (категории без данных исключены, остальные пересчитаны):"
            : `Веса в режиме «${modeLabel}»:`}
        </p>
        <ul>
          {weights.map((item) => (
            <li key={item.id}>
              {item.label} — {item.weight}%
            </li>
          ))}
        </ul>
        <p>
          Если у категории нет данных, она исключается, а веса остальных пересчитываются пропорционально
          исходным: активность 20, issues 20, безопасность 20, документация 15, CI/CD 15, состояние кода 10.
          Лайки на оценку не влияют. Пустые коммиты, коммиты ботов и коммиты с датой в будущем отбрасываются.
          {isPrivate
            ? " Открытые critical-уязвимости ограничивают итог значением 59, high — значением 79."
            : " Безопасность и CI/CD оцениваются только в приватном анализе: эти данные SourceCraft отдаёт по токену владельца."}
        </p>
      </details>

      <section className="section-block">
        <SectionHeading
          title="Рекомендации"
          description={
            ai?.recommendations.length
              ? "Сформированы AI на основе оценок и фактов. Отсортированы по приоритету."
              : "Сформированы по правилам для метрик ниже здорового порога. Отсортированы по приоритету."
          }
        />
        {recommendations.length > 0 ? (
          <div className="recommendation-list">
            {recommendations.map((recommendation, index) => {
              const weight = recommendation.category
                ? categoryWeight(recommendation.category, isPrivate, repository.categoryScores)
                : undefined
              return (
                <Card className="recommendation-card" key={recommendation.id}>
                  <span className="recommendation-card__number">{String(index + 1).padStart(2, "0")}</span>
                  <div className="recommendation-card__body">
                    <div className="recommendation-card__title">
                      <strong>{recommendation.title}</strong>
                      <Badge tone={PRIORITY_TONES[recommendation.priority]}>
                        {PRIORITY_LABELS[recommendation.priority]}
                      </Badge>
                    </div>
                    {recommendation.location && (
                      <span className="recommendation-card__location">Место: {recommendation.location}</span>
                    )}
                    <p>{recommendation.description}</p>
                    {weight !== undefined && (
                      <span className="recommendation-card__location">
                        Относится к категории «{recommendation.title}», её вес в итоговой оценке — {weight}%.
                      </span>
                    )}
                  </div>
                </Card>
              )
            })}
          </div>
        ) : (
          <Card className="empty-note">
            {analysis.isRunning ? "Рекомендации появятся после завершения анализа." : "Рекомендаций для этого репозитория нет."}
          </Card>
        )}
      </section>
    </div>
  )
}
