import type { HttpRepoHealthApi } from "../api/client"
import type { RepoRef } from "../route"
import { useCompareSide } from "../useCompare"
import ComparePage from "./ComparePage"

/** The /compare route: loads both repositories and keeps the URL in step with the pickers. */
export default function CompareRoute({
  api,
  a,
  b,
  onNavigate,
  onOpenRepository,
  onBack,
}: {
  api: Pick<HttpRepoHealthApi, "getPublicScore" | "getHistory">
  a: RepoRef
  b: RepoRef
  onNavigate: (a: RepoRef, b: RepoRef) => void
  onOpenRepository: (ref: RepoRef) => void
  onBack: () => void
}) {
  const left = useCompareSide(api, a)
  const right = useCompareSide(api, b)

  return (
    <ComparePage
      left={left}
      right={right}
      onChange={(which, ref) => (which === "left" ? onNavigate(ref, b) : onNavigate(a, ref))}
      onOpenRepository={onOpenRepository}
      onBack={onBack}
    />
  )
}
