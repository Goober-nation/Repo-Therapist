import assert from "node:assert/strict"

import { after, test } from "node:test"

import { createElement } from "react"

import { renderToStaticMarkup } from "react-dom/server"

import { createServer } from "vite"

const vite = await createServer({
  server: { middlewareMode: true, watch: null, hmr: false },
  appType: "custom",
})

after(() => vite.close())

const { sessionLost, forIdentity } = await vite.ssrLoadModule("/src/session.ts")
const { default: TopBar } = await vite.ssrLoadModule("/src/components/TopBar.tsx")
const { HttpRepoHealthApi } = await vite.ssrLoadModule("/src/api/client.ts")
const view = await vite.ssrLoadModule("/src/auditView.ts")

const status = (extra = {}) => ({ is_admin: false, signed_in: true, unlock_available: true, via_password: false, ...extra })
const profile = { id: "1", name: "Timoxa102928" }

test("a saved profile whose session the Gateway no longer knows is a lost session", () => {
  assert.equal(sessionLost(profile, status({ signed_in: false })), true)
})

test("a live session, no profile, or an unknown status is never a lost session", () => {
  assert.equal(sessionLost(profile, status()), false, "the Gateway knows the session")
  assert.equal(sessionLost(null, status({ signed_in: false })), false, "nothing to drop")
  assert.equal(sessionLost(profile, undefined), false, "a status that is still loading or failed must not sign anyone out")
})

test("a status loaded for another identity is unknown, so a sign-in is not undone by the visitor's old answer", () => {
  const visitorStatus = { identity: null, value: status({ signed_in: false }) }

  assert.equal(forIdentity(visitorStatus, null), visitorStatus.value)
  assert.equal(forIdentity(visitorStatus, profile), undefined, "right after the sign-in the old answer is stale")
  assert.equal(sessionLost(profile, forIdentity(visitorStatus, profile)), false)
  assert.equal(forIdentity(undefined, profile), undefined)
})

const topBar = (yandexProfile) =>
  renderToStaticMarkup(
    createElement(TopBar, {
      activePage: "ranking",
      repositoryName: "",
      theme: "light",
      onToggleTheme() {},
      onPageChange() {},
      isSidebarOpen: false,
      onToggleSidebar() {},
      onOpenRepositoryLink: () => true,
      yandexProfile,
      onYandexSignIn() {},
      onSignOut() {},
    }),
  )

test("the top bar has a sign-out button only for a signed-in user", () => {
  const signedIn = topBar(profile)
  assert.match(signedIn, /Выйти/)
  assert.match(signedIn, /Timoxa102928/)
  assert.doesNotMatch(topBar(null), /Выйти/)
})

test("logout posts to /auth/logout with the guard header and the cookie, and no body", async () => {
  let seen
  const api = new HttpRepoHealthApi({
    fetcher: async (url, init) => {
      seen = { url, init }
      return new Response(JSON.stringify({ signed_out: true }), { status: 200 })
    },
  })

  await api.logout()

  assert.equal(seen.url, "/api/auth/logout")
  assert.equal(seen.init.method, "POST")
  assert.equal(seen.init.headers["X-Admin-Request"], "1")
  assert.equal(seen.init.credentials, "include")
  assert.equal(seen.init.body, undefined)
})

test("a failed logout request rejects, so the caller can ignore it while still signing out locally", async () => {
  const api = new HttpRepoHealthApi({ fetcher: async () => new Response("{}", { status: 500 }) })
  await assert.rejects(() => api.logout())
})

test("the log reads a lock caused by logout in Russian", () => {
  const row = view.auditRow({
    id: 1, at: "2026-10-07T14:00:00Z", actor_kind: "admin", actor: "ab12", action: "admin.lock",
    target_kind: "route", target: "/auth/logout", outcome: "ok", details: { via: "logout" },
  })
  assert.equal(row.note, "при выходе из аккаунта")
})
