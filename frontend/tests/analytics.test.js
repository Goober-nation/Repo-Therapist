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

const { analyticsFromResponse } = await vite.ssrLoadModule("/src/api/mapper.ts")
const { default: AnalyticsSection } = await vite.ssrLoadModule("/src/components/AnalyticsSection.tsx")

const area = (path, over = {}) => ({ path, files: 3, lines: 40, top_owner: "Ann", top_owner_share: 0.8, owners: 2, ...over })

const payload = (over = {}) => ({
  status: "FINISHED",
  analytics: {
    version: 3,
    window_days: 180,
    bus_factor: { status: "ok", value: 2, commits: 50, authors: 4, top_authors: [{ name: "Ann", commits: 30, share: 0.6 }] },
    reviews: {
      status: "ok",
      sampled: 20,
      merged: 10,
      unreviewed: 4,
      unreviewed_share: 0.4,
      median_merge_hours: 6,
      bottlenecks: { status: "ok", reviewed: 12, open: 3, waiting: 1, waiting_days: 7, median_first_review_hours: 5.5 },
    },
    ownership: {
      status: "ok",
      areas: [area("src/")],
      subareas: [area("src/api/")],
      files: [{ path: "src/api/handler.go", lines: 70, top_owner: "Ann", top_owner_share: 0.86, owners: 2 }],
      codeowners: { present: false, rules: 0, covered_share: null },
    },
    ...over,
  },
  graded_at: "2026-10-04T10:00:00Z",
})

test("analytics map subareas, top files and review bottlenecks", () => {
  const got = analyticsFromResponse(payload())

  assert.equal(got.ownership.subareas[0].path, "src/api/")
  assert.deepEqual(got.ownership.files[0], { path: "src/api/handler.go", lines: 70, topOwner: "Ann", topOwnerShare: 0.86, owners: 2 })
  assert.deepEqual(got.reviews.bottlenecks, {
    available: true,
    reason: "unavailable",
    reviewed: 12,
    open: 3,
    waiting: 1,
    waitingDays: 7,
    medianFirstReviewHours: 5.5,
  })
})

test("analytics stored before version 3 show the new parts as unavailable, never as zeros", () => {
  const old = payload()
  delete old.analytics.ownership.subareas
  delete old.analytics.ownership.files
  delete old.analytics.reviews.bottlenecks

  const got = analyticsFromResponse(old)
  assert.deepEqual(got.ownership.subareas, [])
  assert.deepEqual(got.ownership.files, [])
  assert.equal(got.reviews.bottlenecks.available, false)
  assert.equal("medianFirstReviewHours" in got.reviews.bottlenecks, false)
})

test("bottlenecks without a median or with bad numbers stay without a median", () => {
  const noMedian = payload()
  noMedian.analytics.reviews.bottlenecks = { status: "ok", reviewed: 1, open: 2, waiting: 2, waiting_days: 7, median_first_review_hours: null }
  assert.equal("medianFirstReviewHours" in analyticsFromResponse(noMedian).reviews.bottlenecks, false)

  const bad = payload()
  bad.analytics.reviews.bottlenecks = { status: "ok", median_first_review_hours: -3, reviewed: "x" }
  const got = analyticsFromResponse(bad).reviews.bottlenecks
  assert.equal("medianFirstReviewHours" in got, false)
  assert.equal(got.reviewed, 0)
})

const render = (response) => renderToStaticMarkup(createElement(AnalyticsSection, { analytics: analyticsFromResponse(response) }))

test("the owners card offers directories, subdirectories and files, and shows directories first", () => {
  const html = render(payload())
  assert.match(html, /aria-label="Уровень карты владельцев"/)
  assert.match(html, />Каталоги</)
  assert.match(html, />Подкаталоги</)
  assert.match(html, />Самые изменяемые файлы</)
  assert.match(html, /<code>src\/<\/code>/, "the directory view is the default")
  assert.doesNotMatch(html, /handler\.go/, "files appear only after switching the tab")
  assert.match(html, /aria-pressed="true"[^>]*>Каталоги/)
})

test("without subdirectories and files the owners card has no tabs", () => {
  const old = payload()
  old.analytics.ownership.subareas = []
  old.analytics.ownership.files = []
  const html = render(old)
  assert.doesNotMatch(html, /analytics-tabs/)
  assert.match(html, /<code>src\/<\/code>/)
})

test("the review card shows the median first-review time and the waiting pull requests", () => {
  const html = render(payload())
  assert.match(html, /Где ревью ждут/)
  assert.match(html, /Медианное время до первого ревью/)
  assert.match(html, /6 ч/, "5.5 hours rounds to 6 h")
  assert.match(html, /по 12 pull request/)
  assert.match(html, /дольше 7 дн\./)
  assert.match(html, /<strong>1<\/strong> из 3 открытых/)
})

test("bottlenecks can stand alone when there are too few merged pull requests", () => {
  const only = payload()
  only.analytics.reviews = {
    status: "no_data",
    reason: "few_merged",
    sampled: 3,
    merged: 0,
    unreviewed: 0,
    bottlenecks: { status: "ok", reviewed: 0, open: 3, waiting: 2, waiting_days: 7, median_first_review_hours: null },
  }
  const html = render(only)
  assert.match(html, /Где ревью ждут/)
  assert.match(html, /Время до первого ревью не посчитано/)
  assert.doesNotMatch(html, /Нет данных: среди последних pull request/, "the merged-based message is not repeated when open ones are reported")
})

test("an imported history says no data for both review parts", () => {
  const imported = payload()
  imported.analytics.reviews = {
    status: "no_data",
    reason: "imported",
    sampled: 10,
    merged: 0,
    unreviewed: 0,
    bottlenecks: { status: "no_data", reason: "imported", reviewed: 0, open: 0, waiting: 0, waiting_days: 7, median_first_review_hours: null },
  }
  const html = render(imported)
  assert.match(html, /похоже на импорт истории/)
  assert.doesNotMatch(html, /Где ревью ждут/)
})
