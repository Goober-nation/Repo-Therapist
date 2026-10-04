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

const { repositoryFromRanking, historyFromResponse, positionFromResponse } = await vite.ssrLoadModule("/src/api/mapper.ts")
const { parseRoute, routePath } = await vite.ssrLoadModule("/src/route.ts")
const { default: HistoryChart } = await vite.ssrLoadModule("/src/components/HistoryChart.tsx")
const { default: ScoreHistorySection } = await vite.ssrLoadModule("/src/components/ScoreHistorySection.tsx")
const { default: PositionCard, pointsLabel } = await vite.ssrLoadModule("/src/components/PositionCard.tsx")
const { default: ComparePage, parseRepoInput } = await vite.ssrLoadModule("/src/pages/ComparePage.tsx")
const { default: RankingPage, DEFAULT_RANKING_QUERY, rankingFilters, hasActiveFilters } = await vite.ssrLoadModule("/src/pages/RankingPage.tsx")
const { HttpRepoHealthApi } = await vite.ssrLoadModule("/src/api/client.ts")

const entry = (extra = {}) => ({
  place: 1,
  sourcecraft_uuid: "0192b0a8-0000-7000-8000-000000000001",
  name: "demo",
  web_url: "https://sourcecraft.dev/org/demo",
  public_score: 80,
  ...extra,
})

test("ranking entries carry whole-number score and place changes, anything else means none", () => {
  const up = repositoryFromRanking(entry({ score_change: 4, place_change: -2 }))
  assert.equal(up.scoreChange, 4)
  assert.equal(up.placeChange, -2)

  const zero = repositoryFromRanking(entry({ score_change: 0, place_change: 0 }))
  assert.equal(zero.scoreChange, 0)
  assert.equal(zero.placeChange, 0)

  for (const bad of [null, undefined, "3", 1.5, Number.NaN]) {
    const row = repositoryFromRanking(entry({ score_change: bad, place_change: bad }))
    assert.equal("scoreChange" in row, false, `score_change ${String(bad)} must be dropped`)
    assert.equal("placeChange" in row, false)
  }
})

test("history keeps valid points oldest first and turns no-data categories into null", () => {
  const points = historyFromResponse({
    status: "FINISHED",
    points: [
      { graded_at: "2026-10-04T10:00:00Z", score: 66, categories: [{ id: "activity", score: 70, no_data: false }, { id: "issues", score: null, no_data: true }], low_confidence: true },
      { graded_at: "2026-09-28T10:00:00Z", score: 60.4, categories: [{ id: "activity", score: 55 }] },
      { graded_at: "not a date", score: 50 },
      { graded_at: "2026-10-01T10:00:00Z", score: 140 },
      { graded_at: "2026-10-02T10:00:00Z" },
    ],
  })

  assert.deepEqual(points.map((p) => p.score), [60, 66], "invalid dates and scores are dropped, the rest sorted by time")
  assert.deepEqual(points[1].categories, [{ id: "activity", score: 70 }, { id: "issues", score: null }])
  assert.equal(points[1].lowConfidence, true)
  assert.equal(points[0].lowConfidence, null)
  assert.deepEqual(historyFromResponse({ status: "NO_DATA", points: [] }), [])
  assert.deepEqual(historyFromResponse(undefined), [])
  assert.deepEqual(historyFromResponse({ status: "FINISHED", points: "x" }), [])
})

test("position explanation maps neighbours and medians, and rejects malformed answers", () => {
  const good = positionFromResponse({
    status: "FINISHED",
    place: 3,
    total: 5,
    score: 70,
    above: { place: 2, org: "o", name: "r2", score: 80, points: 10 },
    below: null,
    categories: [
      { id: "activity", score: 60, top10_median: 65, gap: -5 },
      { id: "documentation", score: 80, top10_median: null, gap: null },
    ],
    weakest_category: "activity",
  })

  assert.equal(good.place, 3)
  assert.equal(good.above.name, "r2")
  assert.equal("below" in good, false)
  assert.deepEqual(good.categories[0], { id: "activity", score: 60, top10Median: 65, gap: -5 })
  assert.deepEqual(good.categories[1], { id: "documentation", score: 80 })
  assert.equal(good.weakestCategory, "activity")

  assert.equal(positionFromResponse({ status: "NO_DATA" }), null)
  assert.equal(positionFromResponse({ status: "FINISHED", place: 9, total: 5, score: 70 }), null, "place beyond total")
  assert.equal(positionFromResponse({ status: "FINISHED", place: 0, total: 5, score: 70 }), null)
  assert.equal(positionFromResponse({ status: "FINISHED", place: 1, total: 5 }), null, "no score")
})

