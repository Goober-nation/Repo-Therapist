import { useEffect, useMemo, useState } from "react"
import Sidebar from "./components/Sidebar"
import TopBar from "./components/TopBar"
import RankingPage from "./pages/RankingPage"
import RepositoryOverviewPage from "./pages/RepositoryOverviewPage"
import SecurityPage from "./pages/SecurityPage"
import {
  recommendationsFromResponse,
  mergeGradingIntoRepository,
} from "./api/mapper"
import { createRepositoryDataSource } from "./data/repositoryData"
import type { PageId, Recommendation, RepositorySummary } from "./types"

const DEFAULT_REPOSITORY_ID = "awesome-project"
type Theme = "light" | "dark" | "dopamine"

export default function App() {
  const repositoryDataSource = useMemo(createRepositoryDataSource, [])
  const [activePage, setActivePage] = useState<PageId>("overview")
  const [selectedRepositoryId, setSelectedRepositoryId] = useState(
    DEFAULT_REPOSITORY_ID,
  )
  const [repositories, setRepositories] = useState<RepositorySummary[]>([])
  const [recommendations, setRecommendations] =
    useState<Recommendation[] | undefined>()
  const [isLoading, setIsLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [isAuthenticated, setIsAuthenticated] = useState(false)
  const [theme, setTheme] = useState<Theme>(() => {
    if (typeof window === "undefined") return "light"
    const savedTheme = window.localStorage.getItem("repo-health-theme")
    return savedTheme === "dark" || savedTheme === "dopamine"
      ? savedTheme
      : "light"
  })
  const [isAnalysisRunning, setIsAnalysisRunning] = useState(false)
  const [analysisRefreshed, setAnalysisRefreshed] = useState(false)
  const [analysisStatus, setAnalysisStatus] =
    useState<"PENDING" | "FINISHED" | null>(null)

  useEffect(() => {
    document.documentElement.dataset.theme = theme
    window.localStorage.setItem("repo-health-theme", theme)
  }, [theme])

  useEffect(() => {
    let isCurrent = true
    repositoryDataSource
      .listRepositories()
      .then((items) => {
        if (!isCurrent) return
        setRepositories(items)
        if (
          items.length > 0 &&
          !items.some((item) => item.id === selectedRepositoryId)
        ) {
          setSelectedRepositoryId(items[0].id)
        }
      })
      .catch(() => {
        if (isCurrent) setLoadError("Не удалось загрузить список репозиториев.")
      })
      .finally(() => {
        if (isCurrent) setIsLoading(false)
      })
    return () => {
      isCurrent = false
    }
  }, [repositoryDataSource, selectedRepositoryId])

  const selectedRepository =
    repositories.find((repository) => repository.id === selectedRepositoryId) ??
    repositories[0]

  useEffect(() => {
    if (!selectedRepository) return
    let isCurrent = true
    setRecommendations(undefined)
    repositoryDataSource
      .getRecommendations(selectedRepository, import.meta.env.VITE_USER_TOKEN)
      .then((response) => {
        if (!isCurrent) return
        setRecommendations(
          recommendationsFromResponse(response).map((item, index) => ({
            id: `${selectedRepository.id}-recommendation-${index}`,
            priority: item.priority === "high" ? "high" : "medium",
            title: item.title ?? item.subject1 ?? "Рекомендация",
            description:
              item.description ?? "Описание рекомендации пока не передано API.",
            currentScore: item.current_score ?? selectedRepository.healthScore,
            projectedScore:
              item.projected_score ?? selectedRepository.healthScore,
          })),
        )
      })
      .catch(() => {
        if (isCurrent) setRecommendations([])
      })
    return () => {
      isCurrent = false
    }
  }, [repositoryDataSource, selectedRepository])

  function openRepository(repositoryId: string) {
    setSelectedRepositoryId(repositoryId)
    setAnalysisRefreshed(false)
    setAnalysisStatus(null)
    setActivePage("overview")
  }

  async function runAnalysis() {
    if (isAnalysisRunning || !selectedRepository) return

    setIsAnalysisRunning(true)
    setAnalysisRefreshed(false)
    try {
      const grading = await repositoryDataSource.getGrading(
        selectedRepository,
        import.meta.env.VITE_USER_TOKEN,
      )
      setRepositories((current) =>
        current.map((repository) =>
          repository.id === selectedRepository.id
            ? mergeGradingIntoRepository(repository, grading)
            : repository,
        ),
      )
      setIsAnalysisRunning(false)
      setAnalysisStatus(grading.status)
      setAnalysisRefreshed(true)
    } catch {
      setIsAnalysisRunning(false)
      setLoadError("Не удалось получить оценку репозитория.")
    }
  }

  if (isLoading)
    return <div className="app-loading">Загружаем репозитории…</div>
  if (!selectedRepository)
    return (
      <div className="app-loading">
        {loadError ?? "Репозитории не найдены."}
      </div>
    )

  return (
    <div className="app-shell">
      <Sidebar
        activePage={activePage}
        onPageChange={setActivePage}
        isAuthenticated={isAuthenticated}
        onSignIn={() => setIsAuthenticated(true)}
        onSignOut={() => setIsAuthenticated(false)}
      />
      <div className="app-workspace">
        <TopBar
          activePage={activePage}
          repositoryName={selectedRepository.name}
          theme={theme}
          onToggleTheme={() =>
            setTheme((currentTheme) => {
              if (currentTheme === "light") return "dark"
              if (currentTheme === "dark") return "dopamine"
              return "light"
            })
          }
        />
        {loadError && (
          <div className="inline-notice inline-notice--error" role="alert">
            {loadError}
          </div>
        )}
        <main className="page-content">
          {activePage === "ranking" ? (
            <RankingPage
              repositories={repositories}
              onSelectRepository={openRepository}
            />
          ) : activePage === "security" ? (
            <SecurityPage
              repository={selectedRepository}
              onBack={() => setActivePage("overview")}
            />
          ) : (
            <RepositoryOverviewPage
              repository={selectedRepository}
              isAnalysisRunning={isAnalysisRunning}
              analysisRefreshed={analysisRefreshed}
              analysisStatus={analysisStatus}
              recommendations={recommendations}
              onOpenSecurity={() => setActivePage("security")}
              onRunAnalysis={runAnalysis}
            />
          )}
        </main>
      </div>
    </div>
  )
}
