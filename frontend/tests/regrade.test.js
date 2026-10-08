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

const view = await vite.ssrLoadModule("/src/regradeView.ts")
const audit = await vite.ssrLoadModule("/src/auditView.ts")
const { HttpRepoHealthApi, ApiRequestError } = await vite.ssrLoadModule("/src/api/client.ts")
const { default: RegradePanel } = await vite.ssrLoadModule("/src/components/RegradePanel.tsx")

const form = (patch = {}) => ({ ...view.EMPTY_BATCH_FORM, ...patch })

test("an empty form is refused with a hint, 'all' needs no conditions", () => {
  assert.match(view.buildFilter(form()).problem, /хотя бы одно условие/)
  assert.deepEqual(view.buildFilter(form({ mode: "all", languages: "go" })).filter, { all: true })
})

test("the form turns into a filter: trimmed, de-duplicated, numbers parsed", () => {
  const built = view.buildFilter(
    form({ languages: " Go, go ;Rust\n", minScore: "10", maxScore: "60", olderThanDays: "30", repos: "org/a, org/b\norg/a" }),
  )
  assert.deepEqual(built.filter, {
    languages: ["Go", "Rust"],
    repos: ["org/a", "org/b"],
    min_score: 10,
    max_score: 60,
    older_than_days: 30,
  })
})

test("bad values are named in Russian and never produce a filter", () => {
  const cases = [
    [{ minScore: "abc" }, /Оценка от.*целое/],
    [{ maxScore: "101" }, /Оценка до.*от 0 до 100/],
    [{ minScore: "70", maxScore: "20" }, /«от» больше/],
    [{ olderThanDays: "0" }, /Давность оценки/],
    [{ repos: "just-a-name" }, /организация\/репозиторий/],
    [{ repos: "org/repo?x=1" }, /организация\/репозиторий/],
    [{ repos: Array.from({ length: 201 }, (_, i) => `o/r${i}`).join(" ") }, /не больше 200/],
    [{ languages: Array.from({ length: 21 }, (_, i) => `l${i}`).join(",") }, /не больше 20/],
  ]
  for (const [patch, pattern] of cases) {
    const built = view.buildFilter(form(patch))
    assert.equal(built.filter, undefined, JSON.stringify(patch))
    assert.match(built.problem, pattern)
  }
})

test("progress counts finished, failed and skipped repositories", () => {
  assert.deepEqual(view.batchProgress({ total: 8, ok: 3, failed: 1, skipped: 0 }), { done: 4, percent: 50 })
  assert.deepEqual(view.batchProgress({ total: 0, ok: 0, failed: 0, skipped: 0 }), { done: 0, percent: 0 })
  assert.equal(view.isActive({ status: "paused" }), true)
  assert.equal(view.isActive({ status: "cancelled" }), false)
})

test("a batch is followed while active or while repositories are still in flight", () => {
  assert.equal(view.shouldPoll({ status: "running", running: 0 }), true)
  assert.equal(view.shouldPoll({ status: "paused", running: 0 }), true)
  assert.equal(view.shouldPoll({ status: "cancelled", running: 2 }), true)
  assert.equal(view.shouldPoll({ status: "cancelled", running: 0 }), false)
  assert.equal(view.shouldPoll({ status: "completed", running: 0 }), false)
})

test("estimates, statuses and error codes read as Russian", () => {
  assert.equal(view.formatEstimate(30), "меньше минуты")
  assert.equal(view.formatEstimate(45 * 60), "≈ 45 мин")
  assert.equal(view.formatEstimate(90 * 60), "≈ 1 ч 30 мин")
  assert.equal(view.formatEstimate(2 * 3600), "≈ 2 ч")
  assert.equal(view.statusLabel("paused").label, "На паузе")
  assert.equal(view.statusLabel("weird").label, "weird")
  assert.match(view.itemErrorLabel("timeout"), /вовремя/)
  assert.equal(view.itemErrorLabel("new_code"), "new_code")
})

test("filter summaries describe the conditions", () => {
  const f = (patch) => ({ languages: null, min_score: null, max_score: null, older_than_days: null, repos: null, all: false, ...patch })
  assert.equal(view.filterSummary(f({ all: true })), "Все публичные репозитории")
  assert.equal(
    view.filterSummary(f({ languages: ["Go"], min_score: 10, max_score: 60, older_than_days: 30, repos: ["a/b"] })),
    "языки: Go; оценка 10–60; оценке больше 30 дн. или её нет; список: 1",
  )
  assert.equal(view.filterSummary(f({ max_score: 40 })), "оценка до 40")
})

