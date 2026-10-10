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

const view = await vite.ssrLoadModule("/src/auditView.ts")
const { parseRoute, routePath } = await vite.ssrLoadModule("/src/route.ts")
const { HttpRepoHealthApi, ApiRequestError } = await vite.ssrLoadModule("/src/api/client.ts")

const entry = (extra = {}) => ({
  id: 7,
  at: "2026-10-07T14:17:40Z",
  actor_kind: "user",
  actor: "e490a85d15a0c0b5",
  action: "admin.denied",
  target_kind: "route",
  target: "/admin/audit",
  outcome: "denied",
  details: { reason: "not_admin" },
  ...extra,
})

test("the old /admin link opens the single access tab", () => {
  assert.deepEqual(parseRoute("/admin"), { page: "access" })
  assert.equal(routePath({ page: "access" }), "/admin-access")
})

test("an audit entry becomes a readable row", () => {
  const row = view.auditRow(entry())
  assert.equal(row.action, "Отказ в доступе администратора")
  assert.equal(row.actorKind, "Пользователь")
  assert.equal(row.actor, "e490a85d15a0c0b5")
  assert.equal(row.target, "Маршрут: /admin/audit")
  assert.deepEqual(row.outcome, { label: "Отказ", tone: "warning" })
  assert.equal(row.note, "не администратор")
})

test("unknown actions, outcomes and reasons are shown as they are, not hidden", () => {
  const row = view.auditRow(
    entry({ action: "report.export.create", outcome: "queued", actor_kind: "robot", details: { reason: "odd" }, target: "" }),
  )
  assert.equal(row.action, "report.export.create")
  assert.deepEqual(row.outcome, { label: "queued", tone: "neutral" })
  assert.equal(row.actorKind, "robot")
  assert.equal(row.note, "odd")
  assert.equal(row.target, "")
})

test("missing or non-text details give no note", () => {
  assert.equal(view.reasonLabel(undefined), "")
  assert.equal(view.reasonLabel(null), "")
  assert.equal(view.reasonLabel({}), "")
  assert.equal(view.reasonLabel({ reason: 5 }), "")
})

test("an anonymous entry has no hash to show", () => {
  const row = view.auditRow(entry({ actor_kind: "anonymous", actor: "" }))
  assert.equal(row.actorKind, "Аноним")
  assert.equal(row.actor, "")
})

test("getAudit sends the cursor, outcome and limit, and asks the admin route", async () => {
  const urls = []
  const api = new HttpRepoHealthApi({
    fetcher: async (url) => {
      urls.push(url)
      return new Response(JSON.stringify({ entries: [], next_before_id: 0 }), { status: 200 })
    },
  })
  await api.getAudit()
  await api.getAudit({ outcome: "denied", beforeId: 41, limit: 25 })
  assert.deepEqual(urls, ["/api/admin/audit", "/api/admin/audit?limit=25&before_id=41&outcome=denied"])
})

test("a 401 or 403 on the audit log gets admin wording, not the repository message", async () => {
  for (const status of [401, 403]) {
    const api = new HttpRepoHealthApi({ fetcher: async () => new Response("{}", { status }) })
    await assert.rejects(
      () => api.getAudit(),
      (error) => error instanceof ApiRequestError && error.status === status && /администратор/.test(error.message) && !/Репозиторий/.test(error.message),
    )
  }
})

test("getAdminMe asks /admin/me and passes the answer through", async () => {
  const urls = []
  const api = new HttpRepoHealthApi({
    fetcher: async (url) => {
      urls.push(url)
      return new Response(JSON.stringify({ is_admin: true }), { status: 200 })
    },
  })
  assert.deepEqual(await api.getAdminMe(), { is_admin: true })
  assert.deepEqual(urls, ["/api/admin/me"])
})

