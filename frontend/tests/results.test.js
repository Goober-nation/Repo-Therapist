import assert from "node:assert/strict"
import { after, test } from "node:test"
import { createElement } from "react"
import { renderToStaticMarkup } from "react-dom/server"
import { createServer } from "vite"

// Use the application's existing TS/TSX transformer; no separate test compiler.
const vite = await createServer({
  server: { middlewareMode: true, watch: null, hmr: false },
  appType: "custom",
})
after(() => vite.close())
const {
  normalizeCategoryScores,
  mergeGradingIntoRepository,
  repositoryFromSourceCraft,
} = await vite.ssrLoadModule("/src/api/mapper.ts")
const { default: AnalysisResults } = await vite.ssrLoadModule(
  "/src/components/AnalysisResults.tsx",
)
const { default: Overview } = await vite.ssrLoadModule(
  "/src/pages/RepositoryOverviewPage.tsx",
)
const { default: Ranking } = await vite.ssrLoadModule(
  "/src/pages/RankingPage.tsx",
)
const { default: Security } = await vite.ssrLoadModule(
  "/src/pages/SecurityPage.tsx",
)
const { buildReport } = await vite.ssrLoadModule("/src/mocks/downloadReport.ts")
const { getCategoryScore } = await vite.ssrLoadModule("/src/metrics.ts")
const { HttpRepoHealthApi } = await vite.ssrLoadModule("/src/api/client.ts")
const { DEMO_REPOSITORIES } = await vite.ssrLoadModule("/src/mocks/demoData.ts")
const { MockRepositoryDataSource } = await vite.ssrLoadModule(
  "/src/data/repositoryData.ts",
)

const baseline = { ...DEMO_REPOSITORIES[0], id: "test-repository" }
const render = (component, props) =>
  renderToStaticMarkup(createElement(component, props))
const results = (categories, status) =>
  render(AnalysisResults, { categories, status })

test("HTTP client preserves the browser fetch receiver and passes dynamic JSON to the mapper", async (context) => {
  context.mock.method(globalThis, "fetch", async function (url) {
    assert.equal(this, globalThis, "Browser fetch requires the Window receiver")
    assert.equal(url, "/api/grade/example/repository")
    return new Response(
      JSON.stringify({ status: "FINISHED", criteria: { Мяу: { score: 84 } } }),
      {
        headers: { "Content-Type": "application/json" },
      },
    )
  })
  const api = new HttpRepoHealthApi({ baseUrl: "/api" })
  const response = await api.getGrading("example", "repository")
  assert.equal(
    mergeGradingIntoRepository(baseline, response).categoryScores["Мяу"].score,
    84,
  )
})

test("new and renamed categories reach both the overview and downloaded report", () => {
  const repository = mergeGradingIntoRepository(baseline, {
    status: "FINISHED",
    criteria: {
      Мяу: { score: 72, description: "Описание с бэка" },
      "Architecture v2": 93,
    },
  })
  assert.deepEqual(Object.keys(repository.categoryScores), [
    "Мяу",
    "Architecture v2",
  ])
  const html = render(Overview, {
    repository,
    isAnalysisRunning: false,
    analysisRefreshed: false,
    onOpenSecurity() {},
    onRunAnalysis() {},
  })
  assert.match(html, /Мяу: 72 из 100/)
  assert.match(html, /Architecture v2: 93 из 100/)
  assert.match(html, /Описание с бэка/)
  assert.doesNotMatch(html, /Активность проекта по истории изменений/)
  const report = buildReport(repository)
  assert.match(report, /Мяу: 72\/100/)
  assert.match(report, /Architecture v2: 93\/100/)
  assert.doesNotMatch(report, /undefined|Безопасность:/)
})

test("the documented array shape and object shape produce the same categories", () => {
  const object = {
    Мяу: 50,
    custom: { score: 81, name: "Произвольный критерий", trend: -2 },
  }
  const array = [{ Мяу: 50 }, { custom: object.custom }]
  assert.deepEqual(
    normalizeCategoryScores(array),
    normalizeCategoryScores(object),
  )
  assert.match(
    results(normalizeCategoryScores(array)),
    /Произвольный критерий: 81 из 100/,
  )
})

test("zero is a valid score; null, invalid types and out-of-range numbers are unavailable", () => {
  const categories = normalizeCategoryScores({
    zero: 0,
    max: 100,
    fractional: 85.6,
    absent: null,
    empty: {},
    text: "75",
    boolean: false,
    array: [70],
    invalid: NaN,
    infinite: Infinity,
    negative: -1,
    tooLarge: 101,
  })
  assert.equal(categories.zero.score, 0)
  assert.equal(categories.max.score, 100)
  assert.equal(categories.fractional.score, 86)
  for (const name of [
    "absent",
    "empty",
    "text",
    "boolean",
    "array",
    "invalid",
    "infinite",
    "negative",
    "tooLarge",
  ]) {
    assert.equal(categories[name].score, null, name)
  }
  const html = results(categories)
  assert.equal((html.match(/role="progressbar"/g) ?? []).length, 3)
  assert.match(html, /zero: 0 из 100/)
  assert.match(html, /Нет данных/)
  assert.match(
    buildReport({ ...baseline, categoryScores: categories }),
    /absent: Нет данных/,
  )
})

