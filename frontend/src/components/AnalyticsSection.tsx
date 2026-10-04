import { useState } from "react"
import type { OwnedFile, OwnershipArea, RepoAnalytics } from "../types"
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
  few_reviewed: "Нет данных: слишком мало pull request'ов с ревью, чтобы назвать типичное время ожидания.",
}

type OwnerView = "areas" | "subareas" | "files"

const OWNER_VIEWS: { id: OwnerView; label: string }[] = [
  { id: "areas", label: "Каталоги" },
  { id: "subareas", label: "Подкаталоги" },
  { id: "files", label: "Самые изменяемые файлы" },
]

/** Directories (top level or two deep) or the most changed files, with their main owner. */
function OwnersCard({ ownership }: { ownership: RepoAnalytics["ownership"] }) {
  const [view, setView] = useState<OwnerView>("areas")
  const offered = OWNER_VIEWS.filter((item) => ownership[item.id].length > 0)
  const current = offered.find((item) => item.id === view)?.id ?? offered[0]?.id

  if (!ownership.available || !current) {
    return (
      <Card className="analytics-owners">
        <Label>Карта владельцев кода</Label>
        <p className="muted-copy">Нет данных: за период слишком мало коммитов, чтобы построить карту.</p>
      </Card>
    )
  }

  const rows: (OwnershipArea | OwnedFile)[] = ownership[current]
  const isFiles = current === "files"

  return (
    <Card className="analytics-owners">
      <Label>Карта владельцев кода</Label>
      {offered.length > 1 && (
        <div className="analytics-tabs" role="group" aria-label="Уровень карты владельцев">
          {offered.map((item) => (
            <button
              key={item.id}
              type="button"
              className={`analytics-tab ${item.id === current ? "analytics-tab--active" : ""}`}
              aria-pressed={item.id === current}
              onClick={() => setView(item.id)}
            >
              {item.label}
            </button>
          ))}
        </div>
      )}
      <div className="table-scroll">
        <table className="analytics-table">
          <thead>
            <tr>
              <th>{isFiles ? "Файл" : current === "subareas" ? "Подкаталог" : "Каталог"}</th>
              <th>{isFiles ? "Строк" : "Файлов"}</th>
              <th>Основной владелец</th>
              <th>Доля изменений</th>
              <th>Владельцев</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.path}>
                <td>
                  <code>{row.path}</code>
                </td>
                <td>{"files" in row ? row.files : row.lines}</td>
                <td>{row.topOwner}</td>
                <td>
                  <ShareBar value={row.topOwnerShare} label={`${row.topOwner}: ${percent(row.topOwnerShare)} изменений`} />{" "}
                  {percent(row.topOwnerShare)}
                </td>
                <td>{row.owners}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  )
}

const ISSUE_NO_DATA: Record<string, string> = {
  few_authors: "Нет данных: задачи открыли слишком мало разных людей — похоже, команда ведёт трекер для себя, а не принимает обращения.",
  few_issues: "Нет данных: среди последних задач слишком мало, чтобы судить о скорости ответа.",
  imported: "Нет данных: задачи созданы почти одновременно, похоже на импорт трекера.",
  unavailable: "Нет данных: комментарии к задачам получить не удалось.",
}

function formatHours(hours: number): string {
  if (hours < 1) return "менее часа"
  if (hours < 10) return `${(Math.round(hours * 10) / 10).toLocaleString("ru-RU")} ч`
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
      description="Фактор автобуса, ревью pull request'ов, ответы на задачи и карта владельцев кода. Справочные показатели: на оценку не влияют, кроме времени ответа на задачи — оно входит в категорию Issues."
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

  const { busFactor, ownership, reviews, issueReplies } = analytics
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
          ) : reviews.bottlenecks.available ? null : (
            <p className="muted-copy">{REVIEW_NO_DATA[reviews.reason ?? ""] ?? REVIEW_NO_DATA.unavailable}</p>
          )}
          {reviews.bottlenecks.available ? (
            <div className="analytics-bottlenecks">
              <strong>Где ревью ждут</strong>
              {reviews.bottlenecks.medianFirstReviewHours !== undefined ? (
                <p>
                  Медианное время до первого ревью: <strong>{formatHours(reviews.bottlenecks.medianFirstReviewHours)}</strong>{" "}
                  (по {reviews.bottlenecks.reviewed} pull request&apos;ам с ревью).
                </p>
              ) : (
                <p className="muted-copy">
                  Время до первого ревью не посчитано: pull request&apos;ов с ревью слишком мало.
                </p>
              )}
              {reviews.bottlenecks.open > 0 ? (
                <p>
                  Открытых без ревью{reviews.bottlenecks.waitingDays > 0 ? ` дольше ${reviews.bottlenecks.waitingDays} дн.` : ""}:{" "}
                  <strong>{reviews.bottlenecks.waiting}</strong> из {reviews.bottlenecks.open} открытых.
                </p>
              ) : null}
            </div>
          ) : reviews.available ? (
            <p className="muted-copy">{REVIEW_NO_DATA[reviews.bottlenecks.reason ?? ""] ?? REVIEW_NO_DATA.unavailable}</p>
          ) : null}
        </Card>

        <Card className="analytics-card">
          <Label>Ответы на задачи</Label>
          {issueReplies.available && issueReplies.medianFirstReplyHours !== undefined ? (
            <>
              <div className="analytics-card__value">{formatHours(issueReplies.medianFirstReplyHours)}</div>
              <p>
                медианное время до первого ответа на задачи среди последних {issueReplies.sampled}: отвечено{" "}
                {issueReplies.replied}, без ответа {issueReplies.unanswered}.
              </p>
              <p className="muted-copy">
                Ответ — комментарий не автора и не бота. Задача без ответа считается за время ожидания (до закрытия или
                до сегодняшнего дня, не больше {issueReplies.capDays} дн.). Это же время входит в категорию Issues, когда оно
                применимо к репозиторию.
              </p>
            </>
          ) : (
            <p className="muted-copy">{ISSUE_NO_DATA[issueReplies.reason ?? ""] ?? ISSUE_NO_DATA.unavailable}</p>
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

      <OwnersCard ownership={ownership} />
    </section>
  )
}
