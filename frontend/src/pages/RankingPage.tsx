import { useMemo, useState } from "react"
import { Badge, Button, Card, PageHeading } from "../components/Shell"
import { getHealthLabel, getLastActivityLabel } from "../mocks/demoData"
import type { RepositorySummary } from "../types"

type RankingView = "overall" | "growing" | "security" | "issues"
type SortOption = "health-desc" | "health-asc" | "likes-desc" | "activity-recent"

const rankingViews: { id: RankingView; label: string }[] = [
  { id: "overall", label: "Общий рейтинг" },
  { id: "growing", label: "Быстрый рост" },
  { id: "security", label: "Безопасность" },
  { id: "issues", label: "Issues" },
]

function getStatusTone(score: number): "good" | "warning" | "critical" {
  if (score >= 85) return "good"
  if (score >= 60) return "warning"
  return "critical"
}

function sortRepositories(
  repositories: RepositorySummary[],
  view: RankingView,
  sort: SortOption,
) {
  const sortComparators: Record<SortOption, (
    left: RepositorySummary,
    right: RepositorySummary,
  ) => number> = {
    "health-desc": (left, right) => right.healthScore - left.healthScore,
    "health-asc": (left, right) => left.healthScore - right.healthScore,
    "likes-desc": (left, right) => right.likes - left.likes,
    "activity-recent": (left, right) =>
      left.lastActivityDaysAgo - right.lastActivityDaysAgo,
  }

  const viewComparator =
    view === "growing"
      ? (left: RepositorySummary, right: RepositorySummary) =>
          right.trend - left.trend
      : view === "security"
        ? (left: RepositorySummary, right: RepositorySummary) =>
            right.categoryScores.security.score -
            left.categoryScores.security.score
        : view === "issues"
          ? (left: RepositorySummary, right: RepositorySummary) =>
              right.categoryScores.issues.score -
              left.categoryScores.issues.score
          : sortComparators[sort]

  return repositories.sort(
    (left, right) =>
      viewComparator(left, right) || sortComparators[sort](left, right),
  )
}

