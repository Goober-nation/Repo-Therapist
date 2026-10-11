import { Fragment, useEffect, useId, useRef, useState } from "react"
import type {
  CatalogOrgResponse,
  CatalogRepoResponse,
  SearchSuggestResponse,
} from "../api/contracts"
import { directRepository, type RepoRef } from "../route"
import { LanguageLabel } from "./Shell"

/** Suggestions are fetched this long after the user stops typing. */
export const SUGGEST_DELAY_MS = 1000

export interface SuggestApi {
  suggest(text: string): Promise<SearchSuggestResponse>
}

type Option =
  | { kind: "repo"; repo: CatalogRepoResponse }
  | { kind: "org"; org: CatalogOrgResponse }

/** The suggestions in display order: the repositories, then the organization. */
export function suggestionOptions(
  suggestions: SearchSuggestResponse | null,
): Option[] {
  if (!suggestions) return []
  return [
    ...suggestions.repos
      .slice(0, 3)
      .map((repo): Option => ({ kind: "repo", repo })),
    ...suggestions.orgs
      .slice(0, 1)
      .map((org): Option => ({ kind: "org", org })),
  ]
}

/**
 * The one search field over the public repo catalog. Typing suggests the top 3 repositories and the
 * top organization; Enter or the button opens the search page, or a SourceCraft link / exact
 * "org/repo" directly.
 */
