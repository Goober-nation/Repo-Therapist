import { Button } from "./Shell"

/** Previous / next controls of the audit log; the log is paged by cursor, so the total is not known. */
export default function AuditPager({
  page,
  hasPrevious,
  hasNext,
  isLoading,
  onPrevious,
  onNext,
}: {
  page: number
  hasPrevious: boolean
  hasNext: boolean
  isLoading: boolean
  onPrevious: () => void
  onNext: () => void
}) {
  if (!hasPrevious && !hasNext) return null

  return (
    <nav className="audit-pager" aria-label="Страницы журнала">
      <Button onClick={onPrevious} disabled={!hasPrevious || isLoading}>
        Назад
      </Button>
      <span className="muted" aria-current="page">
        Страница {page}
      </span>
      <Button onClick={onNext} disabled={!hasNext || isLoading}>
        Дальше
      </Button>
    </nav>
  )
}
