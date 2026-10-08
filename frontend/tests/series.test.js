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

const view = await vite.ssrLoadModule("/src/seriesView.ts")
const { HttpRepoHealthApi, ApiRequestError } = await vite.ssrLoadModule("/src/api/client.ts")
const { default: MetricChart } = await vite.ssrLoadModule("/src/components/MetricChart.tsx")
const { default: SeriesCard } = await vite.ssrLoadModule("/src/components/SeriesCard.tsx")
const { default: MetricsPanel } = await vite.ssrLoadModule("/src/components/MetricsPanel.tsx")
const { default: AnalyticsPage } = await vite.ssrLoadModule("/src/pages/AnalyticsPage.tsx")

test("the graphs the tabs ask for are exactly the series the Gateway serves", () => {
  // Mirrors the Gateway's series.Catalog (gateway-service/internal/series): a name added on one side only
  // would be a graph that always says "no such graph".
  assert.deepEqual(
    view.PUBLIC_SERIES.map((s) => s.name),
    ["gradings_per_day", "grading_avg_seconds"],
  )
  assert.deepEqual(
    view.ADMIN_SERIES.map((s) => s.name),
    [
      "http_rate",
      "http_latency_p95",
      "grpc_errors",
      "gradings_by_trigger",
      "grading_outcomes",
      "grading_p95_seconds",
      "limiter_rejections",
      "llm_requests",
      "batch_items",
      "admin_denied",
    ],
  )
  for (const info of [...view.PUBLIC_SERIES, ...view.ADMIN_SERIES]) {
    assert.ok(info.title && info.description, info.name)
    assert.doesNotMatch(`${info.title} ${info.description}`, /админк/i)
  }
})

test("axis ticks move in round steps, and counts in whole numbers", () => {
  assert.equal(view.niceStep(0), 1)
  assert.equal(view.niceStep(-3), 1)
  assert.equal(view.niceStep(NaN), 1)
  assert.equal(view.niceStep(1.2), 2)
  assert.equal(view.niceStep(2.2), 2.5)
  assert.equal(view.niceStep(3), 5)
  assert.equal(view.niceStep(7), 10)
  assert.equal(view.niceStep(41), 50)
  assert.equal(view.niceStep(0.034), 0.05)

  // Four steps cover the largest value; a count never gets a step below 1.
  assert.equal(view.tickStep("count", 4), 1) // ticks 0 1 2 3 4
  assert.equal(view.tickStep("count", 5), 2) // 0 2 4 6 8
  assert.equal(view.tickStep("count", 0.3), 1)
  assert.equal(view.tickStep("seconds", 0.3), 0.1)
  assert.equal(view.tickStep("seconds", 41), 20) // 41 / 4 = 10.25 is just over 10, so the next round step
})

test("values read in Russian style with their unit", () => {
  assert.equal(view.formatValue("count", 12), "12")
  assert.equal(view.formatValue("count", 2.5), "2,5")
  assert.equal(view.formatValue("count", 340.4), "340")
  assert.equal(view.formatValue("seconds", 0.25), "250 мс")
  assert.equal(view.formatValue("seconds", 4.26), "4,3 с")
  assert.equal(view.formatValue("seconds", 45), "45 с")
  assert.equal(view.formatValue("seconds", 150), "2,5 мин")
  assert.equal(view.formatValue("seconds", 7200), "2,0 ч")
  assert.equal(view.formatValue("per_second", 0.12), "0,12 /с")
  assert.equal(view.formatValue("per_second", 3.4), "3,4 /с")
  assert.equal(view.formatValue("per_second", 42), "42 /с")
  assert.equal(view.formatValue("per_second", 0), "0 /с")
  // Tiny rates keep distinct axis ticks instead of repeating one rounded value.
  assert.deepEqual([0, 0.002, 0.004, 0.006, 0.01].map((v) => view.formatValue("per_second", v)), ["0 /с", "0,002 /с", "0,004 /с", "0,006 /с", "0,010 /с"])
  assert.equal(view.formatValue("count", Infinity), "—")
})

