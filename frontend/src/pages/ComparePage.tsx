import { useState } from "react"
import HistoryChart, { type ChartSeries } from "../components/HistoryChart"
import { Badge, Button, Card, PageHeading, ScoreBar } from "../components/Shell"
import { categoryLabel, formatShortDate, healthLevel, signed } from "../metrics"
import { parseRepositoryLink, type RepoRef } from "../route"
import type { CompareSide } from "../types"

const CATEGORIES = ["activity", "issues", "documentation", "code_health"]

/** "org/repo" or a SourceCraft link; undefined when it is neither. */
export function parseRepoInput(text: string): RepoRef | undefined {
  const link = parseRepositoryLink(text)
  if (link) return { org: link.org, repo: link.repo }
  const parts = text.trim().replace(/\/+$/, "").split("/")
  return parts.length === 2 && parts[0] && parts[1] ? { org: parts[0], repo: parts[1].replace(/\.git$/i, "") } : undefined
}

function SidePicker({
  side,
  label,
  onChange,
}: {
  side: CompareSide
  label: string
  onChange: (ref: RepoRef) => void
}) {
  const [text, setText] = useState(`${side.org}/${side.repo}`)
  const [invalid, setInvalid] = useState(false)

  const apply = () => {
    const ref = parseRepoInput(text)
    setInvalid(!ref)
    if (ref) onChange(ref)
  }

  return (
    <form
      className="compare-picker"
      onSubmit={(event) => {
        event.preventDefault()
        apply()
      }}
    >
      <label>
        <span className="eyebrow">{label}</span>
        <input
          type="text"
          value={text}
          onChange={(event) => setText(event.target.value)}
          aria-invalid={invalid}
          aria-label={`${label}: организация/репозиторий или ссылка SourceCraft`}
          placeholder="организация/репозиторий"
        />
      </label>
      <Button type="submit" variant="secondary">
        Показать
      </Button>
      {invalid && (
        <p className="compare-picker__error" role="alert">
          Введите организацию/репозиторий или ссылку на SourceCraft.
        </p>
      )}
    </form>
  )
}

function SideSummary({ side, onOpen }: { side: CompareSide; onOpen: (ref: RepoRef) => void }) {
  if (side.state === "loading") {
    return (
      <p className="muted" role="status">
        Загружаем {side.org}/{side.repo}…
      </p>
    )
  }

  if (side.state === "error") {
    return (
      <p className="compare-side__error" role="alert">
        {side.error}
      </p>
    )
  }

  const level = healthLevel(side.score)
  return (
    <div className="compare-side__summary">
      <button className="link-button" type="button" onClick={() => onOpen(side)}>
        {side.org}/{side.name ?? side.repo}
      </button>
      <strong className="compare-side__score">{side.score ?? "—"}</strong>
      <div className="compare-side__badges">
        <Badge tone={level.tone}>{level.label}</Badge>
        {side.confidence?.low && (
          <span title={side.confidence.reasons.join("\n")}>
            <Badge tone="neutral">Мало данных</Badge>
          </span>
        )}
      </div>
      {side.gradedAt && <small className="muted">Оценка от {formatShortDate(side.gradedAt)}</small>}
    </div>
  )
}

function CategoryCell({ side, id }: { side: CompareSide; id: string }) {
  const score = side.categories.find((category) => category.id === id)?.score
  if (side.state !== "ready") return <span className="muted">—</span>
  if (score === null || score === undefined) return <span className="muted">Нет данных</span>
  return (
    <span className="compare-cell">
      <strong>{score}</strong>
      <ScoreBar score={score} label={`${categoryLabel(id)}: ${score}`} />
    </span>
  )
}

function difference(left: number | null | undefined, right: number | null | undefined) {
  if (typeof left !== "number" || typeof right !== "number") return <span className="muted">—</span>
  const diff = left - right
  return <span className={diff === 0 ? "" : diff > 0 ? "delta delta--up" : "delta delta--down"}>{signed(diff)}</span>
}

/**
 * Two public repositories side by side: Score, the four public categories (with the difference) and
 * their Score history over time. Presentational: the data comes from useCompareSide.
 */
export default function ComparePage({
  left,
  right,
  onChange,
  onOpenRepository,
  onBack,
}: {
  left: CompareSide
  right: CompareSide
  onChange: (which: "left" | "right", ref: RepoRef) => void
  onOpenRepository: (ref: RepoRef) => void
  onBack: () => void
}) {
  const ready = left.state === "ready" && right.state === "ready"
  const series: ChartSeries[] = [
    { id: "left", label: `${left.org}/${left.repo}`, tone: 5, points: left.history.map((p) => ({ at: p.gradedAt, value: p.score })) },
    { id: "right", label: `${right.org}/${right.repo}`, tone: 1, points: right.history.map((p) => ({ at: p.gradedAt, value: p.score })) },
  ].filter((line) => line.points.length > 0) as ChartSeries[]

  return (
    <div className="page-stack">
      <PageHeading
        eyebrow="Публичные репозитории"
        title="Сравнение репозиториев"
        description="Два репозитория рядом: Score, публичные категории и как менялась оценка. Данные только из публичных оценок."
        actions={
          <Button variant="secondary" onClick={onBack}>
            ← К рейтингу
          </Button>
        }
      />

      <div className="compare-sides">
        {([["left", left, "Репозиторий A"], ["right", right, "Репозиторий B"]] as const).map(([which, side, label]) => (
          <Card className="compare-side" key={which}>
            <SidePicker key={`${side.org}/${side.repo}`} side={side} label={label} onChange={(ref) => onChange(which, ref)} />
            <SideSummary side={side} onOpen={onOpenRepository} />
          </Card>
        ))}
      </div>

      {ready && (
        <Card className="compare-table-card">
          <div className="compare-table" role="table" aria-label="Сравнение по категориям">
            <div className="compare-table__row compare-table__row--header" role="row">
              <span role="columnheader">Метрика</span>
              <span role="columnheader">A</span>
              <span role="columnheader">B</span>
              <span role="columnheader">A − B</span>
            </div>
            <div className="compare-table__row" role="row">
              <span role="cell">Repo Health Score</span>
              <strong role="cell">{left.score ?? "Нет данных"}</strong>
              <strong role="cell">{right.score ?? "Нет данных"}</strong>
              <span role="cell">{difference(left.score, right.score)}</span>
            </div>
            {CATEGORIES.map((id) => (
              <div className="compare-table__row" role="row" key={id}>
                <span role="cell">{categoryLabel(id)}</span>
                <span role="cell">
                  <CategoryCell side={left} id={id} />
                </span>
                <span role="cell">
                  <CategoryCell side={right} id={id} />
                </span>
                <span role="cell">
                  {difference(
                    left.categories.find((c) => c.id === id)?.score,
                    right.categories.find((c) => c.id === id)?.score,
                  )}
                </span>
              </div>
            ))}
          </div>
        </Card>
      )}

      {ready && (
        <Card className="history-card">
          <div className="eyebrow">История оценки</div>
          <h2>Как менялся Score</h2>
          {series.length === 0 ? (
            <p className="muted">У этих репозиториев пока нет истории оценки.</p>
          ) : (
            <>
              {series.length < 2 && <p className="muted">История есть только у одного из репозиториев.</p>}
              <HistoryChart series={series} label="История Score двух репозиториев" />
            </>
          )}
        </Card>
      )}
    </div>
  )
}
