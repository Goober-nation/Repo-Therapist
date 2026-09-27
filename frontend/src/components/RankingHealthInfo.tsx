import { useEffect, useId, useState } from "react"
import { createPortal } from "react-dom"
import { getLowestCategoryScores } from "../mocks/rankingHealthInfo"
import type { RepositorySummary } from "../types"

interface TooltipPosition {
  left: number
  top: number
  above: boolean
}

export function RankingHealthInfo({
  repository,
}: {
  repository: RepositorySummary
}) {
  const [position, setPosition] = useState<TooltipPosition | null>(null)
  const tooltipId = useId()
  const weakestCategories = getLowestCategoryScores(repository.categoryScores)

  useEffect(() => {
    if (!position) return
    const close = () => setPosition(null)
    window.addEventListener("scroll", close, true)
    window.addEventListener("resize", close)
    return () => {
      window.removeEventListener("scroll", close, true)
      window.removeEventListener("resize", close)
    }
  }, [position])

  const show = (button: HTMLButtonElement) => {
    const bounds = button.getBoundingClientRect()
    const tooltipWidth = Math.min(280, window.innerWidth - 16)
    setPosition({
      left: Math.max(
        8,
        Math.min(
          bounds.right - tooltipWidth,
          window.innerWidth - tooltipWidth - 8,
        ),
      ),
      top:
        bounds.bottom + 8 > window.innerHeight - 170
          ? bounds.top - 8
          : bounds.bottom + 8,
      above: bounds.bottom + 8 > window.innerHeight - 170,
    })
  }

  return (
    <>
      <button
        className="ranking-health-info__trigger"
        type="button"
        aria-label={`Показать низкие оценки категорий для ${repository.fullName}`}
        aria-describedby={position ? tooltipId : undefined}
        onMouseEnter={(event) => show(event.currentTarget)}
        onMouseLeave={(event) => {
          if (document.activeElement !== event.currentTarget) setPosition(null)
        }}
        onFocus={(event) => show(event.currentTarget)}
        onBlur={() => setPosition(null)}
        onClick={(event) => show(event.currentTarget)}
        onKeyDown={(event) => {
          if (event.key === "Escape") setPosition(null)
        }}
      >
        i
      </button>
      {position &&
        createPortal(
          <div
            id={tooltipId}
            className="ranking-health-info__tooltip"
            role="tooltip"
            style={{
              left: position.left,
              top: position.top,
              transform: position.above ? "translateY(-100%)" : undefined,
            }}
          >
            <strong>Самые низкие оценки</strong>
            {weakestCategories.length > 0 ? (
              <ul>
                {weakestCategories.map((category) => (
                  <li key={category.name}>
                    <span>{category.name}</span>
                    <b>{category.score}/100</b>
                  </li>
                ))}
              </ul>
            ) : (
              <p>Нет данных об оценках категорий.</p>
            )}
            <small>Это слабые категории, а не расчёт их вклада в Health.</small>
          </div>,
          document.body,
        )}
    </>
  )
}
