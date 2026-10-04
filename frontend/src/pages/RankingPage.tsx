import { useMemo, useState } from "react"
import { RANKING_PAGE_SIZE, type RankingFilters, type RankingSort } from "../api/client"
import { Badge, Button, Card, LanguageLabel, PageHeading } from "../components/Shell"
import { formatDateTime, getLastActivityLabel, HEALTHY_SCORE, healthLevel, signed } from "../metrics"
import type { RepositorySummary } from "../types"

export interface RankingQuery {
  page: number
  sort: RankingSort
  language: string
  /** Leave out grades marked low-confidence; off by default, the order itself never changes. */
  hideLowConfidence: boolean
  /** Filters on repository facts; "" is any. A repo whose fact is unknown never matches a filter. */
  size: "" | "small" | "medium" | "large"
  ci: "" | "yes" | "no"
  security: "" | "yes" | "no"
  /** Text searched in the repository name and description. */
  topic: string
}

export const DEFAULT_RANKING_QUERY: RankingQuery = {
  page: 1,
  sort: "score",
  language: "",
  hideLowConfidence: false,
  size: "",
  ci: "",
  security: "",
  topic: "",
}

/** The query's fact filters in the form the API client takes. */
export function rankingFilters(query: RankingQuery): RankingFilters {
  return {
    ...(query.size ? { size: query.size } : {}),
    ...(query.ci ? { ci: query.ci === "yes" } : {}),
    ...(query.security ? { security: query.security === "yes" } : {}),
    ...(query.topic?.trim() ? { topic: query.topic.trim() } : {}),
  }
}

/** True when any filter beyond the sort is on. */
export function hasActiveFilters(query: RankingQuery): boolean {
  return Boolean(query.language || query.hideLowConfidence || query.size || query.ci || query.security || query.topic?.trim())
}

