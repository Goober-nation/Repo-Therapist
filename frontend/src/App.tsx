import { useCallback, useEffect, useMemo, useState } from "react"
import { ApiRequestError, HttpRepoHealthApi } from "./api/client"
import type { GradingResponse, MyOrganization } from "./api/contracts"
import {
  analyticsFromResponse,
  confidenceFromPayload,
  defaultOrganization,
  historyFromResponse,
  mergeGradingIntoRepository,
  positionFromResponse,
  repositoryFromMyRepos,
  repositoryFromRanking,
  repositoryStub,
} from "./api/mapper"
import { loadSavedProfile, saveProfile, type YandexProfile } from "./api/yandexAuth"
import { Button, Card } from "./components/Shell"
import Sidebar from "./components/Sidebar"
import TopBar from "./components/TopBar"
import AdminAccessPage from "./pages/AdminAccessPage"
import AnalyticsPage from "./pages/AnalyticsPage"
import CompareRoute from "./pages/CompareRoute"
import MyRepositoriesPage from "./pages/MyRepositoriesPage"
import { useRegradeStatus } from "./useRegradeStatus"
import { sessionLost } from "./session"
import { useAdmin } from "./useAdmin"
import { useStats } from "./useStats"
import RankingPage, { DEFAULT_RANKING_QUERY, rankingFilters, type RankingQuery } from "./pages/RankingPage"
import RepositoryOverviewPage from "./pages/RepositoryOverviewPage"
import RulesPage from "./pages/RulesPage"
import { gradingToken } from "./metrics"
import { loadPat, patExpiresIn, savePat } from "./patStorage"
import { parseRepositoryLink, useRoute } from "./route"
import type { Confidence, PageId, PositionExplanation, RepoAnalytics, RepositorySummary, ScorePoint } from "./types"
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

interface OrganizationList {
  items: MyOrganization[]
  selected?: string
  isLoading: boolean
}

