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

const view = await vite.ssrLoadModule("/src/statsView.ts")
const { parseRoute, routePath } = await vite.ssrLoadModule("/src/route.ts")
const { default: AnalyticsPage } = await vite.ssrLoadModule("/src/pages/AnalyticsPage.tsx")
const { HttpRepoHealthApi } = await vite.ssrLoadModule("/src/api/client.ts")

const stats = (extra = {}) => ({
  graded_repos: 28,
  avg_score: 51.2,
  median_score: 47,
  distribution: [
    { min: 0, max: 9, repos: 0 },
    { min: 10, max: 19, repos: 4 },
    { min: 90, max: 100, repos: 8 },
  ],
  levels: [
    { level: "healthy", repos: 7 },
    { level: "at_risk", repos: 21 },
  ],
  languages: [{ name: "Go", color: "#00ADD8", repos: 9, avg_score: 50.6 }],
  categories: [{ id: "activity", avg_score: 38.1, repos: 28 }],
  top_advice: [
    { category_id: "documentation", metric_id: "DOC_CODEOWNERS_MISSING", priority: "low", repos: 14 },
    { category_id: "documentation", metric_id: "DOC_ALGORITHMIC_NOTICE", priority: "low", repos: 20 },
    { category_id: "activity", metric_id: "commits_per_week", priority: "high", repos: 7 },
  ],
  trend: [
    { week_start: "2026-09-28T00:00:00Z", avg_score: 51.5, repos: 26 },
    { week_start: null, avg_score: 10, repos: 1 },
  ],
  most_improved: [],
  improved_days: 7,
  min_language_repos: 3,
  generated_at: "2026-10-07T13:40:18Z",
  ...extra,
})

test("distribution bars scale to the tallest bucket and keep real counts", () => {
  const bars = view.distributionBars(stats())
  assert.deepEqual(
    bars.map((bar) => [bar.label, bar.value, bar.width]),
    [
      ["0–9", 0, 0],
      ["10–19", 4, 50],
      ["90–100", 8, 100],
    ],
  )
})

test("an empty distribution does not divide by zero", () => {
  const bars = view.distributionBars(stats({ distribution: [{ min: 0, max: 9, repos: 0 }] }))
  assert.equal(bars[0].width, 0)
})

test("levels carry a share of all graded repositories; no repositories gives 0%", () => {
  assert.deepEqual(
    view.levelItems(stats()).map((level) => [level.label, level.repos, level.share]),
    [
      ["Здоровый", 7, 25],
      ["Под риском", 21, 75],
    ],
  )
  assert.equal(view.percentOf(3, 0), 0)
})

test("advice shows Russian labels, hides internal notices, and gives the share of graded repos", () => {
  const items = view.adviceItems(stats())
  assert.equal(items.length, 2)
  assert.equal(items[0].metric, "Нет CODEOWNERS")
  assert.equal(items[0].share, 50)
  assert.ok(items.every((item) => !item.metric.includes("ALGORITHMIC")))
})

test("an unknown advice code falls back to its id rather than disappearing", () => {
  assert.equal(view.adviceLabel("SOMETHING_NEW"), "SOMETHING_NEW")
})

test("trend drops a week without a start time", () => {
  assert.deepEqual(view.trendPoints(stats()), [{ at: "2026-09-28T00:00:00Z", value: 51.5 }])
})

test("language and category bars clamp the width into 0-100", () => {
  const out = view.languageBars(stats({ languages: [{ name: "X", color: "", repos: 3, avg_score: 140 }] }))
  assert.equal(out[0].width, 100)
  assert.equal(view.categoryBars(stats({ categories: [{ id: "issues", avg_score: -4, repos: 1 }] }))[0].width, 0)
})

test("the analytics route round-trips", () => {
  assert.deepEqual(parseRoute("/analytics"), { page: "analytics" })
  assert.equal(routePath({ page: "analytics" }), "/analytics")
})

test("export links point at the Gateway export routes", () => {
  const api = new HttpRepoHealthApi({ baseUrl: "/api/" })
  assert.equal(api.exportUrl("csv"), "/api/export/grades.csv")
  assert.equal(api.exportUrl("json"), "/api/export/grades.json")
})

test("getStats requests /stats", async () => {
  const urls = []
  const api = new HttpRepoHealthApi({
    fetcher: async (url) => {
      urls.push(url)
      return new Response(JSON.stringify(stats()), { status: 200 })
    },
  })
  const got = await api.getStats()
  assert.deepEqual(urls, ["/api/stats"])
  assert.equal(got.graded_repos, 28)
})

const render = (props) =>
  renderToStaticMarkup(
    createElement(AnalyticsPage, {
      api: new HttpRepoHealthApi(),
      isLoading: false,
      onRetry() {},
      onOpenRepository() {},
      ...props,
    }),
  )

test("the page shows statistics, export links and the privacy note", () => {
  const html = render({ stats: stats() })
  assert.match(html, /Состояние открытых репозиториев/)
  assert.match(html, /href="\/api\/export\/grades\.csv"/)
  assert.match(html, /href="\/api\/export\/grades\.json"/)
  assert.match(html, /Приватные оценки в неё не попадают/)
  assert.match(html, /51\.2/)
  assert.match(html, /Нет CODEOWNERS/)
  assert.doesNotMatch(html, /ALGORITHMIC/)
  assert.match(html, /ни у одного репозитория оценка не выросла/)
})

test("the page lists most improved repositories", () => {
  const html = render({
    stats: stats({ most_improved: [{ org: "acme", name: "tool", score: 80, past_score: 60, change: 20 }] }),
  })
  assert.match(html, /acme\/tool/)
  assert.match(html, /60 → 80/)
  assert.match(html, /\+20/)
})

test("loading, error and empty states render without statistics", () => {
  assert.match(render({ isLoading: true }), /Считаем статистику/)
  const failed = render({ error: "Сервер не ответил вовремя.", isLoading: false })
  assert.match(failed, /Не удалось загрузить аналитику/)
  assert.match(failed, /Повторить/)
  assert.match(render({ stats: stats({ graded_repos: 0 }) }), /Пока нет оценённых репозиториев/)
})

test("a language below the minimum shows the explanation instead of a bar", () => {
  const html = render({ stats: stats({ languages: [] }) })
  assert.match(html, /не меньше 3 его репозиториев/)
})