test("compare route parses and round-trips", () => {
  const route = parseRoute("/compare/org%20a/repo-a/org-b/repo%2Fb")
  assert.deepEqual(route, { page: "compare", a: { org: "org a", repo: "repo-a" }, b: { org: "org-b", repo: "repo/b" } })
  assert.equal(routePath(route), "/compare/org%20a/repo-a/org-b/repo%2Fb")
  assert.equal(parseRoute("/compare/only/one").page, "ranking", "an incomplete compare path falls back to the ranking")
})

test("repo input accepts org/repo and SourceCraft links only", () => {
  assert.deepEqual(parseRepoInput("org/repo"), { org: "org", repo: "repo" })
  assert.deepEqual(parseRepoInput(" org/repo.git/ "), { org: "org", repo: "repo" })
  assert.deepEqual(parseRepoInput("https://sourcecraft.dev/org/repo"), { org: "org", repo: "repo" })
  for (const bad of ["", "org", "a/b/c", "/repo", "https://example.com/org/repo"]) {
    assert.equal(parseRepoInput(bad), undefined, `${bad} must be rejected`)
  }
})

test("the history chart draws lines for two points, a lone dot for one, nothing without points", () => {
  const two = renderToStaticMarkup(
    createElement(HistoryChart, {
      label: "chart",
      series: [{ id: "s", label: "Score", tone: 5, points: [{ at: "2026-09-28T10:00:00Z", value: 60 }, { at: "2026-10-04T10:00:00Z", value: 70 }] }],
    }),
  )
  assert.match(two, /<polyline/)
  assert.equal((two.match(/<circle/g) ?? []).length, 2)
  assert.match(two, /<title>Score: 70 · /)

  const one = renderToStaticMarkup(
    createElement(HistoryChart, { label: "chart", series: [{ id: "s", label: "Score", tone: 5, points: [{ at: "2026-10-04T10:00:00Z", value: 70 }] }] }),
  )
  assert.doesNotMatch(one, /<polyline/)
  assert.equal((one.match(/<circle/g) ?? []).length, 1)

  assert.equal(renderToStaticMarkup(createElement(HistoryChart, { label: "chart", series: [] })), "")
})

test("the history section shows loading, empty, single-point and full states", () => {
  const render = (points) => renderToStaticMarkup(createElement(ScoreHistorySection, { points }))
  assert.match(render(undefined), /Загружаем историю/)
  assert.match(render(null), /История пока пуста/)
  assert.match(render([]), /История пока пуста/)

  const point = (gradedAt, score) => ({ gradedAt, score, categories: [{ id: "activity", score: 50 }, { id: "issues", score: null }], lowConfidence: null })
  const single = render([point("2026-10-04T10:00:00Z", 70)])
  assert.match(single, /Пока одна оценка/)

  const full = render([point("2026-09-28T10:00:00Z", 60), point("2026-10-04T10:00:00Z", 70)])
  assert.match(full, /<polyline/)
  assert.match(full, /\+10/, "the latest change is shown with a sign")
  assert.match(full, /Activity|Активность/)
  assert.doesNotMatch(full, />Issues<\/span>|Задачи<\/span>/, "a category with no data in any point is not offered")
})

