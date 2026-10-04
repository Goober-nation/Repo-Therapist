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

/**
 * Bus factor and the ownership map. Informational: they never change the score, and a block the
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
      description="Фактор автобуса и карта владельцев кода по истории коммитов. Справочные показатели: на оценку не влияют."
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

  const { busFactor, ownership } = analytics
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
