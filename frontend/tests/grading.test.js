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

const view = await vite.ssrLoadModule("/src/gradingBatchView.ts")
const audit = await vite.ssrLoadModule("/src/auditView.ts")
const { HttpRepoHealthApi, ApiRequestError } = await vite.ssrLoadModule("/src/api/client.ts")
const { default: GradingPanel } = await vite.ssrLoadModule("/src/components/GradingPanel.tsx")

// Every valid form needs the limit; the base form has one.
const form = (patch = {}) => ({ ...view.EMPTY_BATCH_FORM, limit: "100", ...patch })

test("the limit is required; with it alone every public repository qualifies", () => {
  assert.match(view.buildFilter(form({ limit: "" })).problem, /сколько репозиториев/)
  assert.match(view.buildFilter(form({ limit: "0" })).problem, /Не больше репозиториев/)
  assert.match(view.buildFilter(form({ limit: "abc" })).problem, /целое/)
  assert.deepEqual(view.buildFilter(form()).filter, { limit: 100 })
  // No business upper bound.
  assert.deepEqual(view.buildFilter(form({ limit: "30000" })).filter, { limit: 30000 })
})

test("the form turns into a filter: trimmed, de-duplicated, numbers parsed", () => {
  const built = view.buildFilter(
    form({ languages: " Go, go ;Rust\n", minScore: "10", maxScore: "60", olderThanDays: "30", repos: "org/a, org/b\norg/a" }),
  )
  assert.deepEqual(built.filter, {
    limit: 100,
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
  const f = (patch) => ({ languages: null, min_score: null, max_score: null, older_than_days: null, repos: null, limit: 0, only_regrade: false, force: false, ...patch })
  assert.equal(
    view.filterSummary(f({ languages: ["Go"], min_score: 10, max_score: 60, older_than_days: 30, repos: ["a/b"] })),
    "языки: Go; оценка 10–60; оценке больше 30 дн. или её нет; список: 1",
  )
  assert.equal(view.filterSummary(f({ limit: 500, only_regrade: true })), "не больше 500 по рейтингу SourceCraft; только переоценка")
  // A batch from before the limit existed shows no limit.
  assert.equal(view.filterSummary(f({})), "—")
  assert.equal(view.filterSummary(f({ max_score: 40 })), "оценка до 40")
})

test("refusal reasons map to wording; denial gets the admin message", () => {
  assert.match(view.batchErrorMessage(409, "batch_active"), /уже выполняется/)
  assert.equal(view.batchErrorMessage(400, "too_many"), undefined)
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
  await api.previewBatch({ limit: 10 })
  await api.startBatch({ limit: 10, languages: ["go"] })
  await api.controlBatch(7, "pause")
  await api.getCurrentBatch()
  await api.getBatch(7)
  await api.listBatches(5)
  assert.deepEqual(
    calls.map((c) => [c.method, c.url, c.guard]),
    [
      ["POST", "/api/admin/grading/preview", "1"],
      ["POST", "/api/admin/grading/batches", "1"],
      ["POST", "/api/admin/grading/batches/7/pause", "1"],
      ["GET", "/api/admin/grading/batches/current", undefined],
      ["GET", "/api/admin/grading/batches/7", undefined],
      ["GET", "/api/admin/grading/batches?limit=5", undefined],
    ],
  )
  assert.equal(calls[1].body, JSON.stringify({ limit: 10, languages: ["go"] }))
})

test("a refused batch request keeps its reason code and gets Russian wording", async () => {
  const api = new HttpRepoHealthApi({
    fetcher: async () => new Response(JSON.stringify({ error: "conflict", code: "batch_active" }), { status: 409 }),
  })
  await assert.rejects(
    () => api.startBatch({ limit: 10 }),
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

test("batch actions have audit-log labels, old entries keep theirs", () => {
  for (const action of ["start", "pause", "resume", "cancel", "complete"]) {
    assert.match(audit.actionLabel(`batch_grading.${action}`), /пакетн.. оценк/i)
    assert.match(audit.actionLabel(`regrade.batch.${action}`), /переоценк/)
  }
})

test("the panel starts in its loading state and says private repos are excluded", () => {
  const api = new HttpRepoHealthApi({ fetcher: async () => new Response("{}", { status: 200 }) })
  const html = renderToStaticMarkup(createElement(GradingPanel, { api }))
  assert.match(html, /Пакетная оценка/)
  assert.match(html, /Приватные репозитории в пакет не попадают/)
  assert.match(html, /Загружаем/)
  assert.doesNotMatch(html, /админк/i)
})

test("the own list takes SourceCraft links as well as slugs, de-duplicated after conversion", () => {
  const built = view.buildFilter(
    form({
      repos: "org/a https://sourcecraft.dev/org/b\nsourcecraft.dev/Org/A www.sourcecraft.dev/org/c.git, http://sourcecraft.dev/org/d/tree/main",
    }),
  )
  assert.deepEqual(built.filter, { limit: 100, repos: ["org/a", "org/b", "org/c", "org/d"] })

  for (const bad of ["https://sourcecraft.dev/org", "https://github.com/org/repo", "just-a-name"]) {
    const refused = view.buildFilter(form({ repos: bad }))
    assert.equal(refused.filter, undefined, bad)
    assert.match(refused.problem, /организация\/репозиторий или ссылка/, bad)
  }

  // The 200 limit counts repositories, not entries: a link and a slug for one repository count once.
  const many = Array.from({ length: 200 }, (_, i) => `o/r${i}`).join(" ")
  assert.equal(view.buildFilter(form({ repos: `${many} https://sourcecraft.dev/o/r0` })).filter.repos.length, 200)
})

test("a stand maximum bounds the limit; 0 means none", () => {
  assert.deepEqual(view.buildFilter(form({ limit: "50" }), 50).filter, { limit: 50 })
  assert.match(view.buildFilter(form({ limit: "51" }), 50).problem, /Не больше репозиториев: от 1 до 50/)
  assert.deepEqual(view.buildFilter(form({ limit: "100000" }), 0).filter, { limit: 100000 })
  assert.match(view.batchErrorMessage(400, "limit_too_high"), /этом стенде/)
})

test("only regrade and force go into the filter", () => {
  assert.deepEqual(view.buildFilter(form({ onlyRegrade: true, languages: "Go" })).filter, {
    limit: 100,
    languages: ["Go"],
    only_regrade: true,
  })
  assert.deepEqual(view.buildFilter(form({ force: true })).filter, { limit: 100, force: true })
})

test("the summary names a forced batch; a skipped repo is explained", () => {
  const base = { languages: null, min_score: null, max_score: null, older_than_days: null, repos: null, limit: 10, only_regrade: false, force: true }
  assert.equal(view.filterSummary(base), "не больше 10 по рейтингу SourceCraft; принудительно")
  assert.equal(view.itemErrorLabel("no_activity"), "нет новой активности с последней оценки")
})
