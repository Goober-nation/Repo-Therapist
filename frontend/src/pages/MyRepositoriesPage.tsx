import { useState, type FormEvent } from "react"
import { Badge, Button, Card, LanguageLabel, PageHeading } from "../components/Shell"
import type { MyOrganization } from "../api/contracts"
import { organizationLabel } from "../api/mapper"
import { getLastActivityLabel } from "../metrics"
import type { RepositorySummary } from "../types"

const VISIBILITY_LABELS = { public: "Открытый", internal: "Внутренний", private: "Закрытый" } as const

export default function MyRepositoriesPage({
  isSignedIn,
  hasToken,
  repositories,
  isLoading,
  error,
  organizations = [],
  selectedOrganization,
  hasMore = false,
  isLoadingMore = false,
  onSelectOrganization,
  onLoadMore,
  onSubmitToken,
  onForgetToken,
  onOpenRepository,
}: {
  isSignedIn: boolean
  hasToken: boolean
  repositories: RepositorySummary[]
  isLoading: boolean
  error?: string
  organizations?: MyOrganization[]
  selectedOrganization?: string
  hasMore?: boolean
  isLoadingMore?: boolean
  onSelectOrganization?: (slug: string) => void
  onLoadMore?: () => void
  onSubmitToken: (token: string) => void
  onForgetToken: () => void
  onOpenRepository: (repository: RepositorySummary) => void
}) {
  const [token, setToken] = useState("")
  const [query, setQuery] = useState("")
  const normalizedQuery = query.trim().toLocaleLowerCase()
  const visibleRepositories = normalizedQuery
    ? repositories.filter((repository) => repository.fullName.toLocaleLowerCase().includes(normalizedQuery))
    : repositories

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!token.trim()) return
    onSubmitToken(token)
    setToken("")
  }

  return (
    <div className="page-stack">
      <PageHeading
        eyebrow="Приватный анализ"
        title="Мои репозитории"
        description="Анализ ваших репозиториев SourceCraft по всем шести категориям, включая безопасность (AppSec) и CI/CD. Эти данные SourceCraft отдаёт только владельцу, поэтому нужен ваш персональный токен доступа."
      />

      {!isSignedIn ? (
        <Card className="empty-state">
          <strong>Войдите через Яндекс ID</strong>
          <p>Кнопка входа находится в правом верхнем углу.</p>
        </Card>
      ) : !hasToken ? (
        <Card>
          <form className="token-form" onSubmit={submit}>
            <label htmlFor="sourcecraft-pat">Персональный токен SourceCraft (PAT)</label>
            <div className="token-form__row">
              <input
                id="sourcecraft-pat"
                type="password"
                autoComplete="off"
                spellCheck={false}
                value={token}
                onChange={(event) => setToken(event.target.value)}
                placeholder="Вставьте токен"
              />
              <Button type="submit" variant="primary" disabled={!token.trim()}>
                Показать репозитории
              </Button>
            </div>
            <p className="muted-copy">
              Токен сохраняется в этом браузере на один час и передаётся Gateway в заголовке запроса. Потом
              его нужно ввести снова; кнопка «Сменить токен» удаляет его сразу.
            </p>
          </form>
        </Card>
      ) : (
        <Card className="ranking-card">
          <div className="ranking-toolbar">
            {organizations.length > 0 && (
              <label className="filter-field filter-field--org">
                <span>Организация</span>
                <select
                  value={selectedOrganization ?? ""}
                  disabled={isLoading}
                  onChange={(event) => onSelectOrganization?.(event.target.value)}
                >
                  {organizations.map((organization) => (
                    <option key={organization.slug} value={organization.slug}>
                      {organizationLabel(organization)}
                    </option>
                  ))}
                </select>
              </label>
            )}
            {repositories.length > 0 && (
              <label className="search-field">
                <span className="visually-hidden">Поиск по моим репозиториям</span>
                <input
                  type="search"
                  placeholder="Найти репозиторий"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                />
              </label>
            )}
            <span className="ranking-count" aria-live="polite">
              {isLoading
                ? "Загружаем репозитории…"
                : `${visibleRepositories.length} из ${repositories.length}${hasMore ? "+" : ""}`}
            </span>
            <Button variant="quiet" onClick={onForgetToken}>
              Сменить токен
            </Button>
          </div>
          {visibleRepositories.length > 0 && (
            <ul className="my-repository-list">
              {visibleRepositories.map((repository) => (
                <li key={repository.id}>
                  <div className="repository-cell">
                    <strong>{repository.fullName}</strong>
                    <small>
                      <LanguageLabel name={repository.language} color={repository.languageColor} /> · активность{" "}
                      {getLastActivityLabel(repository.lastActivityDaysAgo).toLocaleLowerCase()}
                    </small>
                  </div>
                  {repository.visibility && <Badge>{VISIBILITY_LABELS[repository.visibility]}</Badge>}
                  <Button variant="secondary" onClick={() => onOpenRepository(repository)}>
                    Анализировать
                  </Button>
                </li>
              ))}
            </ul>
          )}
          {hasMore && !isLoading && (
            <div className="my-repository-more">
              <Button variant="secondary" disabled={isLoadingMore} onClick={() => onLoadMore?.()}>
                {isLoadingMore ? "Загружаем…" : "Показать ещё"}
              </Button>
            </div>
          )}
          {!isLoading && !error && repositories.length === 0 && (
            <div className="empty-state">
              <strong>Репозиториев не найдено</strong>
              <p>Проверьте, что у токена есть доступ к репозиториям.</p>
            </div>
          )}
        </Card>
      )}
      {error && (
        <div className="inline-notice inline-notice--error" role="alert">
          {error}
        </div>
      )}
    </div>
  )
}