export default function RankingPage({
  repositories,
  onSelectRepository,
  total = repositories.length,
  query = DEFAULT_RANKING_QUERY,
  languages = [],
  isLoading = false,
  error,
  dynamicsDays,
  compareSelection = [],
  onToggleCompare = () => {},
  onCompare = () => {},
  onClearCompare = () => {},
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
  /** Window of the shown score and place changes in days; undefined while the ranking is not loaded. */
  dynamicsDays?: number
  /** Repositories ticked for comparison (at most two). */
  compareSelection?: RepositorySummary[]
  onToggleCompare?: (repository: RepositorySummary) => void
  onCompare?: () => void
  onClearCompare?: () => void
  onQueryChange?: (query: RankingQuery) => void
  onRetry?: () => void
}) {
  const [searchQuery, setSearchQuery] = useState("")
  // The topic is applied on Enter or when the field loses focus, not on every keystroke.
  const [topicDraft, setTopicDraft] = useState(query.topic ?? "")
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

  const applyTopic = () => {
    if (topicDraft.trim() !== (query.topic ?? "").trim()) change({ topic: topicDraft.trim() })
  }

  const resetFilters = () => {
    setSearchQuery("")
    setMinimumHealth(0)
    setTopicDraft("")
    if (hasActiveFilters(query)) {
      change({ language: "", hideLowConfidence: false, size: "", ci: "", security: "", topic: "" })
    }
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
          <label className="filter-field">
            <span>Размер</span>
            <select
              value={query.size ?? ""}
              onChange={(event) => change({ size: event.target.value as RankingQuery["size"] })}
              title="По числу строк кода (границы по умолчанию): малый — до 2000, средний — от 2000 до 20000, большой — от 20000"
            >
              <option value="">Любой</option>
              <option value="small">Малый</option>
              <option value="medium">Средний</option>
              <option value="large">Большой</option>
            </select>
          </label>
          <label className="filter-field">
            <span>CI</span>
            <select
              value={query.ci ?? ""}
              onChange={(event) => change({ ci: event.target.value as RankingQuery["ci"] })}
              title="Есть ли в репозитории файл настройки CI. Результаты запусков для чужих репозиториев недоступны"
            >
              <option value="">Любой</option>
              <option value="yes">Есть конфиг</option>
              <option value="no">Нет конфига</option>
            </select>
          </label>
          <label className="filter-field">
            <span>Политика безопасности</span>
            <select
              value={query.security ?? ""}
              onChange={(event) => change({ security: event.target.value as RankingQuery["security"] })}
              title="Есть ли в репозитории файл SECURITY.md. Результаты проверок AppSec видит только владелец репозитория"
            >
              <option value="">Любая</option>
              <option value="yes">Есть SECURITY.md</option>
              <option value="no">Нет SECURITY.md</option>
            </select>
          </label>
          <label className="filter-field filter-field--topic">
            <span>Тематика</span>
            <input
              type="search"
              placeholder="Слова из названия или описания"
              maxLength={64}
              value={topicDraft}
              onChange={(event) => setTopicDraft(event.target.value)}
              onBlur={applyTopic}
              onKeyDown={(event) => {
                if (event.key === "Enter") applyTopic()
              }}
            />
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
        {(query.size || query.ci || query.security) && (
          <p className="ranking-note">
            Фильтры по размеру, CI и SECURITY.md смотрят на сам репозиторий (число строк, файлы настроек), а не на
            результаты проверок. Репозитории, для которых это пока не известно, в такой выборке не показываются.
          </p>
        )}
        {dynamicsDays !== undefined && (
          <p className="ranking-note">
            Рядом с местом и Score — изменение за {dynamicsDays} дн.: ▲ выше, ▼ ниже, «новый» — оценки неделю назад ещё не
            было. Изменение места показывается при сортировке по Score.
          </p>
        )}

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
                    {repository.placeChange !== undefined && repository.placeChange !== 0 && (
                      <small
                        className={`delta ${repository.placeChange > 0 ? "delta--up" : "delta--down"}`}
                        title={`${repository.placeChange > 0 ? "Поднялся" : "Опустился"} на ${Math.abs(repository.placeChange)} за ${dynamicsDays ?? ""} дн.`}
                      >
                        {repository.placeChange > 0 ? "▲" : "▼"}
                        {Math.abs(repository.placeChange)}
                      </small>
                    )}
                  </span>
                  <span className="repository-cell" role="cell">
                    <button
                      className="ranking-row__open"
                      type="button"
                      onClick={() => onSelectRepository(repository)}
                      aria-label={`Открыть анализ ${repository.fullName}`}
                    />
                    {repository.healthScore !== null && repository.orgSlug && repository.repoSlug && (
                      <label className="compare-check" title="Выберите два репозитория для сравнения">
                        <input
                          type="checkbox"
                          checked={compareSelection.some((item) => item.id === repository.id)}
                          disabled={
                            compareSelection.length >= 2 && !compareSelection.some((item) => item.id === repository.id)
                          }
                          onChange={() => onToggleCompare(repository)}
                          aria-label={`Сравнить ${repository.fullName}`}
                        />
                        <span aria-hidden="true">Сравнить</span>
                      </label>
                    )}
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
                    {repository.scoreChange !== undefined ? (
                      repository.scoreChange !== 0 && (
                        <small
                          className={`delta ${repository.scoreChange > 0 ? "delta--up" : "delta--down"}`}
                          title={`Score за ${dynamicsDays ?? ""} дн.: ${signed(repository.scoreChange)}`}
                        >
                          {signed(repository.scoreChange)}
                        </small>
                      )
                    ) : (
                      repository.healthScore !== null &&
                      dynamicsDays !== undefined && (
                        <small className="delta delta--new" title={`Оценки ${dynamicsDays} дн. назад ещё не было`}>
                          новый
                        </small>
                      )
                    )}
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
            <strong>{hasActiveFilters(query) ? "Нет репозиториев с такими фильтрами" : "Пока нет репозиториев в общем рейтинге"}</strong>
            <p>
              {hasActiveFilters(query)
                ? "Ослабьте фильтры: репозитории, для которых нужный факт пока не известен, в такой выборке не показываются."
                : "Здесь появятся публичные репозитории после расчёта их рейтинга."}
            </p>
            {hasActiveFilters(query) && (
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

        {compareSelection.length > 0 && (
          <div className="compare-bar" role="region" aria-label="Сравнение репозиториев">
            <span>
              Сравнение: {compareSelection.map((item) => item.fullName).join(" и ")}
              {compareSelection.length < 2 && " — выберите ещё один репозиторий"}
            </span>
            <span className="compare-bar__actions">
              <Button variant="primary" disabled={compareSelection.length !== 2} onClick={onCompare}>
                Сравнить
              </Button>
              <Button variant="quiet" onClick={onClearCompare}>
                Сбросить
              </Button>
            </span>
          </div>
        )}

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
