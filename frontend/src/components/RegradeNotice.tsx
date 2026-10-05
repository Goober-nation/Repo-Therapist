import { useEffect, useState } from "react"
import { formatCountdown } from "../metrics"
import type { RegradeStatus } from "../types"

/** Re-renders every `intervalMs` so a countdown keeps moving; `now` is injectable for tests. */
function useNow(intervalMs: number): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), intervalMs)
    return () => clearInterval(timer)
  }, [intervalMs])
  return now
}

/** One line under the ranking heading: time to the next scheduled regrade, its progress, or that it is off. */
export default function RegradeNotice({ status, now: fixedNow }: { status?: RegradeStatus; now?: number }) {
  const ticking = useNow(30_000)
  if (!status) return null
  const now = fixedNow ?? ticking

  if (status.running) {
    return (
      <p className="regrade-note regrade-note--running" role="status">
        <span className="regrade-note__dot" aria-hidden="true" />
        Идёт плановая переоценка публичных репозиториев
        {status.total > 0 ? `: ${Math.min(status.done, status.total)} из ${status.total}` : "…"}
      </p>
    )
  }

  if (!status.enabled) {
    return (
      <p className="regrade-note regrade-note--off">
        <span className="regrade-note__dot" aria-hidden="true" />
        Плановая переоценка выключена: оценки обновляются, когда открывают репозиторий.
      </p>
    )
  }

  const left = status.nextRunAt ? formatCountdown(Date.parse(status.nextRunAt) - now) : null
  return (
    <p className="regrade-note" title={status.nextRunAt ? `Следующая переоценка: ${new Date(status.nextRunAt).toLocaleString("ru-RU")}` : undefined}>
      <span className="regrade-note__dot" aria-hidden="true" />
      {left ? `До плановой переоценки публичных репозиториев: ${left}` : "Плановая переоценка публичных репозиториев вот-вот начнётся"}
    </p>
  )
}