test("the position card explains neighbours, ties, medians and the weakest category", () => {
  assert.equal(pointsLabel(1), "1 балл")
  assert.equal(pointsLabel(2), "2 балла")
  assert.equal(pointsLabel(5), "5 баллов")
  assert.equal(pointsLabel(11), "11 баллов")
  assert.equal(pointsLabel(22), "22 балла")

  const noop = () => {}
  const render = (position) => renderToStaticMarkup(createElement(PositionCard, { position, onOpenRepository: noop }))
  assert.match(render(undefined), /Считаем место/)
  assert.match(render(null), /после первой публичной оценки/)

  const html = render({
    place: 3,
    total: 5,
    score: 70,
    above: { place: 2, org: "o", name: "r2", score: 80, points: 10 },
    below: { place: 4, org: "o", name: "r4", score: 70, points: 0 },
    categories: [
      { id: "activity", score: 60, top10Median: 65, gap: -5 },
      { id: "documentation", score: 80, top10Median: 75, gap: 5 },
    ],
    weakestCategory: "activity",
  })
  assert.match(html, /3-е место из 5/)
  assert.match(html, /10 баллов/)
  assert.match(html, /менее свежей активности/, "a tie names the activity tie-break")
  assert.match(html, /−5/)
  assert.match(html, /\+5/)
  assert.match(html, /Сильнее всего отстаёт/)

  const alone = render({ place: 1, total: 1, score: 50, categories: [{ id: "activity", score: 50 }] })
  assert.match(alone, /нет других репозиториев/)
  assert.doesNotMatch(alone, /Сильнее всего отстаёт|Ни одна категория/, "no medians, no verdict")
})

const side = (over = {}) => ({
  org: "o",
  repo: "a",
  state: "ready",
  name: "a",
  score: 75,
  gradedAt: "2026-10-04T10:00:00Z",
  categories: [{ id: "activity", score: 80 }, { id: "issues", score: null }],
  history: [{ gradedAt: "2026-10-01T10:00:00Z", score: 70, categories: [], lowConfidence: null }],
  ...over,
})

test("the compare page shows differences, no-data cells, errors and the chart", () => {
  const noop = () => {}
  const render = (left, right) =>
    renderToStaticMarkup(createElement(ComparePage, { left, right, onChange: noop, onOpenRepository: noop, onBack: noop }))

  const html = render(side(), side({ repo: "b", name: "b", score: 60, history: [], categories: [{ id: "activity", score: 50 }, { id: "issues", score: 40 }] }))
  assert.match(html, /\+15/, "score difference A − B")
  assert.match(html, /\+30/, "category difference")
  assert.match(html, /Нет данных/, "a category without data is not a number")
  assert.match(html, /История есть только у одного из репозиториев/)

  const broken = render(side(), side({ repo: "b", state: "error", error: "Репозиторий не найден среди публичных." }))
  assert.match(broken, /Репозиторий не найден среди публичных/)
  assert.doesNotMatch(broken, /compare-table/, "no table until both sides are ready")

  const loading = render(side({ state: "loading" }), side({ repo: "b" }))
  assert.match(loading, /Загружаем o\/a/)
})

test("the ranking shows place and score changes, the new tag, and the compare bar", () => {
  const noop = () => {}
  const repo = (id, over) => ({
    id,
    orgSlug: "o",
    repoSlug: id,
    name: id,
    fullName: `o/${id}`,
    language: "Go",
    healthScore: 80,
    lastActivityDaysAgo: 1,
    categoryScores: {},
    ...over,
  })
  const props = {
    repositories: [
      repo("up", { place: 1, placeChange: 3, scoreChange: 4 }),
      repo("down", { place: 2, placeChange: -2, scoreChange: -5 }),
      repo("fresh", { place: 3 }),
      repo("flat", { place: 4, placeChange: 0, scoreChange: 0 }),
    ],
    onSelectRepository: noop,
    dynamicsDays: 7,
  }

  const html = renderToStaticMarkup(createElement(RankingPage, props))
  assert.match(html, /▲3/)
  assert.match(html, /▼2/)
  assert.match(html, /\+4/)
  assert.match(html, /−5/)
  assert.match(html, /новый/)
  assert.match(html, /за 7 дн\./)
  assert.doesNotMatch(html, /compare-bar/, "no bar until a repository is ticked")

  const one = renderToStaticMarkup(createElement(RankingPage, { ...props, compareSelection: [props.repositories[0]] }))
  assert.match(one, /выберите ещё один репозиторий/)
  assert.match(one, /<button[^>]*disabled[^>]*>Сравнить<\/button>/, "compare needs exactly two")

  const two = renderToStaticMarkup(createElement(RankingPage, { ...props, compareSelection: [props.repositories[0], props.repositories[1]] }))
  assert.doesNotMatch(two, /<button[^>]*disabled[^>]*>Сравнить<\/button>/)
  assert.match(two, /type="checkbox"[^>]*disabled/, "a third repository cannot be ticked")

  const without = renderToStaticMarkup(createElement(RankingPage, { ...props, dynamicsDays: undefined }))
  assert.doesNotMatch(without, /новый/, "no 'new' tag when the backend sent no window")
})