test("the log pages ten at a time, forward by cursor and back through the cursors already used", () => {
  assert.equal(view.AUDIT_PAGE_SIZE, 10)

  let cursors = view.FIRST_AUDIT_PAGE
  assert.deepEqual(cursors, [0])

  cursors = view.nextAuditPage(cursors, 41)
  cursors = view.nextAuditPage(cursors, 31)
  assert.deepEqual(cursors, [0, 41, 31])

  // The last page has no cursor: "next" changes nothing.
  assert.deepEqual(view.nextAuditPage(cursors, 0), [0, 41, 31])

  assert.deepEqual(view.previousAuditPage(cursors), [0, 41])
  assert.deepEqual(view.previousAuditPage([0]), [0])
})

test("the pager shows the page number, and only the directions that exist", async () => {
  const { default: AuditPager } = await vite.ssrLoadModule("/src/components/AuditPager.tsx")
  const html = (props) =>
    renderToStaticMarkup(createElement(AuditPager, { page: 2, hasPrevious: true, hasNext: true, isLoading: false, onPrevious() {}, onNext() {}, ...props }))

  const both = html({})
  assert.match(both, /Страница 2/)
  assert.doesNotMatch(both, /disabled/)

  assert.match(html({ hasNext: false }), /Дальше<\/button>/)
  assert.match(html({ hasNext: false }), /disabled=""[^>]*>Дальше/)
  assert.doesNotMatch(html({ hasNext: false }), /disabled=""[^>]*>Назад/)
  assert.match(html({ isLoading: true }), /disabled=""[^>]*>Назад/)
  assert.equal(html({ hasPrevious: false, hasNext: false }), "")
})

test("a user action reads as a request with its state, and a token request says the repo is hidden", () => {
  const row = view.auditRow(
    entry({
      actor_kind: "user",
      action: "grading.request",
      target_kind: "repo",
      target: "3f9a1c0b7d2e4a65",
      details: { state: "pending", access: "token" },
      outcome: "ok",
    }),
  )
  assert.equal(row.action, "Запрос оценки репозитория")
  assert.equal(row.target, "Репозиторий: 3f9a1c0b7d2e4a65")
  assert.equal(row.note, "оценка ещё считается · по токену, репозиторий скрыт хешем")

  const recommendation = view.auditRow(
    entry({ action: "recommendations.request", details: { reason: "grading_limit", access: "public" }, outcome: "denied" }),
  )
  assert.equal(recommendation.action, "Запрос рекомендаций от модели")
  assert.equal(recommendation.note, "достигнут общий лимит оценок")
})

test("the user-action filter names both actions and the all option sends nothing", () => {
  assert.deepEqual(
    view.USER_ACTION_FILTERS.map((option) => option.value),
    ["", "grading.request", "grading.force", "recommendations.request"],
  )
})

test("getAudit sends the scope and the action", async () => {
  const urls = []
  const api = new HttpRepoHealthApi({
    fetcher: async (url) => {
      urls.push(url)
      return new Response(JSON.stringify({ entries: [], next_before_id: 0 }), { status: 200 })
    },
  })
  await api.getAudit({ scope: "users", action: "grading.request", limit: 10 })
  await api.getAudit({ scope: "admin" })
  assert.deepEqual(urls, ["/api/admin/audit?limit=10&action=grading.request&scope=users", "/api/admin/audit?scope=admin"])
})

test("the access page shows the admin journal and the user-action block", async () => {
  const { default: AuditLog } = await vite.ssrLoadModule("/src/components/AuditLog.tsx")
  const { renderToStaticMarkup } = await import("react-dom/server")
  const { createElement } = await import("react")
  const api = new HttpRepoHealthApi({ fetcher: async () => new Response("{}", { status: 200 }) })
  const users = renderToStaticMarkup(createElement(AuditLog, { api, scope: "users" }))
  const admin = renderToStaticMarkup(createElement(AuditLog, { api }))
  assert.match(users, /Действия пользователей/)
  assert.match(users, /Запросы рекомендаций/)
  assert.doesNotMatch(admin, /Запросы рекомендаций/)
  assert.match(admin, /Входы и действия администраторов/)
})