test("malformed containers cannot crash the results mapper", () => {
  for (const input of [undefined, null, true, 42, "incorrect", []]) {
    assert.deepEqual(normalizeCategoryScores(input), {})
  }
  assert.deepEqual(
    Object.keys(
      normalizeCategoryScores([null, 3, [], "bad", { newCategory: 80 }]),
    ),
    ["newCategory"],
  )
})

test("category keys are preserved without alias collisions or prototype special cases", () => {
  const input = JSON.parse(
    '{"__proto__":70,"constructor":0,"toString":80,"code":40,"code_health":60}',
  )
  const categories = normalizeCategoryScores(input)
  assert.deepEqual(Object.keys(categories), Object.keys(input))
  assert.equal(categories.__proto__.score, 70)
  assert.equal(categories.constructor.score, 0)
  assert.match(results(categories), /__proto__: 70 из 100/)
})

test("duplicate keys in the array use the last supplied result", () => {
  assert.equal(
    normalizeCategoryScores([{ custom: 50 }, { custom: 80 }]).custom.score,
    80,
  )
})

test("a completed response replaces removed categories and clears absent results", () => {
  for (const payload of [
    { category_scores: {} },
    { category_scores: [] },
    { category_scores: null },
    {},
  ]) {
    const repository = mergeGradingIntoRepository(baseline, {
      status: "FINISHED",
      ...payload,
    })
    assert.deepEqual(repository.categoryScores, {})
    assert.match(
      results(repository.categoryScores, repository.gradeStatus),
      /Оценки по категориям отсутствуют/,
    )
  }
})

test("pending without scores retains the previous result, explicit empty scores clear it", () => {
  assert.deepEqual(
    mergeGradingIntoRepository(baseline, { status: "PENDING" }).categoryScores,
    baseline.categoryScores,
  )
  assert.deepEqual(
    mergeGradingIntoRepository(baseline, {
      status: "PENDING",
      category_scores: {},
    }).categoryScores,
    {},
  )
  assert.match(results({}, "PENDING"), /ещё рассчитываются/)
})

test("category_scores takes precedence when both supported containers are supplied", () => {
  const repository = mergeGradingIntoRepository(baseline, {
    status: "FINISHED",
    category_scores: {},
    criteria: { ignored: 99 },
  })
  assert.deepEqual(repository.categoryScores, {})
})

test("initial API mapping uses dynamic criteria without inserting default zeroes", () => {
  const source = {
    id: "r",
    name: "repo",
    slug: "repo",
    organization: { slug: "org" },
    default_branch: "main",
    visibility: "public",
  }
  assert.deepEqual(repositoryFromSourceCraft(source).categoryScores, {})
  assert.deepEqual(
    repositoryFromSourceCraft(source, {
      status: "FINISHED",
      criteria: { new: 83 },
    }).categoryScores,
    { new: { score: 83, trend: null } },
  )
})

test("ranking and security render with all predefined categories removed", () => {
  const repository = {
    ...baseline,
    categoryScores: normalizeCategoryScores({ Мяу: 91 }),
  }
  const ranking = render(Ranking, {
    repositories: [repository],
    onSelectRepository() {},
  })
  assert.match(ranking, /1 проектов/)
  assert.match(ranking, /<span>—<\/span>/)
  const security = render(Security, { repository, onBack() {} })
  assert.match(security, /Нет данных/)
  assert.doesNotMatch(security, /role="progressbar"/)
  assert.equal(
    getCategoryScore(repository.categoryScores, "security", "appsec"),
    null,
  )
  assert.equal(
    getCategoryScore(
      normalizeCategoryScores({ APPSEC: 0 }),
      "security",
      "appsec",
    ),
    0,
  )
})

test("metadata is optional, validated, and escaped as text", () => {
  const html = results(
    normalizeCategoryScores({
      "<script>alert(1)</script>": {
        score: 80,
        name: 17,
        trend: "bad",
        description: { invalid: true },
      },
      other: {
        score: null,
        name: "<b>Title</b>",
        description: "<img src=x onerror=alert(1)>",
      },
    }),
  )
  assert.doesNotMatch(
    html,
    /<script>|<img |<b>|NaN|undefined|\[object Object\]/,
  )
  assert.match(html, /&lt;script&gt;/)
  assert.match(html, /&lt;b&gt;Title&lt;\/b&gt;/)
})

test("mock refresh preserves category names, descriptions and trends", async () => {
  const source = new MockRepositoryDataSource()
  const repository = (await source.listRepositories())[0]
  const refreshed = mergeGradingIntoRepository(
    repository,
    await source.getGrading(repository),
  )
  assert.deepEqual(refreshed.categoryScores, repository.categoryScores)
})
