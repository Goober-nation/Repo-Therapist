import { useCallback, useEffect, useMemo, useState } from "react"
import { ApiRequestError, HttpRepoHealthApi } from "./api/client"
import type { GradingResponse } from "./api/contracts"
import { mergeGradingIntoRepository, repositoryFromMyRepos, repositoryFromRanking, repositoryStub } from "./api/mapper"
import { loadSavedProfile, saveProfile, type YandexProfile } from "./api/yandexAuth"
import { Button, Card } from "./components/Shell"
import Sidebar from "./components/Sidebar"
import TopBar from "./components/TopBar"
import MyRepositoriesPage from "./pages/MyRepositoriesPage"
import RankingPage, { DEFAULT_RANKING_QUERY, type RankingQuery } from "./pages/RankingPage"
import RepositoryOverviewPage from "./pages/RepositoryOverviewPage"
import { parseRepositoryLink, useRoute } from "./route"
import type { PageId, RepositorySummary } from "./types"
import { useRepositoryAnalysis } from "./useRepositoryAnalysis"

type Theme = "light" | "dark" | "dopamine"

const THEMES: Theme[] = ["light", "dark", "dopamine"]
const THEME_KEY = "repo-health-theme"
const api = new HttpRepoHealthApi()
/** Placeholder the mapper uses when SourceCraft reports no language; not a server-side filter value. */
const UNKNOWN_LANGUAGE = "Не указан"

interface RepositoryList {
  items: RepositorySummary[]
  isLoading: boolean
  error?: string
}

function readTheme(): Theme {
  try {
    const saved = window.localStorage.getItem(THEME_KEY) as Theme
    return THEMES.includes(saved) ? saved : "light"
  } catch {
    return "light"
  }
}

function errorText(error: unknown, fallback: string) {
  return error instanceof ApiRequestError ? error.message : fallback
}

