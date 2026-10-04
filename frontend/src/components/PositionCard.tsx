import { categoryLabel, signed } from "../metrics"
import type { PositionExplanation, PositionNeighbour } from "../types"
import { Card, Label } from "./Shell"

/** "1 балл", "2 балла", "5 баллов": the Russian plural of a number of points. */
export function pointsLabel(value: number): string {
  const n = Math.abs(value)
  const last = n % 10
  const lastTwo = n % 100
  if (lastTwo >= 11 && lastTwo <= 14) return `${n} баллов`
  if (last === 1) return `${n} балл`
  if (last >= 2 && last <= 4) return `${n} балла`
  return `${n} баллов`
}

function Neighbour({
  neighbour,
  ahead,
  onOpen,
}: {
  neighbour: PositionNeighbour
  ahead: boolean
  onOpen: (org: string, repo: string) => void
}) {
  const link = (
    <button className="link-button" type="button" onClick={() => onOpen(neighbour.org, neighbour.name)}>
      {neighbour.org ? `${neighbour.org}/` : ""}
      {neighbour.name}
    </button>
  )

  if (neighbour.points === 0) {
    return (
      <li>
        Место {neighbour.place}: {link} с тем же Score {neighbour.score};{" "}
        {ahead ? "выше него из-за более свежей активности" : "ниже за счёт менее свежей активности"}.
      </li>
    )
  }

  return (
    <li>
      {ahead ? "До места выше" : "Отрыв от места ниже"} ({neighbour.place}-е, {link}, Score {neighbour.score}):{" "}
      <strong>{pointsLabel(neighbour.points)}</strong>.
    </li>
  )
}

/**
 * Why a repository has its place in the default ranking: its neighbours and, per category, the gap
 * to the median of the top 10. undefined = loading, null = not ranked yet.
 */
export default function PositionCard({
  position,
  onOpenRepository,
}: {
  position?: PositionExplanation | null
  onOpenRepository: (org: string, repo: string) => void
}) {
  const header = (
    <>
      <Label>Место в рейтинге</Label>
      <h2>Почему это место</h2>
    </>
  )

  if (position === undefined) {
    return (
      <Card className="position-card">
        {header}
        <p className="muted" role="status">
          Считаем место…
        </p>
      </Card>
    )
  }

  if (position === null) {
    return (
      <Card className="position-card">
        {header}
        <p className="muted">Место появится после первой публичной оценки репозитория.</p>
      </Card>
    )
  }

  const compared = position.categories.filter((category) => category.gap !== undefined)
  const weakest = position.weakestCategory && compared.find((category) => category.id === position.weakestCategory)

  return (
    <Card className="position-card">
      {header}
      <p className="position-card__headline">
        <strong>
          {position.place}-е место из {position.total}
        </strong>
        , Score {position.score}
      </p>
      <ul className="position-card__neighbours">
        {position.above && <Neighbour neighbour={position.above} ahead onOpen={onOpenRepository} />}
        {position.below && <Neighbour neighbour={position.below} ahead={false} onOpen={onOpenRepository} />}
        {!position.above && !position.below && <li>В рейтинге нет других репозиториев для сравнения.</li>}
      </ul>

      {compared.length > 0 && (
        <>
          <div className="position-table" role="table" aria-label="Категории и медиана топ-10">
            <div className="position-table__row position-table__row--header" role="row">
              <span role="columnheader">Категория</span>
              <span role="columnheader">Score</span>
              <span role="columnheader" title="Медиана категории среди 10 лучших репозиториев">Топ-10</span>
              <span role="columnheader">Разница</span>
            </div>
            {position.categories.map((category) => (
              <div className="position-table__row" role="row" key={category.id}>
                <span role="cell">{categoryLabel(category.id)}</span>
                <strong role="cell">{category.score}</strong>
                <span role="cell">{category.top10Median ?? "—"}</span>
                <span
                  role="cell"
                  className={
                    category.gap === undefined || category.gap === 0 ? "" : category.gap > 0 ? "delta delta--up" : "delta delta--down"
                  }
                >
                  {category.gap === undefined ? "—" : signed(category.gap)}
                </span>
              </div>
            ))}
          </div>
          <p className="position-card__summary">
            {weakest && weakest.gap !== undefined
              ? `Сильнее всего отстаёт: ${categoryLabel(weakest.id)} (${signed(weakest.gap)} к медиане топ-10).`
              : "Ни одна категория не ниже медианы топ-10."}
          </p>
        </>
      )}
    </Card>
  )
}
