import { useEffect, useState, type ReactNode } from "react"
import { SEARCH_PAGE_SIZE } from "../api/client"
import type { CatalogOrgResponse, CatalogRepoResponse, SearchOrgsResponse, SearchReposResponse } from "../api/contracts"
import CatalogSearch, { repoCountLabel, type SuggestApi } from "../components/CatalogSearch"
import { Badge, Button, Card, LanguageLabel, PageHeading } from "../components/Shell"
import { formatDateTime, healthLevel } from "../metrics"
import type { RepoRef, SearchRoute } from "../route"

export interface SearchApi extends SuggestApi {
  searchRepos(text: string, org?: string, page?: number): Promise<SearchReposResponse>
  searchOrgs(text: string, page?: number): Promise<SearchOrgsResponse>
}

interface Loaded<T> {
  data?: T
  error?: string
  isLoading: boolean
}

function useSearchResult<T>(load: (() => Promise<T>) | null, key: string): Loaded<T> {
  const [state, setState] = useState<Loaded<T>>({ isLoading: load !== null })

  useEffect(() => {
    if (!load) {
      setState({ isLoading: false })
      return
    }
    let current = true
    setState((previous) => ({ data: previous.data, isLoading: true }))
    load()
      .then((data) => current && setState({ data, isLoading: false }))
      .catch(
        (reason: unknown) =>
          current &&
          setState({ isLoading: false, error: reason instanceof Error ? reason.message : "Не удалось выполнить поиск" }),
      )
    return () => {
      current = false
    }
    // key captures everything load depends on; load itself is a new closure on every render.
  }, [key])

  return state
}

function Pager({ page, total, onPage, label }: { page: number; total: number; onPage: (page: number) => void; label: string }) {
  const pageCount = Math.max(1, Math.ceil(total / SEARCH_PAGE_SIZE))
  if (pageCount <= 1) return null
  return (
    <nav className="ranking-pagination" aria-label={label}>
      <Button variant="secondary" disabled={page <= 1} onClick={() => onPage(page - 1)}>
        ← Назад
      </Button>
      <span>
        Страница {page} из {pageCount}
      </span>
      <Button variant="secondary" disabled={page >= pageCount} onClick={() => onPage(page + 1)}>
        Вперёд →
      </Button>
    </nav>
  )
}

function RepoList({ repos, onOpen }: { repos: CatalogRepoResponse[]; onOpen: (ref: RepoRef) => void }) {
  return (
    <ul className="search-results">
      {repos.map((repo) => {
        const level = healthLevel(repo.public_score)
        return (
          <li key={repo.sourcecraft_uuid} className="search-result">
            <button
              type="button"
              className="search-result__open"
              onClick={() => onOpen({ org: repo.org_slug, repo: repo.repo_slug })}
              aria-label={`Открыть анализ ${repo.org_slug}/${repo.repo_slug}`}
            />
            <span className="search-result__main">
              <strong>{repo.repo_slug}</strong>
              <small>{repo.org_slug}</small>
              {repo.description && <span className="search-result__description">{repo.description}</span>}
            </span>
            <span className="search-result__meta">
              {repo.language.name && <LanguageLabel name={repo.language.name} color={repo.language.color} />}
              {repo.last_activity_at && (
                <small title={formatDateTime(repo.last_activity_at)}>
                  Активность: {new Date(repo.last_activity_at).toLocaleDateString("ru-RU")}
                </small>
              )}
            </span>
            <span className="search-result__score">
              {repo.public_score === null ? (
                <Badge tone="neutral">Нет оценки</Badge>
              ) : (
                <>
                  <strong>{repo.public_score}</strong>
                  <Badge tone={level.tone}>{level.label}</Badge>
                </>
              )}
            </span>
          </li>
        )
      })}
    </ul>
  )
}

function OrgList({ orgs, onOpen }: { orgs: CatalogOrgResponse[]; onOpen: (slug: string) => void }) {
  return (
    <ul className="search-results">
      {orgs.map((org) => (
        <li key={org.slug} className="search-result">
          <button
            type="button"
            className="search-result__open"
            onClick={() => onOpen(org.slug)}
            aria-label={`Репозитории организации ${org.slug}`}
          />
          <span className="search-result__main">
            <strong>{org.slug}</strong>
            <small>Организация</small>
          </span>
          <span className="search-result__score">
            <small>{repoCountLabel(org.repo_count)}</small>
          </span>
        </li>
      ))}
    </ul>
  )
}