test("refusal reasons map to wording; denial gets the admin message", () => {
  assert.match(view.batchErrorMessage(409, "batch_active"), /уже выполняется/)
  assert.match(view.batchErrorMessage(400, "too_many"), /Сузьте/)
  assert.match(view.batchErrorMessage(403, ""), /администратор/)
  assert.equal(view.batchErrorMessage(500, ""), undefined)
})

test("batch calls use the admin routes, the guard header and a JSON body", async () => {
  const calls = []
  const api = new HttpRepoHealthApi({
    fetcher: async (url, init) => {
      calls.push({ url, method: init.method ?? "GET", guard: init.headers["X-Admin-Request"], body: init.body })
      return new Response(JSON.stringify({ batches: [] }), { status: 200 })
    },
  })
  await api.previewBatch({ all: true })
  await api.startBatch({ languages: ["go"] })
  await api.controlBatch(7, "pause")
  await api.getCurrentBatch()
  await api.getBatch(7)
  await api.listBatches(5)
  assert.deepEqual(
    calls.map((c) => [c.method, c.url, c.guard]),
    [
      ["POST", "/api/admin/regrade/preview", "1"],
      ["POST", "/api/admin/regrade/batches", "1"],
      ["POST", "/api/admin/regrade/batches/7/pause", "1"],
      ["GET", "/api/admin/regrade/batches/current", undefined],
      ["GET", "/api/admin/regrade/batches/7", undefined],
      ["GET", "/api/admin/regrade/batches?limit=5", undefined],
    ],
  )
  assert.equal(calls[1].body, JSON.stringify({ languages: ["go"] }))
})

test("a refused batch request keeps its reason code and gets Russian wording", async () => {
  const api = new HttpRepoHealthApi({
    fetcher: async () => new Response(JSON.stringify({ error: "conflict", code: "batch_active" }), { status: 409 }),
  })
  await assert.rejects(
    () => api.startBatch({ all: true }),
    (error) => error instanceof ApiRequestError && error.status === 409 && error.code === "batch_active" && /уже выполняется/.test(error.message),
  )

  const denied = new HttpRepoHealthApi({ fetcher: async () => new Response("{}", { status: 403 }) })
  await assert.rejects(
    () => denied.listBatches(),
    (error) => error.status === 403 && /администратор/.test(error.message) && !/Репозиторий/.test(error.message),
  )

  const offline = new HttpRepoHealthApi({ fetcher: async () => Promise.reject(new TypeError("down")) })
  await assert.rejects(
    () => offline.listBatches(),
    (error) => error.status === 0 && /подключение/.test(error.message),
  )
})

test("batch actions have audit-log labels", () => {
  for (const action of ["start", "pause", "resume", "cancel", "complete"]) {
    assert.match(audit.actionLabel(`regrade.batch.${action}`), /переоценк/)
  }
})

test("the panel starts in its loading state and says private repos are excluded", () => {
  const api = new HttpRepoHealthApi({ fetcher: async () => new Response("{}", { status: 200 }) })
  const html = renderToStaticMarkup(createElement(RegradePanel, { api }))
  assert.match(html, /Пакетная переоценка/)
  assert.match(html, /Приватные репозитории в пакет не попадают/)
  assert.match(html, /Загружаем/)
  assert.doesNotMatch(html, /админк/i)
})

test("top by rating and force go into the filter; force also applies to 'all'", () => {
  assert.deepEqual(view.buildFilter(form({ topRated: "100", languages: "Go" })).filter, {
    languages: ["Go"],
    top_rated: 100,
  })
  assert.deepEqual(view.buildFilter(form({ topRated: "5", force: true })).filter, { top_rated: 5, force: true })
  assert.deepEqual(view.buildFilter(form({ mode: "all", force: true })).filter, { all: true, force: true })
  assert.match(view.buildFilter(form({ force: true })).problem, /хотя бы одно условие/)
  assert.match(view.buildFilter(form({ topRated: "0" })).problem, /Топ по рейтингу.*от 1 до 1000/)
  assert.match(view.buildFilter(form({ topRated: "1001" })).problem, /от 1 до 1000/)
})

test("the summary names the top and a forced batch; a skipped repo is explained", () => {
  const base = { languages: null, min_score: null, max_score: null, older_than_days: null, repos: null, top_rated: null, all: false, force: false }
  assert.equal(view.filterSummary({ ...base, top_rated: 50, languages: ["go"] }), "топ 50 по рейтингу SourceCraft; языки: go")
  assert.equal(view.filterSummary({ ...base, all: true, force: true }), "Все публичные репозитории; принудительно")
  assert.equal(view.filterSummary({ ...base, top_rated: 10, force: true }), "топ 10 по рейтингу SourceCraft; принудительно")
  assert.equal(view.itemErrorLabel("no_activity"), "нет новой активности с последней оценки")
})
