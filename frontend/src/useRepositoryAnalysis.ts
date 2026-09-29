import { useCallback, useEffect, useRef, useState } from "react"
import { ApiRequestError, type HttpRepoHealthApi } from "./api/client"
import type { GradingResponse } from "./api/contracts"
import { aiReportFromResponse } from "./api/mapper"
import type { AiReport } from "./types"

/** Gateway allows RATE_LIMIT (60 in docker-compose) requests per client per minute. */
const POLL_INTERVAL_MS = 10_000
const RATE_LIMITED_WAIT_MS = 30_000
/** Covers the backend defaults CLONE_TIMEOUT=10m + GRADE_TIMEOUT=4m; raise with them. */
const POLL_TIMEOUT_MS = 15 * 60_000

export interface RepositoryAnalysis {
  isRunning: boolean
  /** undefined until the user requests the AI summary. */
  ai?: AiReport
  aiLoading: boolean
  aiError?: string
  error?: string
  /** Result of a run the user started with the button. */
  rerun?: { gradeChanged: boolean; stillPending: boolean }
  run: () => void
  loadAiSummary: () => void
}

interface State {
  isRunning: boolean
  ai?: AiReport
  aiLoading: boolean
  aiError?: string
  error?: string
  rerun?: RepositoryAnalysis["rerun"]
}

const NO_AI: AiReport = { strengths: [], weaknesses: [], recommendations: [] }

export async function pollUntilReady<T extends { status: string }>(
  load: () => Promise<T>,
  isActive: () => boolean,
): Promise<T> {
  const deadline = Date.now() + POLL_TIMEOUT_MS
  const wait = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))
  // A 429 means "slow down", not "failed": wait and ask again until the deadline.
  const attempt = async (): Promise<T> => {
    for (;;) {
      try {
        return await load()
      } catch (error) {
        const limited = error instanceof ApiRequestError && error.status === 429
        if (!limited || Date.now() + RATE_LIMITED_WAIT_MS > deadline || !isActive()) throw error
        await wait(RATE_LIMITED_WAIT_MS)
      }
    }
  }
  let response = await attempt()
  while (response.status === "PENDING" && Date.now() < deadline && isActive()) {
    await wait(POLL_INTERVAL_MS)
    if (isActive()) response = await attempt()
  }
  return response
}

function errorText(error: unknown, fallback: string) {
  return error instanceof ApiRequestError ? error.message : fallback
}

/**
 * Loads the grade (Gateway starts grading on demand and answers PENDING until it is done).
 * The AI summary is requested separately when the user opens its section. `run()` repeats
 * grading; the backend regrades when the repository has activity newer than the last grade,
 * otherwise it returns the current one.
 */
export function useRepositoryAnalysis(
  api: HttpRepoHealthApi,
  target: { org: string; repo: string; pat?: string } | undefined,
  lastGradedAt: string | undefined,
  onGrading: (grading: GradingResponse) => void,
): RepositoryAnalysis {
  const [state, setState] = useState<State>({ isRunning: false, aiLoading: false })
  // Reruns count per repository, so a rerun of one page is never reported on another.
  const [reruns, setReruns] = useState<{ key: string; count: number }>({ key: "", count: 0 })
  const onGradingRef = useRef(onGrading)
  const gradedAtRef = useRef(lastGradedAt)
  const aiRequestRef = useRef<{
    key: string
    status: "loading" | "ready" | "error"
    active: boolean
  } | null>(null)
  useEffect(() => {
    onGradingRef.current = onGrading
    gradedAtRef.current = lastGradedAt
  })

  const org = target?.org
  const repo = target?.repo
  const pat = target?.pat
  const key = `${org}/${repo}/${pat ? "private" : "public"}`
  const attempt = reruns.key === key ? reruns.count : 0
  // Keep results from one token/session or explicit rerun from being reused for another.
  const aiRequestKey = `${key}/${attempt}/${pat ?? ""}`

  useEffect(() => {
    if (!org || !repo) return
    let active = true
    const isActive = () => active
    const gradedBefore = gradedAtRef.current
    setState({ isRunning: true, aiLoading: false })

    void (async () => {
      let grading: GradingResponse
      try {
        grading = await pollUntilReady(() => api.getGrading(org, repo, pat), isActive)
      } catch (error) {
        if (active) {
          setState({
            isRunning: false,
            aiLoading: false,
            error: errorText(error, "Не удалось получить оценку репозитория."),
          })
        }
        return
      }
      if (!active) return
      onGradingRef.current(grading)
      const stillPending = grading.status === "PENDING"
      const gradedAt = grading.grade?.graded_at ?? grading.graded_at
      const rerun = attempt > 0 ? { gradeChanged: gradedAt !== gradedBefore, stillPending } : undefined
      if (stillPending) {
        setState((current) => ({
          ...current,
          isRunning: false,
          ai: current.aiLoading ? current.ai : { ...NO_AI, status: "PENDING" },
          rerun,
        }))
        return
      }
      setState((current) => ({ ...current, isRunning: false, error: undefined, rerun }))
    })()

    return () => {
      active = false
      const request = aiRequestRef.current
      if (request?.key === aiRequestKey) request.active = false
    }
  }, [api, org, repo, pat, attempt, aiRequestKey])

  const loadAiSummary = useCallback(() => {
    if (!org || !repo) return
    const previous = aiRequestRef.current
    if (
      previous?.key === aiRequestKey &&
      previous.active &&
      (previous.status === "loading" || previous.status === "ready")
    ) {
      return
    }

    const request: { key: string; status: "loading" | "ready" | "error"; active: boolean } = {
      key: aiRequestKey,
      status: "loading",
      active: true,
    }
    aiRequestRef.current = request
    setState((current) => ({ ...current, ai: undefined, aiLoading: true, aiError: undefined }))

    void (async () => {
      try {
        const response = await pollUntilReady(
          () => api.getRecommendations(org, repo, pat),
          () => request.active,
        )
        if (!request.active) return
        request.status = response.status === "PENDING" ? "error" : "ready"
        setState((current) => ({
          ...current,
          ai: aiReportFromResponse(response),
          aiLoading: false,
          aiError: response.status === "PENDING" ? "AI-сводка ещё рассчитывается. Попробуйте запросить её позже." : undefined,
        }))
      } catch (error) {
        if (!request.active) return
        request.status = "error"
        setState((current) => ({
          ...current,
          aiLoading: false,
          aiError: `AI-сводка недоступна: ${errorText(error, "ошибка запроса.").replace(/^./, (c) => c.toLowerCase())}`,
        }))
      }
    })()
  }, [api, org, repo, pat, aiRequestKey])

  const run = useCallback(
    () => setReruns((current) => ({ key, count: current.key === key ? current.count + 1 : 1 })),
    [key],
  )
  return { ...state, run, loadAiSummary }
}
