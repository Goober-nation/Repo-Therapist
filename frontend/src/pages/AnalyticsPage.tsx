import type { HttpRepoHealthApi } from "../api/client"
import type { StatsResponse } from "../api/contracts"
import HistoryChart from "../components/HistoryChart"
import SeriesCard from "../components/SeriesCard"
import { Badge, Button, Card, Label, PageHeading, ScoreBar } from "../components/Shell"
import { formatDateTime, PRIORITY_LABELS, type Tone } from "../metrics"
import { PUBLIC_SERIES } from "../seriesView"
import { adviceItems, categoryBars, distributionBars, languageBars, levelItems, trendPoints } from "../statsView"
import type { Priority } from "../types"

const LEVEL_TONES: Record<string, Tone> = { excellent: "good", healthy: "good", attention: "warning", at_risk: "critical" }
const PRIORITY_TONES: Record<string, Tone> = { critical: "critical", high: "critical", medium: "warning", low: "neutral" }

export default function AnalyticsPage({
  api,
  stats,
  isLoading,
  error,
  onRetry,
  onOpenRepository,
}: {
  api: HttpRepoHealthApi
  stats?: StatsResponse
  isLoading: boolean
  error?: string
  onRetry: () => void
  onOpenRepository: (org: string, repo: string) => void
}) {
  return (
    <div className="page-stack">
      <PageHeading
        eyebrow="Аналитика платформы"
        title="Состояние открытых репозиториев"
        description="Сводка по всем публичным оценкам SourceCraft. Приватные оценки в неё не попадают."
        actions={
          <>
            <a className="button button--secondary" href={api.exportUrl("csv")} download>
              Скачать CSV
            </a>
            <a className="button button--secondary" href={api.exportUrl("json")} download>
              Скачать JSON
            </a>
          </>
        }
      />

      {error && !stats && (
        <Card className="empty-state" >
          <div role="alert">
            <strong>Не удалось загрузить аналитику</strong>
            <p>{error}</p>
          </div>
          <Button onClick={onRetry}>Повторить</Button>
        </Card>
      )}

      {!stats && isLoading && (
        <Card className="empty-state">
          <p role="status">Считаем статистику…</p>
        </Card>
      )}

      {stats && stats.graded_repos === 0 && (
        <Card className="empty-state">
          <strong>Пока нет оценённых репозиториев</strong>
          <p>Статистика появится после первых публичных оценок.</p>
        </Card>
      )}

      {stats && stats.graded_repos > 0 && <StatsBody stats={stats} onOpenRepository={onOpenRepository} />}

      <ServiceActivity api={api} />
    </div>
  )
}