test("legend text is Russian for known groupings and honest for unknown ones", () => {
  assert.equal(view.lineLabel("gradings_by_trigger", "batch", "x"), "пакетом")
  assert.equal(view.lineLabel("grading_outcomes", "error", "x"), "с ошибкой")
  assert.equal(view.lineLabel("admin_denied", "not_admin", "x"), "не администратор")
  assert.equal(view.lineLabel("http_rate", "5xx", "x"), "5xx")
  assert.equal(view.lineLabel("llm_requests", "brand_new", "x"), "brand_new")
  assert.equal(view.lineLabel("limiter_rejections", "", "Отказы по лимиту"), "Отказы по лимиту")
  // A label named like an Object property must not read the prototype.
  assert.equal(view.lineLabel("batch_items", "constructor", "x"), "constructor")
})

test("curves are placed by real time and scaled to a round maximum", () => {
  const geometry = { width: 100, height: 60, pad: { left: 10, right: 10, top: 10, bottom: 10 } }
  const model = view.chartModel(
    [
      { label: "a", points: [[1000, 0], [1100, 30], [1400, 10]] },
      { label: "b", points: [[1000, 5]] },
    ],
    geometry,
    "count",
  )

  assert.equal(model.yMax, 40)
  assert.equal(model.tMin, 1000)
  assert.equal(model.tMax, 1400)
  assert.deepEqual(model.yTicks.map((t) => t.value), [0, 10, 20, 30, 40])

  const [a] = model.lines
  assert.equal(a.coords[0].x, 10) // first time at the left edge
  assert.equal(a.coords[2].x, 90) // last time at the right edge
  assert.equal(a.coords[1].x, 10 + (100 / 400) * 80) // a gap is a gap: x follows time, not the index
  assert.equal(a.coords[0].y, 50) // zero on the baseline
  assert.ok(a.coords[1].y < a.coords[2].y) // 30 is higher than 10

  assert.equal(view.chartModel([], geometry), undefined)
  assert.equal(view.chartModel([{ label: "x", points: [] }], geometry), undefined)

  const single = view.chartModel([{ label: "x", points: [[5, 3]] }], geometry)
  assert.equal(single.lines[0].coords[0].x, 50) // one point sits in the middle
})

test("axis times: dates for daily graphs, clock for short ones", () => {
  const t = Date.UTC(2026, 9, 8, 15, 30) / 1000
  assert.match(view.formatAxisTime(t, 86400), /^\d\d\.\d\d$/)
  assert.match(view.formatAxisTime(t, 600), /^\d\d:\d\d$/)
  assert.match(view.formatAxisTime(t, 3600), /^\d\d\.\d\d \d\d:\d\d$/)
})

test("refused graph requests get Russian wording by who asks", () => {
  assert.match(view.seriesErrorMessage(503, "metrics_off", "admin"), /PROMETHEUS_URL/)
  assert.doesNotMatch(view.seriesErrorMessage(503, "metrics_off", "public"), /PROMETHEUS/)
  assert.match(view.seriesErrorMessage(503, "metrics_unavailable", "admin"), /Prometheus/)
  assert.match(view.seriesErrorMessage(403, "", "admin"), /администратор/)
  assert.equal(view.seriesErrorMessage(403, "", "public"), undefined)
  assert.match(view.seriesErrorMessage(404, "", "public"), /нет/)
  assert.equal(view.seriesErrorMessage(500, "", "admin"), undefined)
})

test("empty curves are dropped and an ungrouped curve is named by its graph", () => {
  const lines = view.chartLines("grading_outcomes", "Результаты оценок", [
    { label: "ok", points: [[1, 2]] },
    { label: "error", points: [] },
  ])
  assert.deepEqual(lines, [{ label: "успешно", points: [[1, 2]] }])
  assert.equal(view.chartLines("limiter_rejections", "Отказы", [{ label: "", points: [[1, 0]] }])[0].label, "Отказы")
})

