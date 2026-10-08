import { useCallback, useEffect, useState } from "react"

/**
 * URL routing on the History API. Paths:
 *   /                      public ranking
 *   /analytics             statistics over all public grades and the grades export
 *   /admin-access          "Доступ админа": enter the admin password; the admin tools show here for admins
 *   /admin                 an old link, opens the same tab
 *   /my                    the signed-in user's repositories
 *   /repo/:org/:repo       public analysis
 *   /my/:org/:repo         private analysis with the owner's token
 *   /rules[/:section]      grading rules, one section per category
 *   /compare/:orgA/:repoA/:orgB/:repoB   two public repositories side by side
 * nginx (`try_files … /index.html`) and the Vite dev server serve index.html for all of them.
 */
export interface RepoRef {
  org: string
  repo: string
}

export type Route =
  | { page: "ranking" }
  | { page: "my" }
  | { page: "analytics" }
  | { page: "access" }
  | { page: "rules"; section?: string }
  | { page: "compare"; a: RepoRef; b: RepoRef }
  | { page: "repository"; org: string; repo: string; private: boolean }

function decode(segment: string): string | undefined {
  try {
    return decodeURIComponent(segment)
  } catch {
    return undefined
  }
}

export function parseRoute(pathname: string): Route {
  const [first, org, repo, orgB, repoB] = pathname.split("/").filter(Boolean).map(decode)
  if (first === "compare" && org && repo && orgB && repoB) {
    return { page: "compare", a: { org, repo }, b: { org: orgB, repo: repoB } }
  }
  if ((first === "repo" || first === "my") && org && repo) {
    return { page: "repository", org, repo, private: first === "my" }
  }
  if (first === "my") return { page: "my" }
  if (first === "analytics") return { page: "analytics" }
  if (first === "admin" || first === "admin-access") return { page: "access" }
  if (first === "rules") return { page: "rules", section: org }
  return { page: "ranking" }
}

export function routePath(route: Route): string {
  if (route.page === "my") return "/my"
  if (route.page === "analytics") return "/analytics"
  if (route.page === "access") return "/admin-access"
  if (route.page === "ranking") return "/"
  if (route.page === "compare") {
    const part = (ref: RepoRef) => `${encodeURIComponent(ref.org)}/${encodeURIComponent(ref.repo)}`
    return `/compare/${part(route.a)}/${part(route.b)}`
  }
  if (route.page === "rules") return route.section ? `/rules/${encodeURIComponent(route.section)}` : "/rules"
  const prefix = route.private ? "my" : "repo"
  return `/${prefix}/${encodeURIComponent(route.org)}/${encodeURIComponent(route.repo)}`
}

/** Accepts https://sourcecraft.dev/org/repo with or without scheme, www, ".git" or a trailing path. */
export function parseRepositoryLink(link: string): { org: string; repo: string } | null {
  const text = link.trim()
  if (!text) return null
  try {
    const url = new URL(/^https?:\/\//i.test(text) ? text : `https://${text}`)
    if (!/^(www\.)?sourcecraft\.dev$/i.test(url.hostname)) return null
    const [org, repo] = url.pathname.split("/").filter(Boolean).map(decode)
    if (!org || !repo) return null
    return { org, repo: repo.replace(/\.git$/i, "") }
  } catch {
    return null
  }
}

export function useRoute() {
  const [route, setRoute] = useState<Route>(() => parseRoute(window.location.pathname))

  useEffect(() => {
    const sync = () => setRoute(parseRoute(window.location.pathname))
    window.addEventListener("popstate", sync)
    return () => window.removeEventListener("popstate", sync)
  }, [])

  const navigate = useCallback((next: Route) => {
    const path = routePath(next)
    if (path !== window.location.pathname) window.history.pushState(null, "", path)
    setRoute(next)
    window.scrollTo(0, 0)
  }, [])

  return [route, navigate] as const
}