export default function RankingPage({
  repositories,
  onSelectRepository,
}: {
  repositories: RepositorySummary[]
  onSelectRepository: (repositoryId: string) => void
}) {
  const [searchQuery, setSearchQuery] = useState("")
  const [selectedLanguage, setSelectedLanguage] = useState("all")
  const [minimumHealth, setMinimumHealth] = useState(0)
  const [minimumActivity, setMinimumActivity] = useState(0)
  const [minimumSecurity, setMinimumSecurity] = useState(0)
  const [sortOption, setSortOption] = useState<SortOption>("health-desc")
  const [activeView, setActiveView] = useState<RankingView>("overall")

  const languages = useMemo(
    () =>
      [
        ...new Set(repositories.map((repository) => repository.language)),
      ].sort(),
    [repositories],
  )

  const visibleRepositories = useMemo(() => {
    const normalizedQuery = searchQuery.trim().toLocaleLowerCase()
    const matchingRepositories = repositories.filter((repository) => {
      const matchesSearch =
        normalizedQuery.length === 0 ||
        repository.fullName.toLocaleLowerCase().includes(normalizedQuery) ||
        repository.language.toLocaleLowerCase().includes(normalizedQuery)
      const matchesLanguage =
        selectedLanguage === "all" || repository.language === selectedLanguage

      return (
        matchesSearch &&
        matchesLanguage &&
        repository.healthScore >= minimumHealth &&
        repository.categoryScores.activity.score >= minimumActivity &&
        repository.categoryScores.security.score >= minimumSecurity
      )
    })

    return sortRepositories(matchingRepositories, activeView, sortOption)
  }, [
    activeView,
    minimumActivity,
    minimumHealth,
    minimumSecurity,
    repositories,
    searchQuery,
    selectedLanguage,
    sortOption,
  ])

  const averageScore = Math.round(
    repositories.reduce(
      (total, repository) => total + repository.healthScore,
      0,
    ) / Math.max(repositories.length, 1),
  )
  const healthyShare = Math.round(
    (repositories.filter((repository) => repository.healthScore >= 75).length /
      Math.max(repositories.length, 1)) *
      100,
  )

  return (
    <div className="page-stack">
      <PageHeading
        eyebrow="Открытые репозитории"
        title="Рейтинг здоровья"
        description="Сравнивайте состояние проектов и открывайте подробный отчёт по репозиторию."
      />

      <div className="stat-grid">
        <Card className="stat-card">
          <span>В выборке</span>
          <strong>{repositories.length}</strong>
        </Card>
        <Card className="stat-card">
          <span>Средний Repo Health Score</span>
          <strong>{averageScore}</strong>
        </Card>
        <Card className="stat-card">
          <span>Score от 75</span>
          <strong>{healthyShare}%</strong>
        </Card>
      </div>

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
              placeholder="Найти репозиторий или язык"
              value={searchQuery}
              onChange={(event) => setSearchQuery(event.target.value)}
            />
          </label>

          <label className="filter-field">
            <span>Язык</span>
            <select
              value={selectedLanguage}
              onChange={(event) => setSelectedLanguage(event.target.value)}
            >
              <option value="all">Все</option>
              {languages.map((language) => (
                <option key={language} value={language}>
                  {language}
                </option>
              ))}
            </select>
          </label>
          <label className="filter-field">
            <span>Score</span>
            <select
              value={minimumHealth}
              onChange={(event) => setMinimumHealth(Number(event.target.value))}
            >
              <option value={0}>Любой</option>
              <option value={70}>От 70</option>
              <option value={80}>От 80</option>
              <option value={90}>От 90</option>
            </select>
          </label>
          <label className="filter-field">
            <span>Активность</span>
            <select
              value={minimumActivity}
              onChange={(event) =>
                setMinimumActivity(Number(event.target.value))
              }
            >
              <option value={0}>Любая</option>
              <option value={70}>От 70</option>
              <option value={80}>От 80</option>
              <option value={90}>От 90</option>
            </select>
          </label>
          <label className="filter-field">
            <span>Безопасность</span>
            <select
              value={minimumSecurity}
              onChange={(event) =>
                setMinimumSecurity(Number(event.target.value))
              }
            >
              <option value={0}>Любая</option>
              <option value={60}>От 60</option>
              <option value={75}>От 75</option>
              <option value={90}>От 90</option>
            </select>
          </label>
          <label className="filter-field filter-field--sort">
            <span>Сортировка</span>
            <select
              value={sortOption}
              onChange={(event) =>
                setSortOption(event.target.value as SortOption)
              }
            >
              <option value="health-desc">Score: выше сначала</option>
              <option value="health-asc">Score: ниже сначала</option>
              <option value="likes-desc">Лайки</option>
              <option value="activity-recent">Последняя активность</option>
            </select>
          </label>
        </div>

        <div
          className="ranking-tabs"
          role="tablist"
          aria-label="Вариант рейтинга"
        >
          {rankingViews.map((view) => (
            <button
              key={view.id}
              className={`ranking-tab ${
                activeView === view.id ? "ranking-tab--active" : ""
              }`}
              type="button"
              role="tab"
              aria-selected={activeView === view.id}
              onClick={() => setActiveView(view.id)}
            >
              {view.label}
            </button>
          ))}
          <span className="ranking-count">
            {visibleRepositories.length} проектов
          </span>
        </div>

        <div className="table-scroll">
          <div className="ranking-table" aria-label="Рейтинг репозиториев">
            <div className="ranking-row ranking-row--header">
              <span>#</span>
              <span>Репозиторий</span>
              <span>Язык</span>
              <span>Health</span>
              <span>Лайки</span>
              <span>Активность</span>
              <span>Security</span>
              <span>Статус</span>
            </div>
            {visibleRepositories.map((repository, index) => (
              <button
                className="ranking-row ranking-row--button"
                key={repository.id}
                type="button"
                onClick={() => onSelectRepository(repository.id)}
                aria-label={`Открыть анализ ${repository.fullName}`}
              >
                <span className="table-rank">{index + 1}</span>
                <span className="repository-cell">
                  <strong>{repository.name}</strong>
                  <small>{repository.fullName}</small>
                </span>
                <span>{repository.language}</span>
                <strong className="table-score">
                  {repository.healthScore}
                </strong>
                <span>{repository.likes.toLocaleString("ru-RU")}</span>
                <span>
                  {getLastActivityLabel(repository.lastActivityDaysAgo)}
                </span>
                <span>{repository.categoryScores.security.score}</span>
                <span>
                  <Badge tone={getStatusTone(repository.healthScore)}>
                    {getHealthLabel(repository.healthScore)}
                  </Badge>
                </span>
              </button>
            ))}
          </div>
        </div>

        {visibleRepositories.length === 0 && (
          <div className="empty-state">
            <strong>Ничего не найдено</strong>
            <p>Попробуйте изменить поисковый запрос или фильтры.</p>
            <Button
              variant="quiet"
              onClick={() => {
                setSearchQuery("")
                setSelectedLanguage("all")
                setMinimumHealth(0)
                setMinimumActivity(0)
                setMinimumSecurity(0)
              }}
            >
              Сбросить фильтры
            </Button>
          </div>
        )}
      </Card>
      <p className="data-caption">
        Показатели отображаются через единый источник данных и могут быть
        заменены на API Gateway.
      </p>
    </div>
  )
}
