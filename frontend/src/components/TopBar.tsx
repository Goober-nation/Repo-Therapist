import type { PageId } from "../types"

const pageTitles: Record<PageId, string> = {
  overview: "Анализ репозитория",
  ranking: "Рейтинг SourceCraft",
  security: "Безопасность репозитория",
}

export default function TopBar({
  activePage,
  repositoryName,
  theme,
  onToggleTheme,
}: {
  activePage: PageId
  repositoryName: string
  theme: "light" | "dark" | "dopamine"
  onToggleTheme: () => void
}) {
  const nextThemeLabel =
    theme === "light"
      ? "Тёмная тема"
      : theme === "dark"
        ? "Дофаминовая тема"
        : "Светлая тема"

  return (
    <header className="topbar">
      <div className="breadcrumbs" aria-label="Текущая страница">
        <span>SourceCraft</span>
        <span className="breadcrumbs__separator">/</span>
        <strong>
          {activePage === "ranking" ? pageTitles[activePage] : repositoryName}
        </strong>
        {activePage === "security" && (
          <>
            <span className="breadcrumbs__separator">/</span>
            <strong>{pageTitles[activePage]}</strong>
          </>
        )}
      </div>
      <div className="topbar__actions">
        <span className="topbar__mode">SourceCraft</span>
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
          <span>{theme === "light" ? "Тёмная" : theme === "dark" ? "Дофамин" : "Светлая"}</span>
        </button>
      </div>
    </header>
  )
}
