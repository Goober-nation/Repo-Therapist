import { useEffect, useState } from "react"
import { ApiRequestError, type HttpRepoHealthApi } from "./api/client"
import { confidenceFromPayload, historyFromResponse } from "./api/mapper"
import type { RepoRef } from "./route"
import type { CompareSide } from "./types"

const NOT_FOUND = "Репозиторий не найден среди публичных. Проверьте имя организации и репозитория."

export function loadingSide(ref: RepoRef): CompareSide {
  return { ...ref, state: "loading", score: null, categories: [], history: [] }
}

/** Public grade and history of one repository for the compare page; read-only. */
export function useCompareSide(
  api: Pick<HttpRepoHealthApi, "getPublicScore" | "getHistory">,
  ref: RepoRef,
): CompareSide {
  const [side, setSide] = useState<CompareSide>(() => loadingSide(ref))

  useEffect(() => {
    let active = true
    setSide(loadingSide(ref))

    Promise.allSettled([api.getPublicScore(ref.org, ref.repo), api.getHistory(ref.org, ref.repo)]).then(
      ([grade, history]) => {
        if (!active) return
        if (grade.status === "rejected") {
          const notFound = grade.reason instanceof ApiRequestError && grade.reason.status === 404
          setSide({
            ...loadingSide(ref),
            state: "error",
            error: notFound ? NOT_FOUND : "Не удалось загрузить данные репозитория. Попробуйте ещё раз.",
          })
          return
        }

        const response = grade.value
        const graded = response.status === "FINISHED" && typeof response.score === "number"
        setSide({
          ...ref,
          state: "ready",
          name: response.name || ref.repo,
          webUrl: response.web_url,
          score: graded ? Math.round(response.score as number) : null,
          level: response.level,
          gradedAt: response.graded_at ?? undefined,
          categories: (response.categories ?? []).flatMap((category) =>
            typeof category?.id === "string"
              ? [{ id: category.id, score: category.no_data || typeof category.score !== "number" ? null : Math.round(category.score) }]
              : [],
          ),
          confidence: confidenceFromPayload(response.low_confidence, response.confidence_reasons),
          history: history.status === "fulfilled" ? historyFromResponse(history.value) : [],
        })
      },
    )

    return () => {
      active = false
    }
  }, [api, ref.org, ref.repo])

  return side
}