function StatsBody({ stats, onOpenRepository }: { stats: StatsResponse; onOpenRepository: (org: string, repo: string) => void }) {
  const distribution = distributionBars(stats)
  const levels = levelItems(stats)
  const languages = languageBars(stats)
  const categories = categoryBars(stats)
  const advice = adviceItems(stats)
  const trend = trendPoints(stats)
  const updated = formatDateTime(stats.generated_at ?? undefined)

  return (
    <>
      <div className="stat-grid">
        <Card className="stat-card">
          <span>Оценено репозиториев</span>
          <strong>{stats.graded_repos}</strong>
        </Card>
        <Card className="stat-card">
          <span>Средний Repo Health Score</span>
          <strong>{stats.avg_score}</strong>
        </Card>
        <Card className="stat-card">
          <span>Медиана</span>
          <strong>{stats.median_score}</strong>
        </Card>
      </div>

      <div className="analytics-grid">
        <Card>
          <Label>Распределение</Label>
          <h2>Сколько репозиториев в каждом диапазоне оценки</h2>
          <div className="distribution" role="img" aria-label="Распределение оценок по диапазонам в десять баллов">
            {distribution.map((bar) => (
              <div className="distribution__col" key={bar.key} title={`${bar.label}: ${bar.note}`}>
                <span className="distribution__value">{bar.value}</span>
                <span className="distribution__bar" style={{ height: `${Math.max(bar.value > 0 ? 4 : 0, bar.width)}%` }} />
                <span className="distribution__label">{bar.label}</span>
              </div>
            ))}
          </div>
          <ul className="level-list">
            {levels.map((level) => (
              <li key={level.key}>
                <Badge tone={LEVEL_TONES[level.key] ?? "neutral"}>{level.label}</Badge>
                <span>
                  {level.repos} · {level.share}%
                </span>
              </li>
            ))}
          </ul>
        </Card>

        <Card>
          <Label>По категориям</Label>
          <h2>Средний балл категории</h2>
          <p className="muted">Среднее по репозиториям, у которых по категории есть данные; «нет данных» нулём не считается.</p>
          <ul className="bar-list">
            {categories.map((bar) => (
              <li key={bar.key}>
                <div className="bar-list__head">
                  <span>{bar.label}</span>
                  <strong>{bar.value}</strong>
                </div>
                <ScoreBar score={bar.width} label={`${bar.label}: ${bar.value}`} />
                <small className="muted">{bar.note}</small>
              </li>
            ))}
          </ul>
        </Card>

        <Card>
          <Label>По языкам</Label>
          <h2>Средний балл по основному языку</h2>
          {languages.length === 0 ? (
            <p className="muted">Недостаточно данных: язык показывается, когда оценено не меньше {stats.min_language_repos} его репозиториев.</p>
          ) : (
            <>
              <p className="muted">Языки, у которых оценено не меньше {stats.min_language_repos} репозиториев.</p>
              <ul className="bar-list">
                {languages.map((bar) => (
                  <li key={bar.key}>
                    <div className="bar-list__head">
                      <span className="language-label">
                        {bar.color && <span className="language-label__dot" style={{ backgroundColor: bar.color }} aria-hidden="true" />}
                        {bar.label}
                      </span>
                      <strong>{bar.value}</strong>
                    </div>
                    <ScoreBar score={bar.width} label={`${bar.label}: ${bar.value}`} />
                    <small className="muted">{bar.note}</small>
                  </li>
                ))}
              </ul>
            </>
          )}
        </Card>

        <Card>
          <Label>Динамика</Label>
          <h2>Средний балл по неделям</h2>
          <p className="muted">Среднее по репозиториям, оценённым в эту неделю: показывает, что измерила платформа, а не изменение одного набора.</p>
          {trend.length > 0 ? (
            <HistoryChart
              label="Средний Repo Health Score по неделям"
              series={[{ id: "avg", label: "Средний балл", tone: 1, points: trend }]}
            />
          ) : (
            <p className="muted">Истории оценок пока нет.</p>
          )}
        </Card>

        <Card className="analytics-grid__wide">
          <Label>Что чаще всего улучшать</Label>
          <h2>Самые частые рекомендации</h2>
          {advice.length === 0 ? (
            <p className="muted">Рекомендаций пока нет.</p>
          ) : (
            <ol className="advice-list">
              {advice.map((item) => (
                <li key={item.key}>
                  <div>
                    <strong>{item.metric}</strong>
                    <small className="muted">{item.category}</small>
                  </div>
                  <Badge tone={PRIORITY_TONES[item.priority] ?? "neutral"}>
                    {PRIORITY_LABELS[item.priority as Priority] ?? item.priority}
                  </Badge>
                  <span className="advice-list__count">
                    {item.repos} репо · {item.share}%
                  </span>
                </li>
              ))}
            </ol>
          )}
        </Card>

        <Card className="analytics-grid__wide">
          <Label>Рост</Label>
          <h2>Больше всего выросли за {stats.improved_days} дн.</h2>
          {stats.most_improved.length === 0 ? (
            <p className="muted">За этот период ни у одного репозитория оценка не выросла, либо повторных оценок ещё нет.</p>
          ) : (
            <ol className="advice-list">
              {stats.most_improved.map((repo) => (
                <li key={`${repo.org}/${repo.name}`}>
                  <div>
                    <button className="link-button" type="button" onClick={() => onOpenRepository(repo.org, repo.name)}>
                      {repo.org}/{repo.name}
                    </button>
                    <small className="muted">
                      {repo.past_score} → {repo.score}
                    </small>
                  </div>
                  <Badge tone="good">+{repo.change}</Badge>
                </li>
              ))}
            </ol>
          )}
        </Card>
      </div>

      <p className="muted analytics-note">
        {updated ? `Обновлено ${updated}. ` : ""}Данные кэшируются на несколько минут. Выгрузка содержит все публичные оценки: репозиторий, ссылку, Score,
        уровень, язык и оценки четырёх публичных категорий.
      </p>
    </>
  )
}

/** How busy the service is, from public gradings only: private grades never enter these graphs. */
function ServiceActivity({ api }: { api: HttpRepoHealthApi }) {
  return (
    <section aria-labelledby="service-activity-title" className="page-stack">
      <div>
        <Label>Работа сервиса</Label>
        <h2 id="service-activity-title" className="regrade-title">
          Как идут оценки
        </h2>
        <p className="muted">За последние две недели, только по публичным репозиториям.</p>
      </div>
      <div className="metric-grid">
        {PUBLIC_SERIES.map((info) => (
          <SeriesCard
            key={info.name}
            info={info}
            loadKey={info.name}
            load={() => api.getPublicSeries(info.name)}
            emptyText="Данные появятся, когда сервис накопит историю оценок."
          />
        ))}
      </div>
    </section>
  )
}