/** Repositories of one organization; nextPageToken continues the list. */
interface MyRepositoryList extends RepositoryList {
  org?: string
  nextPageToken?: string
  isLoadingMore?: boolean
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
  // Saved in localStorage for one hour (see patStorage), then dropped.
  const [pat, setPat] = useState<string | undefined>(loadPat)
  const [rankingQuery, setRankingQuery] = useState<RankingQuery>(DEFAULT_RANKING_QUERY)
  // Low-confidence flag of the public grade of the opened repository (GET /score); null = unknown.
  const [confidences, setConfidences] = useState<Record<string, Confidence | null>>({})
  // Informational analytics per view ("my:" with the owner's token, "repo:" public); null = none yet.
  const [analyticsByKey, setAnalyticsByKey] = useState<Record<string, RepoAnalytics | null>>({})
  // Place and score history of a public repository ("org/repo"); null = none yet, absent = loading.
  const [historyByRepo, setHistoryByRepo] = useState<Record<string, ScorePoint[] | null>>({})
  const [positionByRepo, setPositionByRepo] = useState<Record<string, PositionExplanation | null>>({})
  // Repositories ticked in the ranking for comparison (at most two).
  const [compareSelection, setCompareSelection] = useState<RepositorySummary[]>([])
  const regradeStatus = useRegradeStatus(api, route.page === "ranking")
  const admin = useAdmin(api, profile)
  const [statsAttempt, setStatsAttempt] = useState(0)
  const stats = useStats(api, route.page === "analytics", statsAttempt)
  const [rankingAttempt, setRankingAttempt] = useState(0)
  const [ranking, setRanking] = useState<RepositoryList & { total: number; loadedKey?: string; dynamicsDays?: number }>({
    items: [],
    total: 0,
    isLoading: true,
  })
  const [languages, setLanguages] = useState<string[]>([])
  const [orgs, setOrgs] = useState<OrganizationList>({ items: [], isLoading: false })
  const [mine, setMine] = useState<MyRepositoryList>({ items: [], isLoading: false })
  const [gradings, setGradings] = useState<Record<string, GradingResponse>>({})
  // Repositories ("org/repo") the owner switched to the public grade.
  const [publicViews, setPublicViews] = useState<Record<string, boolean>>({})

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
  const rankingKey = `${rankingQuery.page}|${rankingQuery.sort}|${rankingQuery.language}|${rankingQuery.hideLowConfidence}|${rankingQuery.size}|${rankingQuery.ci}|${rankingQuery.security}|${rankingQuery.topic}|${rankingAttempt}`
  useEffect(() => {
    if (route.page !== "ranking" || ranking.loadedKey === rankingKey) return
    let active = true
    setRanking((current) => ({ ...current, isLoading: true, error: undefined }))
    api
      .getRanking(
        rankingQuery.page,
        rankingQuery.sort,
        rankingQuery.language,
        rankingQuery.hideLowConfidence,
        rankingFilters(rankingQuery),
      )
      .then((response) => {
        if (!active) return
        const items = response.entries.flatMap((entry) => repositoryFromRanking(entry) ?? [])
        setRanking({
          items,
          total: response.total_count,
          isLoading: false,
          loadedKey: rankingKey,
          dynamicsDays: typeof response.dynamics_days === "number" ? response.dynamics_days : undefined,
        })
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
    savePat(pat)
    if (!pat) return
    // Drop the token from memory too once its saved copy expires.
    const expiry = window.setTimeout(() => setPat(undefined), patExpiresIn() ?? 60 * 60 * 1000)
    return () => window.clearTimeout(expiry)
  }, [pat])

  // Organizations of the token owner; the default one (personal, else first) opens right away.
  useEffect(() => {
    if (!pat) return
    let active = true
    setOrgs({ items: [], isLoading: true })
    setMine({ items: [], isLoading: true })
    api
      .getMyOrganizations(pat)
      .then((response) => {
        if (!active) return
        const selected = defaultOrganization(response)
        setOrgs({ items: response.organizations ?? [], selected, isLoading: false })
        if (!selected) setMine({ items: [], isLoading: false })
      })
      .catch((error) => {
        if (!active) return
        setOrgs({ items: [], isLoading: false })
        setMine({ items: [], isLoading: false, error: errorText(error, "Не удалось получить список организаций.") })
        if (error instanceof ApiRequestError && (error.status === 401 || error.status === 403)) setPat(undefined)
      })
    return () => {
      active = false
    }
  }, [pat])

  // Repositories of the selected organization only, one batch at a time.
  useEffect(() => {
    const org = orgs.selected
    if (!pat || !org) return
    let active = true
    setMine({ items: [], isLoading: true, org })
    api
      .getMyRepositories(pat, org)
      .then((response) => {
        if (!active) return
        setMine({
          items: response.repositories.map(repositoryFromMyRepos),
          isLoading: false,
          org,
          nextPageToken: response.next_page_token || undefined,
        })
      })
      .catch((error) => {
        if (!active) return
        setMine({ items: [], isLoading: false, org, error: errorText(error, "Не удалось получить список репозиториев.") })
        if (error instanceof ApiRequestError && error.status === 401) setPat(undefined)
      })
    return () => {
      active = false
    }
  }, [pat, orgs.selected])

  const loadMoreRepositories = useCallback(() => {
    const { org, nextPageToken } = mine
    if (!pat || !org || !nextPageToken || mine.isLoadingMore) return
    setMine((current) => ({ ...current, isLoadingMore: true, error: undefined }))
    api
      .getMyRepositories(pat, org, nextPageToken)
      .then((response) => {
        setMine((current) => {
          if (current.org !== org || current.nextPageToken !== nextPageToken) return current
          const known = new Set(current.items.map((item) => item.id))
          const added = response.repositories.map(repositoryFromMyRepos).filter((item) => !known.has(item.id))
          return {
            ...current,
            items: [...current.items, ...added],
            isLoadingMore: false,
            nextPageToken: response.next_page_token || undefined,
          }
        })
      })
      .catch((error) => {
        setMine((current) =>
          current.org === org
            ? { ...current, isLoadingMore: false, error: errorText(error, "Не удалось загрузить ещё репозитории.") }
            : current,
        )
      })
  }, [pat, mine])

  const target = route.page === "repository" ? route : undefined
  const needsToken = Boolean(target?.private && !pat)

  const baseRepository = useMemo(() => {
    if (!target) return undefined
    const list = target.private ? mine.items : ranking.items
    return (
      list.find((item) => item.orgSlug === target.org && item.repoSlug === target.repo) ??
      repositoryStub(target.org, target.repo, target.private)
    )
  }, [target, mine.items, ranking.items])

  const repoName = target && `${target.org}/${target.repo}`
  const publicView = Boolean(repoName && publicViews[repoName])
  const token = target ? gradingToken(pat, target.private, baseRepository?.visibility, publicView) : undefined
  const canViewPublicly = Boolean(target?.private && pat && baseRepository?.visibility === "public")
  const key = target && `${token ? "my" : "repo"}:${repoName}`
  const grading = key ? gradings[key] : undefined
  const publicConfidence = repoName ? confidences[repoName] : undefined
  const repository = useMemo(() => {
    if (!baseRepository) return undefined
    const merged = grading ? mergeGradingIntoRepository(baseRepository, grading) : baseRepository
    // The flag belongs to public grades: the owner's private grade never shows it.
    const withFlag = token
      ? { ...merged, confidence: undefined }
      : publicConfidence
        ? { ...merged, confidence: publicConfidence }
        : merged
    return target?.private && !token ? { ...withFlag, analysisMode: "public" as const } : withFlag
  }, [baseRepository, grading, target?.private, token, publicConfidence])

  // The flag is advisory and read separately from the grade: a failure just leaves it unknown.
  const publicGradeShown = Boolean(target && !token && grading?.status === "FINISHED")
  const gradedAtKey = grading?.grade?.graded_at
  useEffect(() => {
    if (!target || !repoName || !publicGradeShown) return
    let active = true
    api
      .getPublicScore(target.org, target.repo)
      .then((response) => {
        if (active) {
          setConfidences((current) => ({
            ...current,
            [repoName]: confidenceFromPayload(response.low_confidence, response.confidence_reasons) ?? null,
          }))
        }
      })
      .catch(() => {})
    return () => {
      active = false
    }
  }, [target?.org, target?.repo, repoName, publicGradeShown, gradedAtKey])

  // The place and the score history belong to the public grade and are advisory like the flag: a
  // failure leaves them empty ("none yet"). Refetched when the grade is new.
  useEffect(() => {
    if (!target || !repoName || !publicGradeShown) return
    let active = true
    api
      .getHistory(target.org, target.repo)
      .then((response) => {
        if (active) setHistoryByRepo((current) => ({ ...current, [repoName]: historyFromResponse(response) }))
      })
      .catch(() => {
        if (active) setHistoryByRepo((current) => ({ ...current, [repoName]: null }))
      })
    api
      .getPosition(target.org, target.repo)
      .then((response) => {
        if (active) setPositionByRepo((current) => ({ ...current, [repoName]: positionFromResponse(response) }))
      })
      .catch(() => {
        if (active) setPositionByRepo((current) => ({ ...current, [repoName]: null }))
      })
    return () => {
      active = false
    }
  }, [target?.org, target?.repo, repoName, publicGradeShown, gradedAtKey])

  // Analytics are read separately from the grade and refetched when the grade is new; a failure
  // just leaves them out (advisory data).
  const gradeReady = Boolean(target && !needsToken && grading?.status === "FINISHED")
  const analyticsGradedAt = grading?.grade?.graded_at
  useEffect(() => {
    if (!target || !key || !gradeReady) return
    let active = true
    api
      .getAnalytics(target.org, target.repo, token)
      .then((response) => {
        if (active) setAnalyticsByKey((current) => ({ ...current, [key]: analyticsFromResponse(response) }))
      })
      .catch(() => {
        if (active) setAnalyticsByKey((current) => ({ ...current, [key]: null }))
      })
    return () => {
      active = false
    }
  }, [target?.org, target?.repo, key, token, gradeReady, analyticsGradedAt])

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
    target && !needsToken ? { org: target.org, repo: target.repo, pat: token } : undefined,
    repository?.gradedAt,
    handleGrading,
  )

  useEffect(() => {
    document.title = repository ? `${repository.fullName} — Repo Therapist` : "Repo Therapist — здоровье репозиториев SourceCraft"
  }, [repository?.fullName])

  // Signing out ends the Gateway session and forgets everything tied to the user in this tab: the saved
  // profile, the SourceCraft token and the private data loaded with it. The local part never waits for
  // the network, so a failed request cannot leave the user looking signed in.
  const handleSignOut = useCallback(() => {
    api.logout().catch(() => undefined)
    saveProfile(null)
    setProfile(null)
    setPat(undefined)
    setOrgs({ items: [], isLoading: false })
    setMine({ items: [], isLoading: false })
    setGradings({})
    setAnalyticsByKey({})
    setPublicViews({})
    navigate({ page: "ranking" })
  }, [navigate])

  // The saved profile is display data kept in the tab; when the Gateway says the session is gone (it
  // expired, or the server lost it), drop the profile so the interface stops claiming to be signed in.
  useEffect(() => {
    if (!sessionLost(profile, admin.status)) return
    saveProfile(null)
    setProfile(null)
  }, [profile, admin.status])

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

  const toggleCompare = (item: RepositorySummary) =>
    setCompareSelection((current) =>
      current.some((selected) => selected.id === item.id)
        ? current.filter((selected) => selected.id !== item.id)
        : current.length >= 2
          ? current
          : [...current, item],
    )

  const openCompare = () => {
    const [first, second] = compareSelection
    if (first?.orgSlug && first.repoSlug && second?.orgSlug && second.repoSlug) {
      navigate({
        page: "compare",
        a: { org: first.orgSlug, repo: first.repoSlug },
        b: { org: second.orgSlug, repo: second.repoSlug },
      })
    }
  }

  // The compare page is reached from the ranking, so the ranking stays highlighted.
  const activePage: PageId = route.page === "repository" ? "overview" : route.page === "compare" ? "ranking" : route.page

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
        onSignOut={handleSignOut}
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
                dynamicsDays={ranking.dynamicsDays}
                regradeStatus={regradeStatus}
                compareSelection={compareSelection}
                onToggleCompare={toggleCompare}
                onCompare={openCompare}
                onClearCompare={() => setCompareSelection([])}
                onQueryChange={setRankingQuery}
                onRetry={() => setRankingAttempt((value) => value + 1)}
                onSelectRepository={(item) => openRepository(item, false)}
              />
            )}
            {route.page === "analytics" && (
              <AnalyticsPage
                api={api}
                stats={stats.data}
                isLoading={stats.isLoading}
                error={stats.error}
                onRetry={() => setStatsAttempt((value) => value + 1)}
                onOpenRepository={(org, repo) => navigate({ page: "repository", org, repo, private: false })}
              />
            )}
            {route.page === "access" && (
              <AdminAccessPage
                api={api}
                status={admin.status}
                error={admin.error}
                onRetry={admin.retry}
                onStatusChange={admin.setStatus}
              />
            )}
            {route.page === "my" && (
              <MyRepositoriesPage
                isSignedIn={profile !== null}
                hasToken={Boolean(pat)}
                repositories={mine.items}
                isLoading={mine.isLoading}
                error={mine.error}
                organizations={orgs.items}
                selectedOrganization={orgs.selected}
                hasMore={Boolean(mine.nextPageToken)}
                isLoadingMore={Boolean(mine.isLoadingMore)}
                onSelectOrganization={(slug) => setOrgs((current) => ({ ...current, selected: slug }))}
                onLoadMore={loadMoreRepositories}
                onSubmitToken={(token) => setPat(token.trim())}
                onForgetToken={() => {
                  setPat(undefined)
                  setOrgs({ items: [], isLoading: false })
                  setMine({ items: [], isLoading: false })
                }}
                onOpenRepository={(item) => openRepository(item, true)}
              />
            )}
            {route.page === "compare" && (
              <CompareRoute
                api={api}
                a={route.a}
                b={route.b}
                onNavigate={(a, b) => navigate({ page: "compare", a, b })}
                onOpenRepository={(ref) => navigate({ page: "repository", org: ref.org, repo: ref.repo, private: false })}
                onBack={() => showPage("ranking")}
              />
            )}
            {route.page === "rules" && (
              <RulesPage section={route.section} onSectionChange={(section) => navigate({ page: "rules", section })} />
            )}
            {route.page === "repository" &&
              (needsToken ? (
                <Card className="empty-state">
                  <strong>Нужен токен SourceCraft</strong>
                  <p>Приватный анализ использует ваш токен, он хранится в этом браузере один час.</p>
                  <Button variant="primary" onClick={() => showPage("my")}>
                    Ввести токен
                  </Button>
                </Card>
              ) : (
                repository && (
                  <RepositoryOverviewPage
                    repository={repository}
                    analysis={analysis}
                    analytics={key ? analyticsByKey[key] : undefined}
                    rankingInfo={
                      !token && repoName
                        ? { history: historyByRepo[repoName], position: positionByRepo[repoName] }
                        : undefined
                    }
                    onOpenRepository={(org, repo) => navigate({ page: "repository", org, repo, private: false })}
                    onBackToRanking={() => showPage("ranking")}
                    canForceRegrade={profile !== null}
                    publicView={
                      canViewPublicly && repoName
                        ? {
                            checked: publicView,
                            onChange: (checked) => setPublicViews((current) => ({ ...current, [repoName]: checked })),
                          }
                        : undefined
                    }
                  />
                )
              ))}
          </main>
        </div>
      </div>
    </div>
  )
}
