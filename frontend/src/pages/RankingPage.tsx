import { useMemo, useState } from "react"
import { RANKING_PAGE_SIZE, type RankingSort } from "../api/client"
import { Badge, Button, Card, LanguageLabel, PageHeading } from "../components/Shell"
import { formatDateTime, getLastActivityLabel, HEALTHY_SCORE, healthLevel } from "../metrics"
import type { RepositorySummary } from "../types"

export interface RankingQuery {
  page: number
  sort: RankingSort
  language: string
  /** Leave out grades marked low-confidence; off by default, the order itself never changes. */
  hideLowConfidence: boolean
}

export const DEFAULT_RANKING_QUERY: RankingQuery = { page: 1, sort: "score", language: "", hideLowConfidence: false }

export default function RankingPage({
  repositories,
  onSelectRepository,
  total = repositories.length,
  query = DEFAULT_RANKING_QUERY,
  languages = [],
  isLoading = false,
  error,
  onQueryChange = () => {},
  onRetry = () => {},
}: {
  repositories: RepositorySummary[]
  onSelectRepository: (repository: RepositorySummary) => void
  /** Repositories in the whole ranking (all pages) for the current language filter. */
  total?: number
  query?: RankingQuery
  /** Languages seen so far, for the server-side language filter. */
  languages?: string[]
  isLoading?: boolean
  error?: string
  onQueryChange?: (query: RankingQuery) => void
  onRetry?: () => void
}) {
  const [searchQuery, setSearchQuery] = useState("")
  const [minimumHealth, setMinimumHealth] = useState(0)
  const pageCount = Math.max(1, Math.ceil(total / RANKING_PAGE_SIZE))

  // Search and the minimum score narrow the loaded page only; the catalog is paged on the server.
  const visibleRepositories = useMemo(() => {
    const text = searchQuery.trim().toLocaleLowerCase()
    return repositories.filter(
      (repository) =>
        (!text ||
          repository.fullName.toLocaleLowerCase().includes(text) ||
          repository.language.toLocaleLowerCase().includes(text)) &&
        (minimumHealth === 0 || (repository.healthScore ?? -1) >= minimumHealth),
    )
  }, [minimumHealth, repositories, searchQuery])

  const scores = repositories.flatMap((repository) =>
    repository.healthScore === null ? [] : [repository.healthScore],
  )
  const averageScore = scores.length
    ? Math.round(scores.reduce((sum, score) => sum + score, 0) / scores.length)
    : null

  const change = (next: Partial<RankingQuery>) => onQueryChange({ ...query, page: 1, ...next })

  const resetFilters = () => {
    setSearchQuery("")
    setMinimumHealth(0)
    if (query.language || query.hideLowConfidence) change({ language: "", hideLowConfidence: false })
  }

  return (
    <div className="page-stack">
      <PageHeading
        eyebrow="Публичные репозитории"
        title="Рейтинг здоровья"
        description="Открытые репозитории SourceCraft, отсортированные по Repo Health Score. Лайки показаны для справки и на место в рейтинге не влияют. Выберите репозиторий, чтобы увидеть подробную оценку и рекомендации."
      />

      {total > 0 && (
        <div className="stat-grid">
          <Card className="stat-card">
            <span>В рейтинге{query.language ? ` (${query.language})` : ""}</span>
            <strong>{total}</strong>
          </Card>
          <Card className="stat-card">
            <span>Средний Score на странице</span>
            <strong>{averageScore ?? "—"}</strong>
          </Card>
          <Card className="stat-card">
            <span>Страница</span>
            <strong>
              {query.page} из {pageCount}
            </strong>
          </Card>
        </div>
      )}

      <Card className="ranking-card">
        <div className="ranking-toolbar">
          <label className="search-field">
            <span className="visually-hidden">Поиск репозиториев</span>
            <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
              <circle cx="7" cy="7" r="4.5" />
              <path d="m10.5 10.5 3 3" />
            </svg>
            <input
              type="search"
              placeholder="Поиск на этой странице"
              value={searchQuery}
              onChange={(event) => setSearchQuery(event.target.value)}
            />
          </label>
          <label className="filter-field">
            <span>Язык</span>
            <select value={query.language} onChange={(event) => change({ language: event.target.value })}>
              <option value="">Все</option>
              {[...new Set([...languages, query.language].filter(Boolean))].sort().map((language) => (
                <option key={language} value={language}>
                  {language}
                </option>
              ))}
            </select>
          </label>
          <label className="filter-field">
            <span>Score (на странице)</span>
            <select value={minimumHealth} onChange={(event) => setMinimumHealth(Number(event.target.value))}>
              <option value={0}>Любой</option>
              <option value={60}>От 60</option>
              <option value={HEALTHY_SCORE}>От {HEALTHY_SCORE}</option>
              <option value={90}>От 90</option>
            </select>
          </label>
          <label className="filter-field filter-field--sort">
            <span>Сортировка</span>
            <select value={query.sort} onChange={(event) => change({ sort: event.target.value as RankingSort })}>
              <option value="score">Repo Health Score</option>
              <option value="likes">Лайки</option>
              <option value="last_activity">Последняя активность</option>
            </select>
          </label>
          <label className="filter-check">
            <input
              type="checkbox"
              checked={query.hideLowConfidence}
              onChange={(event) => change({ hideLowConfidence: event.target.checked })}
            />
            <span>Скрыть оценки с низкой достоверностью</span>
          </label>
          <span className="ranking-count" aria-live="polite">
            {visibleRepositories.length} проектов{visibleRepositories.length !== repositories.length ? " на странице" : ""}
          </span>
        </div>

        <div className="table-scroll">
          <div className="ranking-table" role="table" aria-label="Рейтинг репозиториев">
            <div className="ranking-row ranking-row--header" role="row">
              <span role="columnheader">Место</span>
              <span role="columnheader">Репозиторий</span>
              <span role="columnheader">Язык</span>
              <span role="columnheader">Score</span>
              <span role="columnheader">Лайки</span>
              <span role="columnheader">Активность</span>
              <span role="columnheader">Статус</span>
            </div>
            {!isLoading && !error && visibleRepositories.map((repository, index) => {
              const level = healthLevel(repository.healthScore)
              return (
                <div
                  className={`ranking-row ranking-row--interactive ${
                    repository.healthScore === null ? "ranking-row--no-data" : ""
                  }`}
                  role="row"
                  key={repository.id}
                >
                  <span className="table-rank" role="cell">
                    {repository.place ?? (query.page - 1) * RANKING_PAGE_SIZE + index + 1}
                  </span>
                  <span className="repository-cell" role="cell">
                    <button
                      className="ranking-row__open"
                      type="button"
                      onClick={() => onSelectRepository(repository)}
                      aria-label={`Открыть анализ ${repository.fullName}`}
                    />
                    <strong>{repository.name}</strong>
                    {repository.webUrl ? (
                      <a
                        className="repository-link"
                        href={repository.webUrl}
                        target="_blank"
                        rel="noopener noreferrer"
                        aria-label={`${repository.fullName} в SourceCraft (новая вкладка)`}
                      >
                        {repository.fullName} ↗
                      </a>
                    ) : (
                      <small>{repository.fullName}</small>
                    )}
                  </span>
                  <span role="cell">
                    <LanguageLabel name={repository.language} color={repository.languageColor} />
                  </span>
                  <strong className="table-score" role="cell">
                    {repository.healthScore ?? "Нет данных"}
                  </strong>
                  <span role="cell">{repository.likes?.toLocaleString("ru-RU") ?? "—"}</span>
                  <span role="cell" title={formatDateTime(repository.lastUpdated)}>
                    {getLastActivityLabel(repository.lastActivityDaysAgo)}
                  </span>
                  <span className="ranking-status-cell" role="cell">
                    <Badge tone={level.tone}>{level.label}</Badge>
                    {repository.confidence?.low && (
                      <span className="confidence-badge" title={repository.confidence.reasons.join("\n")}>
                        <Badge tone="neutral">Мало данных</Badge>
                      </span>
                    )}
                  </span>
                </div>
              )
            })}
          </div>
        </div>

        {error ? (
          <div className="empty-state" role="alert">
            <strong>Не удалось загрузить рейтинг</strong>
            <p>{error}</p>
            <Button variant="secondary" onClick={onRetry}>
              Повторить
            </Button>
          </div>
        ) : isLoading ? (
          <div className="empty-state" role="status">
            Загружаем рейтинг…
          </div>
        ) : repositories.length === 0 ? (
          <div className="empty-state">
            <strong>{query.language ? "Нет репозиториев на этом языке" : "Пока нет репозиториев в общем рейтинге"}</strong>
            <p>Здесь появятся публичные репозитории после расчёта их рейтинга.</p>
            {query.language && (
              <Button variant="quiet" onClick={resetFilters}>
                Сбросить фильтры
              </Button>
            )}
          </div>
        ) : visibleRepositories.length === 0 ? (
          <div className="empty-state">
            <strong>На этой странице ничего не найдено</strong>
            <p>Поиск и фильтр по Score работают в пределах загруженной страницы.</p>
            <Button variant="quiet" onClick={resetFilters}>
              Сбросить фильтры
            </Button>
          </div>
        ) : null}

        {pageCount > 1 && !error && (
          <nav className="ranking-pagination" aria-label="Страницы рейтинга">
            <Button
              variant="secondary"
              disabled={query.page <= 1 || isLoading}
              onClick={() => onQueryChange({ ...query, page: query.page - 1 })}
            >
              ← Назад
            </Button>
            <span>
              Страница {query.page} из {pageCount}
            </span>
            <Button
              variant="secondary"
              disabled={query.page >= pageCount || isLoading}
              onClick={() => onQueryChange({ ...query, page: query.page + 1 })}
            >
              Вперёд →
            </Button>
          </nav>
        )}
      </Card>
    </div>
  )
}