export default function CatalogSearch({
  api,
  initialText = "",
  autoFocus = false,
  onSearch,
  onOpenRepository,
  onOpenOrganization,
}: {
  api: SuggestApi
  initialText?: string
  autoFocus?: boolean
  onSearch: (text: string) => void
  onOpenRepository: (ref: RepoRef) => void
  onOpenOrganization: (slug: string) => void
}) {
  const [text, setText] = useState(initialText)
  const [suggestions, setSuggestions] = useState<SearchSuggestResponse | null>(
    null,
  )
  const [isLoading, setIsLoading] = useState(false)
  const [isOpen, setIsOpen] = useState(false)
  const [active, setActive] = useState(-1)
  const [error, setError] = useState("")
  const rootRef = useRef<HTMLFormElement>(null)
  // Only the answer to the latest request is shown; an older one arriving late is dropped.
  const requestRef = useRef(0)
  const listId = useId()

  useEffect(() => setText(initialText), [initialText])

  useEffect(() => {
    const query = text.trim()
    const request = ++requestRef.current
    setActive(-1)
    if (!query || directRepository(query)) {
      setSuggestions(null)
      setIsLoading(false)
      return
    }
    setIsLoading(true)
    const timer = setTimeout(() => {
      api
        .suggest(query)
        .then((result) => {
          if (request !== requestRef.current) return
          setSuggestions(result)
          setError("")
        })
        .catch((reason: unknown) => {
          if (request !== requestRef.current) return
          setSuggestions(null)
          setError(
            reason instanceof Error
              ? reason.message
              : "Не удалось получить подсказки",
          )
        })
        .finally(() => {
          if (request === requestRef.current) setIsLoading(false)
        })
    }, SUGGEST_DELAY_MS)
    return () => clearTimeout(timer)
  }, [api, text])

  useEffect(() => {
    const close = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setIsOpen(false)
    }
    document.addEventListener("pointerdown", close)
    return () => document.removeEventListener("pointerdown", close)
  }, [])

  const options = suggestionOptions(suggestions)
  const query = text.trim()
  const showPanel = isOpen && query !== "" && !directRepository(query)

  const pick = (option: Option) => {
    setIsOpen(false)
    if (option.kind === "repo")
      onOpenRepository({
        org: option.repo.org_slug,
        repo: option.repo.repo_slug,
      })
    else onOpenOrganization(option.org.slug)
  }

  const submit = () => {
    if (active >= 0 && options[active]) {
      pick(options[active])
      return
    }
    if (!query) return
    setIsOpen(false)
    const direct = directRepository(query)
    if (direct) onOpenRepository(direct)
    else onSearch(query)
  }

  return (
    <form
      ref={rootRef}
      className="catalog-search"
      role="search"
      onSubmit={(event) => {
        event.preventDefault()
        submit()
      }}
    >
      <div className="catalog-search__bar">
        <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
          <circle cx="7" cy="7" r="4.5" />
          <path d="m10.5 10.5 3 3" />
        </svg>
        <input
          type="search"
          role="combobox"
          aria-label="Поиск репозиториев и организаций SourceCraft"
          aria-expanded={showPanel}
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={
            active >= 0 ? `${listId}-${active}` : undefined
          }
          placeholder="Репозиторий, организация или ссылка sourcecraft.dev"
          autoComplete="off"
          spellCheck={false}
          maxLength={200}
          autoFocus={autoFocus}
          value={text}
          onChange={(event) => {
            setText(event.target.value)
            setIsOpen(true)
          }}
          onFocus={() => setIsOpen(true)}
          onKeyDown={(event) => {
            if (event.key === "ArrowDown" && options.length) {
              event.preventDefault()
              setIsOpen(true)
              setActive((index) => (index + 1) % options.length)
            } else if (event.key === "ArrowUp" && options.length) {
              event.preventDefault()
              setActive((index) =>
                index <= 0 ? options.length - 1 : index - 1,
              )
            } else if (event.key === "Escape") {
              setIsOpen(false)
              setActive(-1)
            }
          }}
        />
        <button type="submit" className="catalog-search__submit">
          Найти
        </button>
      </div>
      {showPanel && (
        <div
          className="catalog-search__panel"
          id={listId}
          role="listbox"
          aria-label="Подсказки"
        >
          {options.map((option, index) => (
            <Fragment
              key={
                option.kind === "repo"
                  ? option.repo.sourcecraft_uuid
                  : `org:${option.org.slug}`
              }
            >
              {(index === 0 || options[index - 1].kind !== option.kind) && (
                <div className="catalog-search__group" role="presentation">
                  {option.kind === "repo" ? "Репозитории" : "Организация"}
                </div>
              )}
              <div
                id={`${listId}-${index}`}
                role="option"
                aria-selected={index === active}
                className={`catalog-search__option catalog-search__option--${option.kind} ${
                  index === active ? "catalog-search__option--active" : ""
                }`}
                onPointerDown={(event) => event.preventDefault()}
                onClick={() => pick(option)}
                onMouseEnter={() => setActive(index)}
              >
                {option.kind === "repo" ? (
                  <>
                    <span className="catalog-search__name">
                      <strong>{option.repo.repo_slug}</strong>
                      <small>{option.repo.org_slug}</small>
                    </span>
                    {option.repo.language.name && (
                      <LanguageLabel
                        name={option.repo.language.name}
                        color={option.repo.language.color}
                      />
                    )}
                    <span className="catalog-search__score">
                      {option.repo.public_score ?? "Нет оценки"}
                    </span>
                  </>
                ) : (
                  <>
                    <span className="catalog-search__name">
                      <strong>{option.org.slug}</strong>
                      <small>Организация</small>
                    </span>
                    <span className="catalog-search__score">
                      {repoCountLabel(option.org.repo_count)}
                    </span>
                  </>
                )}
              </div>
            </Fragment>
          ))}
          <div className="catalog-search__status" role="status">
            {isLoading
              ? "Ищем…"
              : error
                ? error
                : suggestions && options.length === 0
                  ? "Ничего не найдено"
                  : `Enter — все результаты по «${query}»`}
          </div>
        </div>
      )}
    </form>
  )
}

/** "1 репозиторий", "3 репозитория", "5 репозиториев". */
export function repoCountLabel(count: number): string {
  const tens = count % 100
  const ones = count % 10
  const word =
    tens >= 11 && tens <= 14
      ? "репозиториев"
      : ones === 1
        ? "репозиторий"
        : ones >= 2 && ones <= 4
          ? "репозитория"
          : "репозиториев"
  return `${count} ${word}`
}