export default function App() {
  const [route, navigate] = useRoute()
  const [theme, setTheme] = useState(readTheme)
  const [isSidebarOpen, setIsSidebarOpen] = useState(false)
  const [profile, setProfile] = useState<YandexProfile | null>(loadSavedProfile)
  // Kept in memory only: never written to storage, gone after a reload.
  const [pat, setPat] = useState<string>()
  const [rankingQuery, setRankingQuery] = useState<RankingQuery>(DEFAULT_RANKING_QUERY)
  const [rankingAttempt, setRankingAttempt] = useState(0)
  const [ranking, setRanking] = useState<RepositoryList & { total: number; loadedKey?: string }>({
    items: [],
    total: 0,
    isLoading: true,
  })
  const [languages, setLanguages] = useState<string[]>([])
  const [mine, setMine] = useState<RepositoryList>({ items: [], isLoading: false })
  const [gradings, setGradings] = useState<Record<string, GradingResponse>>({})

  useEffect(() => {
    document.documentElement.dataset.theme = theme
    try {
      window.localStorage.setItem(THEME_KEY, theme)
    } catch {
      // Storage can be unavailable (private mode); the theme still applies for this visit.
    }
  }, [theme])

  // One ranking page per request, loaded when the ranking is shown; a page already on screen
  // is not fetched again when the user comes back to it.
  const rankingKey = `${rankingQuery.page}|${rankingQuery.sort}|${rankingQuery.language}|${rankingAttempt}`
  useEffect(() => {
    if (route.page !== "ranking" || ranking.loadedKey === rankingKey) return
    let active = true
    setRanking((current) => ({ ...current, isLoading: true, error: undefined }))
    api
      .getRanking(rankingQuery.page, rankingQuery.sort, rankingQuery.language)
      .then((response) => {
        if (!active) return
        const items = response.entries.flatMap((entry) => repositoryFromRanking(entry) ?? [])
        setRanking({ items, total: response.total_count, isLoading: false, loadedKey: rankingKey })
        const named = items.flatMap((item) => (item.language === UNKNOWN_LANGUAGE ? [] : [item.language]))
        setLanguages((current) => [...new Set([...current, ...named])])
      })
      .catch((error) => {
        if (active) {
          setRanking({ items: [], total: 0, isLoading: false, error: errorText(error, "Не удалось загрузить рейтинг.") })
        }
      })
    return () => {
      active = false
    }
  }, [route.page, rankingKey, ranking.loadedKey, rankingQuery])

  useEffect(() => {
    if (!pat) return
    let active = true
    setMine({ items: [], isLoading: true })
    api
      .getMyRepositories(pat)
      .then((response) => {
        if (active) setMine({ items: response.repositories.map(repositoryFromMyRepos), isLoading: false })
      })
      .catch((error) => {
        if (!active) return
        setMine({ items: [], isLoading: false, error: errorText(error, "Не удалось получить список репозиториев.") })
        if (error instanceof ApiRequestError && (error.status === 401 || error.status === 403)) setPat(undefined)
      })
    return () => {
      active = false
    }
  }, [pat])

  const target = route.page === "repository" ? route : undefined
  const key = target && `${target.private ? "my" : "repo"}:${target.org}/${target.repo}`
  const needsToken = Boolean(target?.private && !pat)

  const baseRepository = useMemo(() => {
    if (!target) return undefined
    const list = target.private ? mine.items : ranking.items
    return (
      list.find((item) => item.orgSlug === target.org && item.repoSlug === target.repo) ??
      repositoryStub(target.org, target.repo, target.private)
    )
  }, [target, mine.items, ranking.items])

  const grading = key ? gradings[key] : undefined
  const repository = useMemo(
    () => (baseRepository && grading ? mergeGradingIntoRepository(baseRepository, grading) : baseRepository),
    [baseRepository, grading],
  )

  const handleGrading = useCallback(
    (response: GradingResponse) => {
      if (!key) return
      // A timed-out rerun answers PENDING; keep the last completed grade on screen.
      setGradings((current) =>
        response.status === "PENDING" && current[key] ? current : { ...current, [key]: response },
      )
    },
    [key],
  )

  const analysis = useRepositoryAnalysis(
    api,
    target && !needsToken ? { org: target.org, repo: target.repo, pat: target.private ? pat : undefined } : undefined,
    repository?.gradedAt,
    handleGrading,
  )

  useEffect(() => {
    document.title = repository ? `${repository.fullName} — Repo Therapist` : "Repo Therapist — здоровье репозиториев SourceCraft"
  }, [repository?.fullName])

  const handleSignIn = useCallback(
    (signedIn: YandexProfile) => {
      saveProfile(signedIn)
      setProfile(signedIn)
      navigate({ page: "my" })
    },
    [navigate],
  )

  const openRepository = (item: RepositorySummary, isPrivate: boolean) => {
    if (item.orgSlug && item.repoSlug) {
      navigate({ page: "repository", org: item.orgSlug, repo: item.repoSlug, private: isPrivate })
    }
  }

  const openRepositoryLink = (link: string) => {
    const parsed = parseRepositoryLink(link)
    if (parsed) navigate({ page: "repository", ...parsed, private: false })
    return parsed !== null
  }

  const showPage = (page: Exclude<PageId, "overview">) => navigate({ page })

  const activePage: PageId = route.page === "repository" ? "overview" : route.page

  return (
    <div className="app-shell">
      <TopBar
        activePage={activePage}
        repositoryName={repository?.name ?? ""}
        theme={theme}
        onToggleTheme={() => setTheme((current) => THEMES[(THEMES.indexOf(current) + 1) % THEMES.length])}
        onPageChange={showPage}
        isSidebarOpen={isSidebarOpen}
        onToggleSidebar={() => setIsSidebarOpen((open) => !open)}
        onOpenRepositoryLink={openRepositoryLink}
        yandexProfile={profile}
        onYandexSignIn={handleSignIn}
      />
      <div className="app-body">
        {isSidebarOpen && <Sidebar activePage={activePage} onPageChange={showPage} />}
        <div className="app-workspace">
          <main className="page-content">
            {route.page === "ranking" && (
              <RankingPage
                repositories={ranking.items}
                total={ranking.total}
                query={rankingQuery}
                languages={languages}
                isLoading={ranking.isLoading}
                error={ranking.error}
                onQueryChange={setRankingQuery}
                onRetry={() => setRankingAttempt((value) => value + 1)}
                onSelectRepository={(item) => openRepository(item, false)}
              />
            )}
            {route.page === "my" && (
              <MyRepositoriesPage
                isSignedIn={profile !== null}
                hasToken={Boolean(pat)}
                repositories={mine.items}
                isLoading={mine.isLoading}
                error={mine.error}
                onSubmitToken={(token) => setPat(token.trim())}
                onForgetToken={() => {
                  setPat(undefined)
                  setMine({ items: [], isLoading: false })
                }}
                onOpenRepository={(item) => openRepository(item, true)}
              />
            )}
            {route.page === "repository" &&
              (needsToken ? (
                <Card className="empty-state">
                  <strong>Нужен токен SourceCraft</strong>
                  <p>Приватный анализ использует ваш токен, а он хранится только до перезагрузки страницы.</p>
                  <Button variant="primary" onClick={() => showPage("my")}>
                    Ввести токен
                  </Button>
                </Card>
              ) : (
                repository && (
                  <RepositoryOverviewPage
                    repository={repository}
                    analysis={analysis}
                    onBackToRanking={() => showPage("ranking")}
                  />
                )
              ))}
          </main>
        </div>
      </div>
    </div>
  )
}