function Section({
  title,
  total,
  state,
  children,
  empty,
}: {
  title: string
  total?: number
  state: Loaded<unknown>
  children: ReactNode
  empty: string
}) {
  return (
    <Card className="search-section">
      <header className="search-section__header">
        <h2>{title}</h2>
        {total !== undefined && <span className="ranking-count">{total}</span>}
      </header>
      {state.error ? (
        <div className="empty-state" role="alert">
          <strong>Не удалось выполнить поиск</strong>
          <p>{state.error}</p>
        </div>
      ) : state.isLoading && !state.data ? (
        <div className="empty-state" role="status">
          Ищем…
        </div>
      ) : total === 0 ? (
        <div className="empty-state">
          <p>{empty}</p>
        </div>
      ) : (
        children
      )}
    </Card>
  )
}

/** Search results over the public repo catalog: repositories and organizations, or one organization's repositories. */
export default function SearchPage({
  api,
  route,
  onNavigate,
  onOpenRepository,
}: {
  api: SearchApi
  route: SearchRoute
  onNavigate: (route: SearchRoute) => void
  onOpenRepository: (ref: RepoRef) => void
}) {
  const text = route.q.trim()
  const reposPage = route.reposPage ?? 1
  const orgsPage = route.orgsPage ?? 1
  const hasQuery = text !== "" || Boolean(route.org)

  const repos = useSearchResult(
    hasQuery ? () => api.searchRepos(text, route.org ?? "", reposPage) : null,
    `${text}\n${route.org ?? ""}\n${reposPage}`,
  )
  const orgs = useSearchResult(
    text && !route.org ? () => api.searchOrgs(text, orgsPage) : null,
    route.org ? "" : `${text}\n${orgsPage}`,
  )

  const openOrganization = (slug: string) => onNavigate({ page: "search", q: "", org: slug })
  const search = (next: string) => onNavigate({ page: "search", q: next })

  return (
    <div className="page-stack">
      <PageHeading
        eyebrow="Публичные репозитории"
        title={route.org ? `Организация ${route.org}` : "Поиск"}
        description={
          route.org
            ? "Публичные репозитории организации из каталога SourceCraft. Выберите репозиторий, чтобы открыть его оценку."
            : "Поиск по названиям всех публичных репозиториев и организаций SourceCraft, в том числе ещё не оценённых."
        }
      />

      <CatalogSearch
        api={api}
        initialText={route.q}
        autoFocus={!hasQuery}
        onSearch={search}
        onOpenRepository={onOpenRepository}
        onOpenOrganization={openOrganization}
      />

      {!hasQuery ? (
        <Card className="search-section">
          <div className="empty-state">
            <p>Введите название репозитория или организации.</p>
          </div>
        </Card>
      ) : (
        <>
          {route.org && text === "" && (
            <p className="search-back">
              <Button variant="quiet" onClick={() => window.history.back()}>
                ← Назад
              </Button>
            </p>
          )}
          <Section
            title={route.org ? "Репозитории организации" : "Репозитории"}
            total={repos.data?.total_count}
            state={repos}
            empty={route.org ? "У организации нет публичных репозиториев в каталоге." : "Репозитории не найдены."}
          >
            <RepoList repos={repos.data?.repos ?? []} onOpen={onOpenRepository} />
            <Pager
              label="Страницы репозиториев"
              page={reposPage}
              total={repos.data?.total_count ?? 0}
              onPage={(page) => onNavigate({ ...route, reposPage: page })}
            />
          </Section>
          {!route.org && (
            <Section title="Организации" total={orgs.data?.total_count} state={orgs} empty="Организации не найдены.">
              <OrgList orgs={orgs.data?.orgs ?? []} onOpen={openOrganization} />
              <Pager
                label="Страницы организаций"
                page={orgsPage}
                total={orgs.data?.total_count ?? 0}
                onPage={(page) => onNavigate({ ...route, orgsPage: page })}
              />
            </Section>
          )}
        </>
      )}
    </div>
  )
}
