import { useState } from "react"
import type { YandexProfile } from "../api/yandexAuth"
import type { PageId } from "../types"
import YandexSignIn from "./YandexSignIn"

const pageTitles: Record<PageId, string> = {
  overview: "Анализ репозитория",
  ranking: "Рейтинг SourceCraft",
  my: "Мои репозитории",
}

export default function TopBar({
  activePage,
  repositoryName,
  theme,
  onToggleTheme,
  onPageChange,
  isSidebarOpen,
  onToggleSidebar,
  onOpenRepositoryLink,
  yandexProfile,
  onYandexSignIn,
}: {
  activePage: PageId
  repositoryName: string
  theme: "light" | "dark" | "dopamine"
  onToggleTheme: () => void
  onPageChange: (page: Exclude<PageId, "overview">) => void
  isSidebarOpen: boolean
  onToggleSidebar: () => void
  onOpenRepositoryLink: (link: string) => boolean
  yandexProfile: YandexProfile | null
  onYandexSignIn: (profile: YandexProfile) => void
}) {
  const [repositoryLink, setRepositoryLink] = useState("")
  const [isLinkInvalid, setIsLinkInvalid] = useState(false)

  const nextThemeLabel =
    theme === "light"
      ? "Тёмная тема"
      : theme === "dark"
        ? "Дофаминовая тема"
        : "Светлая тема"

  return (
    <header className="topbar">
      <div className="topbar__start">
        <button
          className="menu-toggle"
          type="button"
          onClick={onToggleSidebar}
          aria-expanded={isSidebarOpen}
          aria-controls="app-sidebar"
          aria-label={
            isSidebarOpen
              ? "Скрыть рабочую область"
              : "Показать рабочую область"
          }
        >
          <svg viewBox="0 0 20 20" aria-hidden="true">
            <path d="M3 5h14M3 10h14M3 15h14" />
          </svg>
        </button>
        <button
          className="brand"
          type="button"
          onClick={() => onPageChange("ranking")}
        >
          <span className="brand__mark" aria-hidden="true">
            <svg
              viewBox="0 0 776 960"
              fill="currentColor"
              stroke="currentColor"
              strokeLinecap="round"
              strokeLinejoin="round"
              focusable="false"
            >
              <defs>
                <linearGradient
                  id="repo-health-logo-gradient"
                  gradientUnits="userSpaceOnUse"
                  x1="126"
                  y1="135"
                  x2="640"
                  y2="868"
                >
                  <stop stopColor="#e73583" />
                  <stop offset=".5" stopColor="#8b3fe8" />
                  <stop offset="1" stopColor="#ed791c" />
                </linearGradient>
              </defs>
              <g fill="none">
                <path
                  d="M523 732H640V135H230Q126 135 126 239V600Q126 680 168 715M126 620Q126 564 230 564H640"
                  strokeWidth="48"
                />
              </g>
              <path d="M383 468C255 385 238 262 316 250C352 245 374 266 383 290C392 266 414 245 450 250C528 262 511 385 383 468Z" strokeWidth="12" />
              <path
                d="M253 674H421Q429 674 429 682V857Q429 868 418 861L343 810Q336 805 329 810L256 861Q245 868 245 857V682Q245 674 253 674Z"
                stroke="none"
              />
            </svg>
          </span>
          <span>
            <strong>Repo Therapist</strong>
            <small>SourceCraft</small>
          </span>
        </button>
      </div>
      <div className="breadcrumbs" aria-label="Текущая страница">
        <span>SourceCraft</span>
        <span className="breadcrumbs__separator">/</span>
        <strong>
          {activePage === "overview" ? repositoryName : pageTitles[activePage]}
        </strong>
      </div>
      <div className="topbar__actions">
        <form
          className="topbar-repository-search"
          onSubmit={(event) => {
            event.preventDefault()
            const opened = onOpenRepositoryLink(repositoryLink)
            setIsLinkInvalid(!opened)
            if (opened) setRepositoryLink("")
          }}
        >
          <input
            aria-label="Ссылка на открытый репозиторий SourceCraft"
            placeholder="https://sourcecraft.dev/org/repo"
            inputMode="url"
            value={repositoryLink}
            aria-invalid={isLinkInvalid}
            aria-describedby={isLinkInvalid ? "repository-link-error" : undefined}
            onChange={(event) => {
              setRepositoryLink(event.target.value)
              setIsLinkInvalid(false)
            }}
          />
          <button type="submit" className="topbar-repository-search__submit">
            Открыть
          </button>
          {isLinkInvalid && (
            <small id="repository-link-error" className="topbar-repository-search__error" role="alert">
              Нужна ссылка вида https://sourcecraft.dev/организация/репозиторий
            </small>
          )}
        </form>
        <button
          className="theme-toggle"
          type="button"
          onClick={onToggleTheme}
          aria-label={`Включить: ${nextThemeLabel.toLocaleLowerCase()}`}
          title={nextThemeLabel}
        >
          {theme === "light" ? (
            <svg viewBox="0 0 20 20" aria-hidden="true">
              <path d="M16.8 12.8A6.8 6.8 0 0 1 7.2 3.2 7 7 0 1 0 16.8 12.8Z" />
            </svg>
          ) : theme === "dark" ? (
            <svg viewBox="0 0 20 20" aria-hidden="true">
              <path d="m10 2 1.1 4.2L15 4.8l-1.4 3.9L18 10l-4.4 1.3 1.4 3.9-3.9-1.4L10 18l-1.1-4.2L5 15.2l1.4-3.9L2 10l4.4-1.3L5 4.8l3.9 1.4L10 2Z" />
            </svg>
          ) : (
            <svg viewBox="0 0 20 20" aria-hidden="true">
              <circle cx="10" cy="10" r="4" />
              <path d="M10 2v2M10 16v2M2 10h2M16 10h2M4.3 4.3l1.4 1.4M14.3 14.3l1.4 1.4M15.7 4.3l-1.4 1.4M5.7 14.3l-1.4 1.4" />
            </svg>
          )}
          <span>
            {theme === "light"
              ? "Тёмная"
              : theme === "dark"
                ? "Дофамин"
                : "Светлая"}
          </span>
        </button>
        {yandexProfile ? (
          <button
            className="topbar__account"
            type="button"
            title="Мои репозитории"
            onClick={() => onPageChange("my")}
          >
            <span className="user-avatar" aria-hidden="true">
              {yandexProfile.name.trim().charAt(0).toLocaleUpperCase()}
            </span>
            <span className="topbar__user">
              <strong>{yandexProfile.name}</strong>
              <small>Мои репозитории</small>
            </span>
          </button>
        ) : (
          <YandexSignIn
            theme={theme === "dark" ? "dark" : "light"}
            onSignIn={onYandexSignIn}
          />
        )}
      </div>
    </header>
  )
}
