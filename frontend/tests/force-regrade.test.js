import assert from "node:assert/strict"
import { after, test } from "node:test"
import { createElement } from "react"
import { renderToStaticMarkup } from "react-dom/server"
import { createServer } from "vite"

const vite = await createServer({
  optimizeDeps: { noDiscovery: true },
  server: { middlewareMode: true, watch: null, hmr: false },
  appType: "custom",
})
after(() => vite.close())

const { isGradeStale, STALE_GRADE_DAYS } = await vite.ssrLoadModule("/src/metrics.ts")
const { HttpRepoHealthApi, ApiRequestError } = await vite.ssrLoadModule("/src/api/client.ts")
const { repositoryFromRanking } = await vite.ssrLoadModule("/src/api/mapper.ts")
const { default: Overview } = await vite.ssrLoadModule("/src/pages/RepositoryOverviewPage.tsx")
const { default: RankingPage } = await vite.ssrLoadModule("/src/pages/RankingPage.tsx")
const { reportContent } = await vite.ssrLoadModule("/src/reports/downloadReport.ts")
const { actionLabel, USER_ACTION_FILTERS } = await vite.ssrLoadModule("/src/auditView.ts")

const DAY = 24 * 60 * 60 * 1000
const daysAgo = (days) => new Date(Date.now() - days * DAY).toISOString()

test("a grade is stale only when it is more than a week old", () => {
  const now = Date.parse("2026-10-10T12:00:00Z")
  assert.equal(STALE_GRADE_DAYS, 7)
  assert.equal(isGradeStale("2026-10-03T12:00:00Z", now), false, "exactly seven days is not stale yet")
  assert.equal(isGradeStale("2026-10-03T11:59:00Z", now), true)
  assert.equal(isGradeStale("2026-10-09T00:00:00Z", now), false)
  assert.equal(isGradeStale(undefined, now), false)
  assert.equal(isGradeStale("not a date", now), false)
})

test("force regrade posts to the regrade route with the guard header and the token", async () => {
  const calls = []
  const api = new HttpRepoHealthApi({
    fetcher: async (url, init) => {
      calls.push({ url, method: init.method, headers: init.headers })
      return new Response(JSON.stringify({ status: "PENDING" }), { status: 202 })
    },
  })
  const response = await api.forceRegrade("my org", "repo", "pat-1")
  assert.equal(response.status, "PENDING")
  assert.equal(calls[0].url, "/api/grading/my%20org/repo/regrade")
  assert.equal(calls[0].method, "POST")
  assert.equal(calls[0].headers["X-Admin-Request"], "1")
  assert.equal(calls[0].headers.PAT, "pat-1")
})

test("force regrade refusals read as Russian: sign in, cooldown with minutes, plain rate limit", async () => {
  const answer = (status, headers = {}) =>
    new HttpRepoHealthApi({
      fetcher: async () => new Response(JSON.stringify({ error: "x" }), { status, headers }),
    })
  await assert.rejects(answer(401).forceRegrade("o", "r"), (error) => error.status === 401 && /входа через Яндекс/.test(error.message))
  await assert.rejects(
    answer(429, { "Retry-After": "1500" }).forceRegrade("o", "r"),
    (error) => error instanceof ApiRequestError && error.status === 429 && /через 25 мин/.test(error.message),
  )
  await assert.rejects(answer(429).forceRegrade("o", "r"), (error) => error.status === 429 && /Подождите минуту/.test(error.message))
})

test("ranking entries carry graded_at", () => {
  const entry = { place: 1, sourcecraft_uuid: "u", name: "r", web_url: "https://sourcecraft.dev/o/r", public_score: 50, graded_at: "2026-10-01T00:00:00Z" }
  assert.equal(repositoryFromRanking(entry).gradedAt, "2026-10-01T00:00:00Z")
  assert.equal(repositoryFromRanking({ ...entry, graded_at: undefined }).gradedAt, undefined)
})

const repository = (over = {}) => ({
  id: "x",
  orgSlug: "o",
  repoSlug: "r",
  name: "r",
  fullName: "o/r",
  language: "Go",
  healthScore: 70,
  lastActivityDaysAgo: 1,
  analysisMode: "public",
  categoryScores: {},
  gradedAt: daysAgo(1),
  ...over,
})
const analysis = { isRunning: false, aiLoading: false, run() {}, loadAiSummary() {} }
const overview = (props) => renderToStaticMarkup(createElement(Overview, { analysis, onBackToRanking() {}, ...props }))

test("the overview shows the force toggle to signed-in users only", () => {
  assert.doesNotMatch(overview({ repository: repository() }), /Принудительно/)
  const html = overview({ repository: repository(), canForceRegrade: true })
  assert.match(html, /force-regrade-toggle/)
  assert.match(html, /Принудительно/)
})

test("the overview marks a grade older than a week as outdated", () => {
  const fresh = overview({ repository: repository() })
  assert.doesNotMatch(fresh, /Устарела/)
  const stale = overview({ repository: repository({ gradedAt: daysAgo(9) }) })
  assert.match(stale, /Устарела/)
  assert.match(stale, /более 7 дней назад/)
})

test("a forced run that never got a newer grade says so", () => {
  const html = overview({
    repository: repository(),
    analysis: { ...analysis, rerun: { forced: true, gradeChanged: false, stillPending: false } },
  })
  assert.match(html, /Переоценка не завершилась/)
})

test("the ranking marks outdated grades", () => {
  const html = renderToStaticMarkup(
    createElement(RankingPage, {
      repositories: [repository({ id: "a", place: 1, gradedAt: daysAgo(10) }), repository({ id: "b", place: 2, fullName: "o/b" })],
      onSelectRepository() {},
    }),
  )
  assert.equal(html.match(/>Устарела</g)?.length, 1)
})

test("reports note an outdated grade", () => {
  assert.match(reportContent(repository({ gradedAt: daysAgo(10) }), []).gradedAt, /оценка устарела: старше 7 дней/)
  assert.doesNotMatch(reportContent(repository(), []).gradedAt, /устарела/)
})

test("forced regrades have an audit label and filter", () => {
  assert.equal(actionLabel("grading.force"), "Принудительная переоценка репозитория")
  assert.ok(USER_ACTION_FILTERS.some((filter) => filter.value === "grading.force"))
})
