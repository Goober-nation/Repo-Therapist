import type { ReactNode } from "react"
import type { PageId } from "../types"

const pageLinks: { id: Exclude<PageId, "overview">; label: string; icon: ReactNode }[] = [
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
    id: "analytics",
    label: "Аналитика",
    icon: (
      <>
        <path d="M3 15V9M7 15V4M11 15V7M15 15V11" />
        <circle cx="15" cy="4" r="1.5" />
      </>
    ),
  },
  {
    id: "my",
    label: "Мои репозитории",
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
    id: "access",
    label: "Доступ админа",
    icon: (
      <>
        <rect x="3.5" y="8" width="11" height="7" rx="1.5" />
        <path d="M6 8V6a3 3 0 0 1 6 0v2" />
      </>
    ),
  },
  {
    id: "rules",
    label: "Правила оценки",
    icon: (
      <>
        <path d="M4 2h7l3 3v11H4z" />
        <path d="M7 8h5M7 11h5M7 14h3" />
      </>
    ),
  },
]

export default function Sidebar({
  activePage,
  onPageChange,
}: {
  activePage: PageId
  onPageChange: (page: Exclude<PageId, "overview">) => void
}) {
  return (
    <aside className="sidebar" id="app-sidebar">
      <div className="sidebar__caption">Рабочая область</div>
      <nav className="sidebar__nav" aria-label="Основная навигация">
        {pageLinks.map((pageLink) => (
          <button
            key={pageLink.id}
            className={`nav-link ${activePage === pageLink.id ? "nav-link--active" : ""}`}
            type="button"
            aria-current={activePage === pageLink.id ? "page" : undefined}
            onClick={() => onPageChange(pageLink.id)}
          >
            <svg viewBox="0 0 18 18" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden="true">
              {pageLink.icon}
            </svg>
            {pageLink.label}
          </button>
        ))}
      </nav>
    </aside>
  )
}
