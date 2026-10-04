import type { RepoAnalytics } from "../types"
import { Card, Label, SectionHeading } from "./Shell"

const percent = (value: number) => `${Math.round(value * 100)}%`

interface ShareBarProps {
  value: number
  label: string
}

function ShareBar({ value, label }: ShareBarProps) {
  return (
    <span className="share-bar" role="img" aria-label={label}>
      <span style={{ width: percent(value) }} />
    </span>
  )
}

function plural(value: number, forms: [string, string, string]): string {
  const tens = value % 100
  const ones = value % 10
  if (tens >= 11 && tens <= 14) return forms[2]
  if (ones === 1) return forms[0]
  if (ones >= 2 && ones <= 4) return forms[1]
  return forms[2]
}

const REVIEW_NO_DATA: Record<string, string> = {
  few_merged: "Нет данных: среди последних pull request'ов слишком мало влитых, чтобы судить о ревью.",
  imported: "Нет данных: pull request'ы созданы почти одновременно, похоже на импорт истории.",
  unavailable: "Нет данных: pull request'ы этого репозитория получить не удалось.",
}

function formatHours(hours: number): string {
  if (hours < 1) return "менее часа"
  if (hours < 48) return `${Math.round(hours)} ч`
  return `${Math.round(hours / 24)} дн.`
}

/**
 * Bus factor, review habits and the ownership map. Informational: they never change the score, and a block the
 * backend could not compute says so instead of showing zeros.
 * analytics: undefined while loading, null when the repository has none yet.
 */
export default function AnalyticsSection({
  analytics,
  isRunning = false,
}: {
  analytics: RepoAnalytics | null | undefined
  isRunning?: boolean
}) {
  const heading = (
    <SectionHeading
      title="Углублённая аналитика"
      description="Фактор автобуса, ревью pull request'ов и карта владельцев кода. Справочные показатели: на оценку не влияют."
    />
  )

  if (analytics === undefined) {
    return (
      <section className="section-block">
        {heading}
        <Card className="empty-note">Загружаем аналитику…</Card>
      </section>
    )
  }

  if (analytics === null) {
    return (
      <section className="section-block">
        {heading}
        <Card className="empty-note">
          {isRunning
            ? "Аналитика появится после завершения анализа."
            : "Аналитики для этого репозитория пока нет: она считается при следующем анализе."}
        </Card>
      </section>
    )
  }

  const { busFactor, ownership, reviews } = analytics
  const { codeowners } = ownership

  return (
    <section className="section-block">
      {heading}
      <div className="analytics-grid">
        <Card className="analytics-card">
          <Label>Фактор автобуса</Label>
          {busFactor.available ? (
            <>
              <div className="analytics-card__value">{busFactor.value}</div>
              <p>
                Половину коммитов за последние {analytics.windowDays} дн.
                сделали {busFactor.value}{" "}
                {plural(busFactor.value, ["автор", "автора", "авторов"])} из{" "}
                {busFactor.authors}.
                {busFactor.value === 1
                  ? " Проект сильно зависит от одного человека."
                  : ""}
              </p>
              <ul className="analytics-list">
                {busFactor.topAuthors.map((author) => (
                  <li key={author.name}>
                    <span>{author.name}</span>
                    <ShareBar
                      value={author.share}
                      label={`${author.name}: ${percent(author.share)} коммитов`}
                    />
                    <span>{percent(author.share)}</span>
                  </li>
                ))}
              </ul>
            </>
          ) : (
            <p className="muted-copy">
              Нет данных: за период слишком мало коммитов, чтобы судить о
              распределении работы.
            </p>
          )}
        </Card>

        <Card className="analytics-card">
          <Label>Ревью pull request&apos;ов</Label>
          {reviews.available && reviews.unreviewedShare !== undefined ? (
            <>
              <div className="analytics-card__value">{percent(reviews.unreviewedShare)}</div>
              <p>
                влитых pull request&apos;ов без ревью: {reviews.unreviewed} из {reviews.merged} среди последних{" "}
                {reviews.sampled}. Ревью — решение или комментарий не автора и не бота.
              </p>
              {reviews.medianMergeHours !== undefined ? (
                <p className="muted-copy">
                  Медианное время до слияния: {formatHours(reviews.medianMergeHours)} (приблизительно: у SourceCraft
                  нет времени слияния).
                </p>
              ) : null}
            </>
          ) : (
            <p className="muted-copy">{REVIEW_NO_DATA[reviews.reason ?? ""] ?? REVIEW_NO_DATA.unavailable}</p>
          )}
        </Card>

        <Card className="analytics-card">
          <Label>CODEOWNERS</Label>
          {codeowners.present ? (
            <>
              <div className="analytics-card__value">
                {codeowners.coveredShare !== undefined
                  ? percent(codeowners.coveredShare)
                  : "—"}
              </div>
              <p>
                {codeowners.coveredShare !== undefined
                  ? "изменённых файлов закреплены за владельцем в CODEOWNERS."
                  : "Файл есть, но сравнить не с чем: за период нет изменённых файлов."}
              </p>
              <p className="muted-copy">
                {codeowners.path}: {codeowners.rules}{" "}
                {plural(codeowners.rules, ["правило", "правила", "правил"])}
              </p>
            </>
          ) : (
            <p className="muted-copy">
              Файл CODEOWNERS не найден: владельцы кода не закреплены.
            </p>
          )}
        </Card>
      </div>

      <Card className="analytics-owners">
        <Label>Карта владельцев по каталогам</Label>
        {ownership.available && ownership.areas.length > 0 ? (
          <div className="table-scroll">
            <table className="analytics-table">
              <thead>
                <tr>
                  <th>Каталог</th>
                  <th>Файлов</th>
                  <th>Основной владелец</th>
                  <th>Доля изменений</th>
                  <th>Владельцев</th>
                </tr>
              </thead>
              <tbody>
                {ownership.areas.map((area) => (
                  <tr key={area.path}>
                    <td>
                      <code>{area.path}</code>
                    </td>
                    <td>{area.files}</td>
                    <td>{area.topOwner}</td>
                    <td>
                      <ShareBar
                        value={area.topOwnerShare}
                        label={`${area.topOwner}: ${percent(area.topOwnerShare)} изменений`}
                      />{" "}
                      {percent(area.topOwnerShare)}
                    </td>
                    <td>{area.owners}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <p className="muted-copy">
            Нет данных: за период слишком мало коммитов, чтобы построить карту.
          </p>
        )}
      </Card>
    </section>
  )
}