test("the ranking request carries only the fact filters that are set", async () => {
  const urls = []
  const api = new HttpRepoHealthApi({
    fetcher: async (url) => {
      urls.push(url)
      return Response.json({ entries: [], total_count: 0, page: 1, page_size: 100 })
    },
  })

  await api.getRanking(1, "score", "", false, { size: "medium", ci: true, security: false, topic: "  REST API  " })
  await api.getRanking(1, "score", "Go", true, { ci: false })
  await api.getRanking(1, "score", "", false, {})
  await api.getRanking(1, "score", "", false, { topic: "   " })

  assert.deepEqual(urls, [
    "/api/ranking?page=1&page_size=100&sortby=score&size=medium&ci=true&security=false&topic=REST+API",
    "/api/ranking?page=1&page_size=100&sortby=score&filter=Go&hide_low_confidence=true&ci=false",
    "/api/ranking?page=1&page_size=100&sortby=score",
    "/api/ranking?page=1&page_size=100&sortby=score",
  ])
})

test("the query's yes/no filters become real booleans and unset ones stay absent", () => {
  assert.deepEqual(rankingFilters(DEFAULT_RANKING_QUERY), {})
  assert.deepEqual(rankingFilters({ ...DEFAULT_RANKING_QUERY, size: "large", ci: "yes", security: "no", topic: " api " }), {
    size: "large",
    ci: true,
    security: false,
    topic: "api",
  })
  // "no" is a real value (false), not the same as not filtering.
  assert.deepEqual(rankingFilters({ ...DEFAULT_RANKING_QUERY, ci: "no" }), { ci: false })
  // Legacy queries without the new fields mean no filters.
  assert.deepEqual(rankingFilters({ page: 1, sort: "score", language: "", hideLowConfidence: false }), {})

  assert.equal(hasActiveFilters(DEFAULT_RANKING_QUERY), false)
  for (const change of [{ language: "Go" }, { hideLowConfidence: true }, { size: "small" }, { ci: "no" }, { security: "yes" }, { topic: "x" }]) {
    assert.equal(hasActiveFilters({ ...DEFAULT_RANKING_QUERY, ...change }), true, JSON.stringify(change))
  }
  assert.equal(hasActiveFilters({ ...DEFAULT_RANKING_QUERY, topic: "   " }), false, "a blank topic is no filter")
})

test("the ranking offers the fact filters, explains them, and words the empty result for filters", () => {
  const noop = () => {}
  const base = { repositories: [], onSelectRepository: noop }

  const plain = renderToStaticMarkup(createElement(RankingPage, { ...base, query: DEFAULT_RANKING_QUERY }))
  for (const label of ["Размер", "CI", "Политика безопасности", "Тематика"]) {
    assert.match(plain, new RegExp(label), label)
  }
  assert.doesNotMatch(plain, /смотрят на сам репозиторий/, "the explanation appears only while a fact filter is on")

  const filtered = renderToStaticMarkup(createElement(RankingPage, { ...base, query: { ...DEFAULT_RANKING_QUERY, ci: "yes", size: "small", topic: "api" } }))
  assert.match(filtered, /смотрят на сам репозиторий/)
  assert.match(filtered, /Нет репозиториев с такими фильтрами/)
  assert.match(filtered, /Ослабьте фильтры/)
  assert.doesNotMatch(filtered, /после расчёта их рейтинга/, "the 'ranking is still empty' text is wrong when filters caused the empty result")
  assert.match(filtered, /Сбросить фильтры/)
  assert.match(filtered, /value="api"/, "the topic field shows the applied text")
  assert.match(filtered, /<option value="yes" selected="">Есть конфиг<\/option>/)

  const empty = renderToStaticMarkup(createElement(RankingPage, { ...base, query: DEFAULT_RANKING_QUERY }))
  assert.match(empty, /Пока нет репозиториев в общем рейтинге/)
})