test("graph calls use the public and admin routes and never send PromQL", async () => {
  const urls = []
  const api = new HttpRepoHealthApi({
    fetcher: async (url) => {
      urls.push(url)
      return new Response(JSON.stringify({ name: "x", unit: "count", step_seconds: 60, generated_at: "", series: [] }), { status: 200 })
    },
  })
  await api.getPublicSeries("gradings_per_day")
  await api.getAdminSeries("http_rate", "7d")
  await api.getAdminSeries("a/b", "1h")
  assert.deepEqual(urls, [
    "/api/stats/series/gradings_per_day",
    "/api/admin/metrics/http_rate?range=7d",
    "/api/admin/metrics/a%2Fb?range=1h",
  ])
})

test("a refused graph request keeps its reason and gets Russian wording", async () => {
  const api = (status, code) =>
    new HttpRepoHealthApi({ fetcher: async () => new Response(JSON.stringify({ error: "x", code }), { status }) })

  await assert.rejects(
    () => api(503, "metrics_off").getAdminSeries("http_rate", "24h"),
    (error) => error instanceof ApiRequestError && error.code === "metrics_off" && /PROMETHEUS_URL/.test(error.message),
  )
  await assert.rejects(
    () => api(503, "metrics_unavailable").getPublicSeries("gradings_per_day"),
    (error) => error.status === 503 && /недоступны/.test(error.message) && !/Prometheus/.test(error.message),
  )
  await assert.rejects(
    () => api(401, "").getAdminSeries("http_rate", "24h"),
    (error) => /администратор/.test(error.message),
  )
  await assert.rejects(
    () => new HttpRepoHealthApi({ fetcher: async () => Promise.reject(new TypeError("down")) }).getPublicSeries("x"),
    (error) => error.status === 0 && /подключение/.test(error.message),
  )
})

const chart = (props = {}) =>
  renderToStaticMarkup(
    createElement(MetricChart, {
      lines: [{ label: "успешно", points: [[1000, 2], [1600, 5]] }],
      unit: "count",
      stepSeconds: 600,
      label: "Результаты оценок",
      ...props,
    }),
  )

test("the chart is an accessible image, with a legend only when several curves share it", () => {
  const one = chart()
  assert.match(one, /role="img"/)
  assert.match(one, /aria-label="Результаты оценок"/)
  assert.match(one, /<polyline/)
  assert.doesNotMatch(one, /history-legend/)

  const two = chart({
    lines: [
      { label: "успешно", points: [[1000, 2], [1600, 5]] },
      { label: "с ошибкой", points: [[1000, 1]] },
    ],
  })
  assert.match(two, /history-legend/)
  assert.match(two, /с ошибкой/)
  assert.match(two, /<circle/) // the one-point curve is a dot

  assert.equal(chart({ lines: [] }), "")
})

test("a graph card starts loading and has a title and description", () => {
  const api = new HttpRepoHealthApi({ fetcher: async () => new Response("{}", { status: 200 }) })
  const html = renderToStaticMarkup(
    createElement(SeriesCard, {
      info: view.PUBLIC_SERIES[0],
      load: () => api.getPublicSeries("gradings_per_day"),
      loadKey: "k",
      emptyText: "пусто",
    }),
  )
  assert.match(html, /Оценок в сутки/)
  assert.match(html, /Загружаем/)
})

test("the admin panel lists every graph with a period choice and says what is not on them", () => {
  const api = new HttpRepoHealthApi({ fetcher: async () => new Response("{}", { status: 200 }) })
  const html = renderToStaticMarkup(createElement(MetricsPanel, { api }))
  for (const info of view.ADMIN_SERIES) assert.ok(html.includes(info.title), info.title)
  assert.match(html, /Период/)
  assert.match(html, /<option value="24h" selected="">Сутки/)
  assert.match(html, /ни репозиториев, ни пользователей/)
})

test("the Analytics tab shows the public service graphs even before there are stats", () => {
  const api = new HttpRepoHealthApi({ fetcher: async () => new Response("{}", { status: 200 }) })
  const html = renderToStaticMarkup(
    createElement(AnalyticsPage, { api, isLoading: false, onRetry() {}, onOpenRepository() {} }),
  )
  assert.match(html, /Как идут оценки/)
  assert.match(html, /только по публичным репозиториям/)
  assert.match(html, /Оценок в сутки/)
  assert.match(html, /Среднее время оценки/)
  assert.doesNotMatch(html, /Запросы к сайту/) // admin graphs never appear on the public tab
})
