import type { ReactNode } from "react"
import type { PageId } from "../types"

const pageLinks: { id: PageId; label: string; icon: ReactNode }[] = [
  {
    id: "overview",
    label: "Обзор репозитория",
    icon: (
      <>
        <rect x="3" y="3" width="5" height="5" rx="1" />
        <rect x="10" y="3" width="3" height="3" rx="1" />
        <rect x="3" y="10" width="3" height="3" rx="1" />
        <rect x="8" y="8" width="5" height="5" rx="1" />
      </>
    ),
  },
  {
    id: "ranking",
    label: "Рейтинг",
    icon: (
      <>
        <path d="M3 12V8M7 12V4M11 12V6M15 12V2" />
        <path d="M2 14h14" />
      </>
    ),
  },
  {
    id: "security",
    label: "Безопасность",
    icon: (
      <>
        <path d="M9 2 3.5 4.5v4c0 3.2 2.3 5.4 5.5 6.7 3.2-1.3 5.5-3.5 5.5-6.7v-4L9 2Z" />
        <path d="m6.5 8.5 1.7 1.7 3.3-3.5" />
      </>
    ),
  },
]

interface SidebarProps {
  activePage: PageId
  onPageChange: (page: PageId) => void
  isAuthenticated: boolean
  onSignIn: () => void
  onSignOut: () => void
}

export default function Sidebar({
  activePage,
  onPageChange,
  isAuthenticated,
  onSignIn,
  onSignOut,
}: SidebarProps) {
  return (
    <aside className="sidebar">
      <button
        className="brand"
        type="button"
        onClick={() => onPageChange("overview")}
      >
        <span className="brand__mark" aria-hidden="true">
          <svg
            viewBox="0 0 64 44"
            fill="none"
            stroke="currentColor"
            strokeLinecap="round"
            strokeLinejoin="round"
            focusable="false"
          >
            <defs>
              <linearGradient
                id="repo-health-logo-gradient"
                gradientUnits="userSpaceOnUse"
                x1="5"
                y1="6"
                x2="59"
                y2="38"
              >
                <stop stopColor="#e73583" />
                <stop offset=".5" stopColor="#8b3fe8" />
                <stop offset="1" stopColor="#ed791c" />
              </linearGradient>
            </defs>
            <path d="m21 6-16 16 16 16M43 6l16 16-16 16" strokeWidth="6" />
            <path d="M19 22h8l5-12 7 24 5-12h5" strokeWidth="4.8" />
          </svg>
        </span>
        <span>
          <strong>Repo Health</strong>
          <small>SourceCraft</small>
        </span>
      </button>

      <div className="sidebar__caption">Рабочая область</div>
      <nav className="sidebar__nav" aria-label="Основная навигация">
        {pageLinks.map((pageLink) => (
          <button
            key={pageLink.id}
            className={`nav-link ${
              activePage === pageLink.id ? "nav-link--active" : ""
            }`}
            type="button"
            aria-current={activePage === pageLink.id ? "page" : undefined}
            onClick={() => onPageChange(pageLink.id)}
          >
            <svg
              viewBox="0 0 18 18"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.4"
              aria-hidden="true"
            >
              {pageLink.icon}
            </svg>
            {pageLink.label}
          </button>
        ))}
      </nav>

      <div className="sidebar__footer">
        {isAuthenticated ? (
          <>
            <span className="user-avatar" aria-hidden="true">
              М
            </span>
            <span className="sidebar__user">
              <strong>Мария</strong>
              <small>Яндекс ID</small>
            </span>
            <button className="auth-action" type="button" onClick={onSignOut}>
              Выйти
            </button>
          </>
        ) : (
          <button className="auth-button" type="button" onClick={onSignIn}>
            <span className="auth-button__mark" aria-hidden="true">
              Я
            </span>
            <span>
              <strong>Войти через Яндекс ID</strong>
              <small>Аккаунт SourceCraft</small>
            </span>
          </button>
        )}
      </div>
    </aside>
  )
}
