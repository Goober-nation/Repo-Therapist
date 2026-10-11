import assert from "node:assert/strict"

import { after, test } from "node:test"

import { runInNewContext } from "node:vm"

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

  repositoryFromMyRepos,

  repositoryFromRanking,

  recommendationsFromResponse,

  adviceToRecommendations,

  aiReportFromResponse,
} = await vite.ssrLoadModule("/src/api/mapper.ts")

const { parseRoute, routePath, parseRepositoryLink } = await vite.ssrLoadModule(
  "/src/route.ts",
)

const { default: AnalysisResults } = await vite.ssrLoadModule(
  "/src/components/AnalysisResults.tsx",
)

const { default: Overview } = await vite.ssrLoadModule(
  "/src/pages/RepositoryOverviewPage.tsx",
)

const { default: Ranking } = await vite.ssrLoadModule(
  "/src/pages/RankingPage.tsx",
)

const { buildReport } = await vite.ssrLoadModule(
  "/src/reports/downloadReport.ts",
)

const { healthLevel, missingCategories, formatMetricValue, scoredCategories, categoryWeight } =
  await vite.ssrLoadModule("/src/metrics.ts")

const idleAnalysis = { isRunning: false, ai: undefined, run() {} }

const { HttpRepoHealthApi, ApiRequestError } = await vite.ssrLoadModule("/src/api/client.ts")

const baseline = {
  id: "test-repository",

  sourceCraftUuid: "test-repository",

  orgSlug: "example",

  repoSlug: "repository",

  name: "repository",

  fullName: "example/repository",

  language: "Go",

  defaultBranch: "main",

  visibility: "public",

  healthScore: 82,

  trend: 0,

  likes: 128,

  lastActivityDaysAgo: 1,

  categoryScores: {
    Безопасность: { score: 64, trend: null },

    Issues: { score: 74, trend: null },

    "Состояние кода": { score: 78, trend: null },
  },
}

const { devSignIn, initializeYandexButton, profileFromYandexResult, YANDEX_TOKEN_PAGE } =
  await vite.ssrLoadModule("/src/api/yandexAuth.ts")

const { default: Sidebar } = await vite.ssrLoadModule(
  "/src/components/Sidebar.tsx",
)

const { default: TopBar } = await vite.ssrLoadModule(
  "/src/components/TopBar.tsx",
)

test("Yandex SDK mounts the official button with an exact helper origin and selected theme", async () => {
  for (const theme of ["light", "dark"]) {
    const sdk = {
      async init(oauth, origin, appearance) {
        assert.deepEqual(oauth, {
          client_id: "public-client",

          response_type: "token",

          redirect_uri: "http://localhost:5173" + YANDEX_TOKEN_PAGE,
        })

        assert.equal(origin, "http://localhost:5173")

        assert.equal(appearance.view, "button")

        assert.equal(appearance.parentId, "owned-container")

        assert.equal(appearance.buttonTheme, theme)

        return {
          status: "ok",

          handler: async () => ({ access_token: "test-token" }),
        }
      },
    }

    const handler = await initializeYandexButton(
      sdk,

      " public-client ",

      "http://localhost:5173",

      "owned-container",

      theme,
    )

    assert.deepEqual(await handler(), { access_token: "test-token" })
  }
})

test("Yandex initialization fails safely for missing configuration and SDK errors", async () => {
  let calls = 0

  const sdk = {
    async init() {
      calls++

      return { status: "error", code: "provider diagnostic" }
    },
  }

  await assert.rejects(
    initializeYandexButton(sdk, "", "http://localhost:5173", "button", "light"),

    /не настроен/,
  )

  assert.equal(calls, 0)

  await assert.rejects(
    initializeYandexButton(
      sdk,

      "client",

      "http://localhost:5173",

      "button",

      "light",
    ),

    (error) => {
      assert.doesNotMatch(error.message, /provider diagnostic/)

      return true
    },
  )

  await assert.rejects(
    initializeYandexButton(
      {
        async init() {
          return { status: "ok" }
        },
      },

      "client",

      "http://localhost:5173",

      "button",

      "light",
    ),
  )
})

test("SDK token establishes a Gateway cookie session and only profile display data is returned", async () => {
  const calls = []

  const profile = await profileFromYandexResult(
    { access_token: "test-oauth-token" },

    "public-client",

    undefined,

    async (url, init) => {
      calls.push(url)

      assert.equal(init.headers.Authorization, "OAuth test-oauth-token")

      if (url.startsWith("/yandex-profile")) {
        assert.equal(init.credentials, "omit")

        return Response.json({
          id: "123",

          client_id: "public-client",

          display_name: "Анна",
        })
      }

      assert.equal(url, "/api/auth/yandex")

      assert.equal(init.method, "POST")

      assert.equal(init.credentials, "include")

      assert.equal("PAT" in init.headers, false)

      return Response.json({ session_id: "server-session" })
    },
  )

  assert.deepEqual(profile, { id: "123", name: "Анна" })

  assert.deepEqual(calls, [
    "/yandex-profile/info?format=json",

    "/api/auth/yandex",
  ])

  assert.equal(JSON.stringify(profile).includes("test-oauth-token"), false)
})

test("cancelled or malformed SDK results never trigger a profile lookup", async () => {
  for (const data of [
    null,

    {},

    { access_token: "" },

    { access_token: 123 },

    { error: "access_denied", access_token: "test-token" },
  ]) {
    let calls = 0

    await assert.rejects(
      profileFromYandexResult(data, "client", undefined, async () => {
        calls++

        throw new Error("unexpected fetch")
      }),
    )

    assert.equal(calls, 0)
  }
})

test("profile errors and a token issued to another application cannot sign the user in", async () => {
  for (const fetcher of [
    async () => new Response("private diagnostic", { status: 401 }),

    async () => Response.json({ id: "123", client_id: "other-client" }),

    async () => new Response("not JSON"),
  ]) {
    await assert.rejects(
      profileFromYandexResult(
        { access_token: "test-token" },

        "public-client",

        undefined,

        fetcher,
      ),

      (error) => {
        assert.doesNotMatch(
          error.message,

          /private diagnostic|test-token|not JSON/,
        )

        return true
      },
    )
  }
})

test("an unmounted sign-in cancels its in-flight profile lookup", async () => {
  const controller = new AbortController()

  const profile = profileFromYandexResult(
    { access_token: "test-token" },

    "public-client",

    controller.signal,

    async (_url, { signal }) => {
      return new Promise((_resolve, reject) =>
        signal.addEventListener("abort", () => reject(signal.reason), {
          once: true,
        }),
      )
    },
  )

  controller.abort()

  await assert.rejects(profile)
})

function runTokenHelper(source, window, status) {
  let onLoad

  window.addEventListener = (type, listener) => {
    if (type === "load") onLoad = listener
  }

  runInNewContext(source, {
    window,

    document: { getElementById: () => status },
  })

  assert.equal(typeof onLoad, "function", "helper must wait for window load")

  onLoad()
}

test("helper sends the token to its own origin on load and keeps the URL for the SDK", async () => {
  const { readFile } = await import("node:fs/promises")

  const source = await readFile(
    new URL("../public/auth/yandex/token.js", import.meta.url),

    "utf8",
  )

  const calls = []

  const status = { textContent: "" }

  runTokenHelper(
    source,

    {
      location: {
        origin: "http://localhost:5173",

        pathname: YANDEX_TOKEN_PAGE,

        hash: "#access_token=test-token",
      },

      history: { replaceState: (...args) => calls.push(["clear", ...args]) },

      YaSendSuggestToken(origin, extraData) {
        // extraData comes from the vm realm, so compare it as JSON.

        calls.push(["send", origin, JSON.stringify(extraData)])
      },
    },

    status,
  )

  // The SDK may still read location.hash asynchronously, so it is not cleared.

  assert.deepEqual(calls, [["send", "http://localhost:5173", "{}"]])

  assert.equal(status.textContent, "")
})

test("helper removes credentials from the URL and reports failure if the SDK is unavailable", async () => {
  const { readFile } = await import("node:fs/promises")

  const source = await readFile(
    new URL("../public/auth/yandex/token.js", import.meta.url),

    "utf8",
  )

  const status = { textContent: "" }

  let cleared = false

  runTokenHelper(
    source,

    {
      location: {
        origin: "http://localhost:5173",

        pathname: YANDEX_TOKEN_PAGE,

        hash: "#access_token=test-token",
      },

      history: {
        replaceState: () => {
          cleared = true
        },
      },
    },

    status,
  )

  assert.equal(cleared, true)

  assert.match(status.textContent, /Не удалось завершить вход/)
})

test("top bar offers Yandex ID sign-in and toggles the workspace panel", () => {
  const props = {
    activePage: "overview",

    repositoryName: "repo",

    theme: "light",

    onToggleTheme() {},

    onPageChange() {},

    onToggleSidebar() {},
  }

  const html = renderToStaticMarkup(
    createElement(TopBar, { ...props, isSidebarOpen: false }),
  )

  assert.match(html, /Repo Therapist/)

  assert.match(html, /Войти через Яндекс ID/)

  assert.doesNotMatch(html, /SourceCraft PAT|Обновить PAT/)

  assert.match(html, /aria-expanded="false"/)

  assert.doesNotMatch(html, /Мария|Аккаунт SourceCraft/)

  assert.match(
    renderToStaticMarkup(
      createElement(TopBar, { ...props, isSidebarOpen: true }),
    ),

    /aria-expanded="true"/,
  )

  const sidebar = renderToStaticMarkup(
    createElement(Sidebar, { activePage: "overview", onPageChange() {} }),
  )

  assert.match(sidebar, /Рабочая область/)

  assert.doesNotMatch(sidebar, /Repo Therapist|Выйти|Яндекс/)
})

test("signed-in account displays the profile with a sign-out button and no account menu", () => {
  const html = renderToStaticMarkup(
    createElement(TopBar, {
      activePage: "ranking",

      repositoryName: "repo",

      theme: "light",

      onToggleTheme() {},

      onPageChange() {},

      onToggleSidebar() {},

      yandexProfile: { id: "123", name: "Анна" },

      onSignOut() {},
    }),
  )

  assert.match(html, /Анна/)
  assert.match(html, /Выйти/)
  assert.doesNotMatch(html, /topbar-account-menu/)
})

test("gateway HTTP errors preserve status without exposing the response body", async () => {
  for (const status of [401, 403, 404, 500, 502]) {
    const api = new HttpRepoHealthApi({
      fetcher: async () =>
        new Response("private backend diagnostic", { status }),
    })

    await assert.rejects(api.getGrading("org", "repo"), (error) => {
      assert.equal(error.status, status)

      assert.doesNotMatch(error.message, /private backend diagnostic/)

      return true
    })
  }
})

test("gateway requests abort on timeout and can be retried", async () => {
  let calls = 0

  const api = new HttpRepoHealthApi({
    timeoutMs: 10,

    fetcher: async (_url, { signal }) => {
      if (++calls > 1) return Response.json({ status: "PENDING" })

      return new Promise((_resolve, reject) => {
        signal.addEventListener("abort", () => reject(signal.reason), {
          once: true,
        })
      })
    },
  })

  await assert.rejects(api.getGrading("org", "repo"), /не ответил вовремя/)

  assert.equal((await api.getGrading("org", "repo")).status, "PENDING")
})

test("gateway network failures and invalid JSON produce a usable error", async () => {
  for (const fetcher of [
    async () => {
      throw new TypeError("Failed to fetch")
    },

    async () => new Response("<html>proxy error</html>"),
  ]) {
    const api = new HttpRepoHealthApi({ fetcher })

    await assert.rejects(
      api.getRecommendations("org", "repo"),

      /Не удалось получить ответ сервера/,
    )
  }
})

const render = (component, props) =>
  renderToStaticMarkup(createElement(component, props))

const results = (categories, status) =>
  render(AnalysisResults, { categories, status })

test("ranking shows the backend place and links to the repository", () => {
  const ranking = render(Ranking, {
    repositories: [
      { ...baseline, place: 7, webUrl: "https://sourcecraft.dev/example/repository" },
    ],

    onSelectRepository: () => {},
  })

  assert.match(ranking, /aria-label="Открыть анализ example\/repository"/)

  assert.match(ranking, /role="cell">7</)

  assert.match(ranking, /href="https:\/\/sourcecraft.dev\/example\/repository"/)

  assert.match(ranking, /rel="noopener noreferrer"/)

  assert.doesNotMatch(ranking, /Безопасность|Security/)
})

test("one health scale drives labels and tones", () => {
  assert.deepEqual(healthLevel(null), { label: "Нет данных", tone: "neutral" })

  assert.deepEqual(healthLevel(80), healthLevel(75))

  assert.equal(healthLevel(80).tone, "good")

  assert.equal(healthLevel(74).tone, "warning")

  assert.equal(healthLevel(59).tone, "critical")

  assert.equal(healthLevel(90).label, "Отличное состояние")
})

test("HTTP client preserves the browser fetch receiver and passes dynamic JSON to the mapper", async (context) => {
  context.mock.method(globalThis, "fetch", async function (url) {
    assert.equal(this, globalThis, "Browser fetch requires the Window receiver")

    assert.equal(url, "/api/grading/example/repository")

    assert.equal(arguments[1].credentials, "include")

    assert.equal("PAT" in arguments[1].headers, false)

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

test("Gateway ranking supplies the real public repository catalog", async () => {
  const api = new HttpRepoHealthApi({
    fetcher: async (url, init) => {
      assert.equal(url, "/api/ranking?page=1&page_size=100&sortby=score")

      assert.equal(init.credentials, "include")

      assert.equal("PAT" in init.headers, false)

      return Response.json({
        entries: [
          {
            place: 1,

            sourcecraft_uuid: "uuid-1",

            name: "project",

            web_url: "https://sourcecraft.dev/team/project",

            public_score: 88,

            likes: 5,

            language: { name: "Go" },
          },
        ],

        total_count: 1,

        page: 0,

        page_size: 100,
      })
    },
  })

  const response = await api.getRanking()

  const repository = repositoryFromRanking(response.entries[0])

  assert.equal(repository.fullName, "team/project")

  assert.equal(repository.healthScore, 88)

  assert.equal(repository.likes, 5)
})

test("recommendation requests do not expose SourceCraft PAT to the browser", async () => {
  const api = new HttpRepoHealthApi({
    fetcher: async (url, init) => {
      assert.equal(url, "/api/recommendations/team/project")

      assert.equal("PAT" in init.headers, false)

      return Response.json({ status: "PENDING" })
    },
  })

  await api.getRecommendations("team", "project")
})

test("ranking asks the server for one page with its sort and language", async () => {
  const urls = []

  const api = new HttpRepoHealthApi({
    fetcher: async (url, init) => {
      assert.equal("PAT" in init.headers, false)

      urls.push(url)

      return Response.json({ entries: [], total_count: 0, page: 2, page_size: 100 })
    },
  })

  await api.getRanking(2, "likes", "Go")

  await api.getRanking()

  assert.deepEqual(urls, [
    "/api/ranking?page=2&page_size=100&sortby=likes&filter=Go",
    "/api/ranking?page=1&page_size=100&sortby=score",
  ])
})

test("the low-confidence toggle is sent only when on, and the public score is read-only", async () => {
  const urls = []
  const api = new HttpRepoHealthApi({
    fetcher: async (url) => {
      urls.push(url)
      return Response.json({ status: "FINISHED", score: 71, low_confidence: true, confidence_reasons: [] })
    },
  })

  await api.getRanking(1, "score", "", true)
  await api.getRanking(1, "score", "Go", false)
  const score = await api.getPublicScore("org", "my repo")

  assert.deepEqual(urls, [
    "/api/ranking?page=1&page_size=100&sortby=score&hide_low_confidence=true",
    "/api/ranking?page=1&page_size=100&sortby=score&filter=Go",
    "/api/score/org/my%20repo",
  ])
  assert.equal(score.low_confidence, true)
})

test("the low-confidence flag is unknown unless the backend sent a boolean, and reasons only travel with a low flag", async () => {
  const { confidenceFromPayload, repositoryFromRanking } = await vite.ssrLoadModule("/src/api/mapper.ts")
  const reasons = [{ code: "LOW_CODE", text: " Мало кода. " }, { code: "X", text: "" }, { code: "Y" }]

  assert.deepEqual(confidenceFromPayload(true, reasons), { low: true, reasons: ["Мало кода."] })
  assert.deepEqual(confidenceFromPayload(false, reasons), { low: false, reasons: [] })
  assert.equal(confidenceFromPayload(null, reasons), undefined)
  assert.equal(confidenceFromPayload(undefined, undefined), undefined)
  assert.deepEqual(confidenceFromPayload(true, undefined), { low: true, reasons: [] })

  const entry = (extra) => ({ place: 1, sourcecraft_uuid: "u", name: "repo", web_url: "https://sourcecraft.dev/org/repo", public_score: 70, ...extra })
  assert.deepEqual(repositoryFromRanking(entry({ low_confidence: true, confidence_reasons: reasons })).confidence, { low: true, reasons: ["Мало кода."] })
  assert.equal("confidence" in repositoryFromRanking(entry({ low_confidence: null })), false)
  assert.equal("confidence" in repositoryFromRanking(entry({})), false)
  assert.equal(repositoryFromRanking(entry({ low_confidence: true })).healthScore, 70)
})

test("the ranking shows a Мало данных badge with the reasons only for flagged rows, and a toggle that is off by default", async () => {
  const { default: RankingPage, DEFAULT_RANKING_QUERY } = await vite.ssrLoadModule("/src/pages/RankingPage.tsx")
  const repository = (id, confidence) => ({
    id, name: id, fullName: `org/${id}`, language: "Go", healthScore: 70, lastActivityDaysAgo: 1, categoryScores: {}, visibility: "public",
    ...(confidence ? { confidence } : {}),
  })

  assert.equal(DEFAULT_RANKING_QUERY.hideLowConfidence, false)

  const html = renderToStaticMarkup(
    createElement(RankingPage, {
      repositories: [repository("flagged", { low: true, reasons: ["Мало кода."] }), repository("fine", { low: false, reasons: [] }), repository("unknown")],
      onSelectRepository: () => {},
    }),
  )

  assert.equal(html.match(/Мало данных/g)?.length, 1)
  assert.match(html, /title="Мало кода\."/)
  assert.match(html, /Скрыть оценки с низкой достоверностью/)
  assert.doesNotMatch(html, /type="checkbox"[^>]*checked/)

  const hidden = renderToStaticMarkup(
    createElement(RankingPage, {
      repositories: [],
      query: { ...DEFAULT_RANKING_QUERY, hideLowConfidence: true },
      onSelectRepository: () => {},
    }),
  )
  assert.match(hidden, /type="checkbox"[^>]*checked/)
})

test("my repositories request sends PAT only in the Gateway header", async () => {
  const calls = []

  const api = new HttpRepoHealthApi({
    fetcher: async (url, init) => {
      calls.push({ url, init })

      return Response.json({ repositories: [], next_page_token: "" })
    },
  })

  await api.getMyRepositories("  secret-sourcecraft-pat  ")

  assert.equal(calls[0].url, "/api/repos")

  assert.equal(calls[0].init.headers.PAT, "secret-sourcecraft-pat")

  assert.equal(calls[0].init.credentials, "include")

  assert.equal("Authorization" in calls[0].init.headers, false)

  assert.equal(
    JSON.stringify(calls[0]).includes("secret-sourcecraft-pat"),

    true,
  )

  await assert.rejects(api.getMyRepositories(" "), /Введите SourceCraft PAT/)
})

test("Gateway recommendation summary is mapped without invented scores", () => {
  const recommendations = recommendationsFromResponse({
    status: "FINISHED",

    raw_response: {
      summary: "Есть что улучшить.",

      recommendations: [
        {
          category: "security",

          priority: "HIGH",

          text: "Обновить зависимость.",
        },
      ],
    },
  })

  assert.equal(recommendations[0].description, "Обновить зависимость.")

  assert.equal(recommendations[0].priority, "high")

  assert.equal("current_score" in recommendations[0], false)
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

    analysis: idleAnalysis,

    onBackToRanking() {},
  })

  assert.match(html, /Мяу: 72 из 100/)

  assert.match(html, /Architecture v2: 93 из 100/)

  assert.match(html, /Описание с бэка/)

  assert.doesNotMatch(html, /Активность проекта по истории изменений/)

  const report = buildReport(repository)

  assert.match(report, /Мяу \| 72\/100/)

  assert.match(report, /Architecture v2 \| 93\/100/)

  assert.doesNotMatch(report, /undefined|Безопасность:/)
})

test("overview explains a pending AI summary and falls back to rule-based advice", () => {
  const html = render(Overview, {
    repository: {
      ...baseline,

      categoryScores: { activity: { score: 80, trend: null }, issues: { score: 50, trend: null } },

      advice: adviceToRecommendations([
        { category_id: "issues", metric_id: "growth_ratio", text: "[high] Закрывайте issues." },
      ]),
    },

    analysis: {
      ...idleAnalysis,

      ai: { status: "PENDING", recommendations: [] },
    },

    onBackToRanking() {},
  })

  assert.match(html, /Summary AI/)

  assert.match(html, /aria-expanded="false"/)

  assert.doesNotMatch(html, /AI-сводка ещё рассчитывается/)

  assert.match(html, /Закрывайте issues\./)

  assert.match(html, /Высокий приоритет/)

  assert.match(html, /вес в итоговой оценке — 50%/)

  assert.match(html, /Веса в этой оценке[\s\S]*Активность — 50%[\s\S]*Issues — 50%/)

  assert.match(html, /Запустить анализ/)
})

test("overview shows strengths, weaknesses, missing data and the calculation", () => {
  const repository = mergeGradingIntoRepository(
    { ...baseline, analysisMode: "public", categoryScores: {}, webUrl: "https://sourcecraft.dev/example/repository" },
    {
      status: "FINISHED",

      grade: {
        score: 70,

        category_scores: {
          activity: { id: "activity", score: 90, metrics: [{ id: "commits_per_week", raw_value: 4.25 }] },

          issues: { id: "issues", score: 40, metrics: [{ id: "share_resolved", raw_value: 0.4 }] },
        },

        missing_data: ["documentation"],

        graded_at: "2026-09-28T10:00:00Z",
      },
    },
  )

  const html = render(Overview, { repository, analysis: { ...idleAnalysis, ai: aiReportFromResponse({ status: "FINISHED", raw_response: { summary: "Итог." } }) }, onBackToRanking() {} })

  assert.match(html, /aria-expanded="false"/)

  assert.doesNotMatch(html, /Итог\./)

  assert.match(html, /Сильные стороны[\s\S]*Активность: 90\/100/)

  assert.match(html, /Требует внимания[\s\S]*Issues: 40\/100/)

  assert.match(html, /Коммитов в неделю \(180 дней\)[\s\S]*4\.25/)

  assert.match(html, /Доля решённых issues[\s\S]*40%/)

  assert.match(html, /Документация<\/strong> — источник данных/)

  assert.match(html, /Безопасность<\/strong> — доступно только владельцу/)

  assert.match(html, /Как рассчитан Repo Health Score/)

  assert.match(html, /href="https:\/\/sourcecraft\.dev\/example\/repository"/)

  assert.match(html, /Последний анализ: 28\.09\.2026/)
})

test("Markdown export uses the recommendations currently shown on the page", () => {
  const repository = baseline

  const report = buildReport(repository, [
    {
      id: "fresh",

      priority: "medium",

      title: "Проверить сборку",

      description: "",

      currentScore: 80,

      projectedScore: 84,
    },
  ])

  assert.match(report, /Проверить сборку/)

  assert.match(report, /средний приоритет/)

  assert.doesNotMatch(report, /Обновить уязвимые зависимости|undefined/)

  const withAi = buildReport(repository, [], {
    summary: "Сводка.",

    recommendations: [],
  })

  assert.match(withAi, /## Summary AI\n\nСводка\./)
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

    issues: { score: 0, no_data: true },

    unavailable: { score: 0, status: "NO_DATA" },

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

  assert.equal(categories.issues.score, null)

  assert.equal(categories.unavailable.score, null)

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

    "issues",

    "unavailable",
  ]) {
    assert.equal(categories[name].score, null, name)
  }

  const html = results(categories)

  assert.equal((html.match(/role="progressbar"/g) ?? []).length, 3)

  assert.match(html, /zero: 0 из 100/)

  assert.match(html, /Нет данных/)

  assert.match(
    buildReport({ ...baseline, categoryScores: categories }),

    /absent \| Нет данных/,
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

test("GET /repos entries become private-mode repositories without default zeroes", () => {
  const source = {
    sourcecraft_uuid: "r",

    org_slug: "org",

    repo_slug: "repo",

    visibility: "private",

    web_url: "https://sourcecraft.dev/org/repo",
  }

  const repository = repositoryFromMyRepos(source)

  assert.equal(repository.fullName, "org/repo")

  assert.equal(repository.analysisMode, "private")

  assert.deepEqual(repository.categoryScores, {})

  assert.deepEqual(
    mergeGradingIntoRepository(repository, {
      status: "FINISHED",

      criteria: { new: 83 },
    }).categoryScores,

    { new: { score: 83, trend: null } },
  )
})

test("ranking renders with all predefined categories removed", () => {
  const repository = {
    ...baseline,

    likes: undefined,

    categoryScores: normalizeCategoryScores({ Мяу: 91 }),
  }

  const ranking = render(Ranking, {
    repositories: [repository],

    onSelectRepository() {},
  })

  assert.match(ranking, /1 проектов/)

  assert.match(ranking, /role="cell">—<\/span>/)
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

test("Gateway grade response maps its nested payload into repository details", () => {
  const refreshed = mergeGradingIntoRepository(baseline, {
    status: "FINISHED",

    grade: {
      score: 76,

      category_scores: { security: 61, docs: { score: 90 } },

      missing_data: ["issues"],

      graded_at: "2026-09-28T10:00:00Z",
    },
  })

  assert.equal(refreshed.healthScore, 76)

  assert.equal(refreshed.categoryScores.security.score, 61)

  assert.equal(refreshed.categoryScores.docs.score, 90)

  assert.deepEqual(refreshed.missingData, ["issues"])
})

test("rule-based advice reads the priority field, falls back to the tag, keeps category", () => {
  const advice = adviceToRecommendations([
    { category_id: "code_health", metric_id: "density_per_kloc", text: "[low] Разберите TODO." },
    { category_id: "ci_cd", metric_id: "has_ci", text: "[critical] Добавьте CI." },
    { category_id: "security", metric_id: "open_high", priority: "high", text: "[high] Исправьте находки." },
    // The field wins over a disagreeing tag; the tag is still stripped.
    { category_id: "activity", metric_id: "m", priority: "low", text: "[high] Коммитьте чаще." },
    { category_id: "documentation", metric_id: "DOC_README_MISSING", priority: "high", text: "Добавьте README." },
    { text: "Без тега." },
    { text: "  " },
    null,
  ])

  assert.deepEqual(
    advice.map(({ priority, title, description }) => [priority, title, description]),
    [
      ["critical", "CI/CD", "Добавьте CI."],
      ["high", "Security", "Исправьте находки."],
      ["high", "Documentation", "Добавьте README."],
      ["medium", "Рекомендация", "Без тега."],
      ["low", "Code health", "Разберите TODO."],
      ["low", "Activity", "Коммитьте чаще."],
    ],
  )
})

test("AI recommendations are titled by category and sorted by priority", () => {
  const report = aiReportFromResponse({
    status: "FINISHED",

    raw_response: {
      recommendations: [
        { priority: "LOW", category: "documentation", text: "Опишите запуск." },
        { priority: "HIGH", category: "security", text: "Обновите пакет." },
      ],
    },
  })

  assert.deepEqual(
    report.recommendations.map((item) => [item.priority, item.title]),
    [
      ["high", "Безопасность"],
      ["low", "Документация"],
    ],
  )
})

test("metric facts keep no-data values and format shares as percent", () => {
  const scores = normalizeCategoryScores({
    issues: {
      score: 50,

      metrics: [
        { id: "share_resolved", raw_value: 0.426 },
        { id: "median_closure_days", no_data: true, raw_value: 0 },
        { id: 3 },
      ],
    },
  })

  assert.deepEqual(scores.issues.facts, [
    { id: "share_resolved", value: 0.426, score: null },
    { id: "median_closure_days", value: null, score: null },
  ])

  assert.deepEqual(
    normalizeCategoryScores({ activity: { score: 60, metrics: [{ id: "commits_per_week", raw_value: 2, score: 0.4 }, { id: "x", raw_value: 1, score: 7 }] } }).activity.facts,
    [
      { id: "commits_per_week", value: 2, score: 0.4 },
      { id: "x", value: 1, score: null },
    ],
  )

  assert.equal(formatMetricValue("share_resolved", 0.426), "43%")

  assert.equal(formatMetricValue("median_closure_days", null), "нет данных")
})

test("private-only categories are reported as missing only in public mode", () => {
  const graded = { ...baseline, categoryScores: { activity: { score: 80, trend: null } }, gradedAt: "2026-09-28" }

  assert.deepEqual(
    missingCategories({ ...graded, analysisMode: "public" }).map((item) => item.id),
    ["security", "ci_cd"],
  )

  assert.deepEqual(missingCategories({ ...graded, analysisMode: "private" }), [])

  assert.deepEqual(missingCategories({ ...baseline, categoryScores: {} }), [])
})

test("routes and pasted links map to repository pages", () => {
  assert.deepEqual(parseRoute("/"), { page: "ranking" })

  assert.deepEqual(parseRoute("/my"), { page: "my" })

  assert.deepEqual(parseRoute("/repo/org/my%20repo"), { page: "repository", org: "org", repo: "my repo", private: false })

  assert.deepEqual(parseRoute("/my/org/repo/extra"), { page: "repository", org: "org", repo: "repo", private: true })

  assert.deepEqual(parseRoute("/repo/%E0%A4%A/x"), { page: "ranking" })

  assert.equal(routePath({ page: "repository", org: "o", repo: "r/x", private: true }), "/my/o/r%2Fx")

  for (const link of [
    "https://sourcecraft.dev/org/repo",
    "sourcecraft.dev/org/repo.git",
    "https://www.sourcecraft.dev/org/repo/tree/main",
    "  HTTPS://SOURCECRAFT.DEV/org/repo/  ",
  ]) {
    assert.deepEqual(parseRepositoryLink(link), { org: "org", repo: "repo" }, link)
  }

  for (const link of ["", "https://github.com/org/repo", "https://sourcecraft.dev/org", "not a url"]) {
    assert.equal(parseRepositoryLink(link), null, link)
  }
})

test("the owner PAT is sent only when the caller passes it", async () => {
  const headers = []

  const api = new HttpRepoHealthApi({
    fetcher: async (url, init) => {
      headers.push(init.headers)

      return Response.json({ status: "PENDING" })
    },
  })

  await api.getGrading("org", "repo")

  await api.getGrading("org", "repo", "  owner-pat ")

  await api.getRecommendations("org", "repo", "owner-pat")

  assert.equal("PAT" in headers[0], false)

  assert.equal(headers[1].PAT, "owner-pat")

  assert.equal(headers[2].PAT, "owner-pat")
})


const { default: MyRepositories } = await vite.ssrLoadModule("/src/pages/MyRepositoriesPage.tsx")

const { pollUntilReady } = await vite.ssrLoadModule("/src/useRepositoryAnalysis.ts")

test("my repositories page walks through sign-in, token and repository list", () => {
  const props = {
    repositories: [],

    isLoading: false,

    onSubmitToken() {},

    onForgetToken() {},

    onOpenRepository() {},
  }

  assert.match(render(MyRepositories, { ...props, isSignedIn: false, hasToken: false }), /Войдите через Яндекс ID/)

  const form = render(MyRepositories, { ...props, isSignedIn: true, hasToken: false })

  assert.match(form, /type="password"/)

  assert.match(form, /autoComplete="off"|autocomplete="off"/)

  const list = render(MyRepositories, {
    ...props,

    isSignedIn: true,

    hasToken: true,

    repositories: [repositoryFromMyRepos({ sourcecraft_uuid: "u", org_slug: "me", repo_slug: "app", visibility: "private" })],
  })

  assert.match(list, /me\/app/)

  assert.match(list, /Закрытый/)

  assert.match(list, /Анализировать/)
})

test("polling repeats while PENDING and stops when the page is left", async (context) => {
  context.mock.timers.enable({ apis: ["setTimeout", "Date"] })

  const statuses = ["PENDING", "PENDING", "FINISHED"]

  let calls = 0

  const done = pollUntilReady(async () => ({ status: statuses[calls++] }), () => true)

  for (let step = 0; step < 5; step += 1) {
    await Promise.resolve()

    await Promise.resolve()

    context.mock.timers.tick(10_000)
  }

  assert.deepEqual(await done, { status: "FINISHED" })

  assert.equal(calls, 3)

  let active = true

  let leftCalls = 0

  const left = pollUntilReady(async () => {
    leftCalls += 1

    active = false

    return { status: "PENDING" }
  }, () => active)

  assert.deepEqual(await left, { status: "PENDING" })

  assert.equal(leftCalls, 1)
})

test("an overall score outside 0-100 is not shown", () => {
  for (const score of [120, -1, Number.NaN]) {
    const repository = mergeGradingIntoRepository(baseline, { status: "FINISHED", grade: { score, category_scores: {} } })

    assert.notEqual(repository.healthScore, 120)

    assert.ok(repository.healthScore === null || repository.healthScore === baseline.healthScore)
  }

  assert.equal(mergeGradingIntoRepository(baseline, { status: "FINISHED", grade: { score: 99.6 } }).healthScore, 100)

  assert.equal(
    repositoryFromRanking({ sourcecraft_uuid: "u", name: "r", web_url: "https://sourcecraft.dev/o/r", public_score: 250 }).healthScore,
    null,
  )
})

test("weights shown are renormalized over categories that have data", () => {
  const sum = (items) => items.reduce((total, item) => total + item.weight, 0)

  assert.deepEqual(
    scoredCategories(false).map((item) => [item.id, item.weight]),
    [["activity", 31], ["issues", 31], ["documentation", 23], ["code_health", 15]],
  )

  const onlyTwo = { activity: { score: 80, trend: null }, issues: { score: 50, trend: null }, documentation: { score: null, trend: null } }

  assert.deepEqual(
    scoredCategories(false, onlyTwo).map((item) => [item.id, item.weight]),
    [["activity", 50], ["issues", 50]],
  )

  assert.equal(categoryWeight("documentation", false, onlyTwo), undefined)

  assert.equal(sum(scoredCategories(true)), 100)
})

test("a rate-limited poll waits and retries instead of failing", async (context) => {
  context.mock.timers.enable({ apis: ["setTimeout", "Date"] })

  let calls = 0

  const done = pollUntilReady(async () => {
    calls += 1

    if (calls === 1) throw new ApiRequestError("Слишком много запросов.", 429)

    return { status: "FINISHED" }
  }, () => true)

  for (let step = 0; step < 4; step += 1) {
    await Promise.resolve()

    await Promise.resolve()

    context.mock.timers.tick(30_000)
  }

  assert.deepEqual(await done, { status: "FINISHED" })

  assert.equal(calls, 2)

  await assert.rejects(
    pollUntilReady(async () => {
      throw new ApiRequestError("Нет", 500)
    }, () => true),
    /Нет/,
  )
})

test("no-access and not-found share one message", async () => {
  const messages = []

  for (const status of [401, 403, 404]) {
    const api = new HttpRepoHealthApi({ fetcher: async () => new Response("", { status }) })

    await api.getGrading("org", "repo").catch((error) => messages.push(error.message))
  }

  assert.equal(new Set(messages).size, 1)
})

test("a failed ranking shows a retry, not an empty catalog", () => {
  const html = render(Ranking, { repositories: [], error: "Сервер не ответил.", onSelectRepository() {} })

  assert.match(html, /Не удалось загрузить рейтинг/)

  assert.match(html, /Повторить/)

  assert.doesNotMatch(html, /Пока нет репозиториев/)

  const paged = render(Ranking, {
    repositories: [{ ...baseline, place: 101 }],
    total: 250,
    query: { page: 2, sort: "score", language: "" },
    onSelectRepository() {},
  })

  assert.match(paged, /Страница 2 из 3/)
})

test("shown weights always add up to 100", () => {
  const ids = ["activity", "issues", "documentation", "code_health", "security", "ci_cd"]

  for (let mask = 1; mask < 1 << ids.length; mask += 1) {
    const withData = Object.fromEntries(
      ids.filter((id, index) => mask & (1 << index)).map((id) => [id, { score: 50, trend: null }]),
    )

    for (const isPrivate of [false, true]) {
      const weights = scoredCategories(isPrivate, withData)

      if (weights.length) assert.equal(weights.reduce((sum, item) => sum + item.weight, 0), 100, JSON.stringify([mask, isPrivate]))
    }
  }
})

test("the service-wide grading limit reports the wait in minutes", async () => {
  const api = new HttpRepoHealthApi({
    fetcher: async () =>
      Response.json({ error: "grading limit reached" }, { status: 429, headers: { "Retry-After": "250" } }),
  })

  await assert.rejects(api.getGrading("org", "repo"), (error) => {
    assert.equal(error.name, "GradingLimitError")
    assert.equal(error.status, 429)
    assert.equal(error.retryAfterSeconds, 250)
    assert.match(error.message, /через 5 мин/)
    return true
  })
})

test("the per-client rate limit keeps the generic 429 message", async () => {
  const api = new HttpRepoHealthApi({
    fetcher: async () => Response.json({ error: "rate limit exceeded" }, { status: 429 }),
  })

  await assert.rejects(api.getGrading("org", "repo"), (error) => {
    assert.equal(error.name, "ApiRequestError")
    assert.match(error.message, /Слишком много запросов/)
    return true
  })
})

test("an interrupted grading asks the user to run the analysis again", async () => {
  const api = new HttpRepoHealthApi({
    fetcher: async () => Response.json({ error: "grading interrupted" }, { status: 502 }),
  })

  await assert.rejects(api.getGrading("org", "repo"), (error) => {
    assert.equal(error.status, 502)
    assert.match(error.message, /Оценка прервана/)
    return true
  })
})

test("language color is kept only as a hex value", async () => {
  const { repositoryFromRanking } = await vite.ssrLoadModule("/src/api/mapper.ts")
  const entry = (color) => ({
    place: 1,
    sourcecraft_uuid: "u",
    name: "repo",
    web_url: "https://sourcecraft.dev/org/repo",
    language: { name: "Go", color },
  })

  assert.equal(repositoryFromRanking(entry("#00ADD8")).languageColor, "#00ADD8")
  assert.equal(repositoryFromRanking(entry("red; background: url(x)")).languageColor, undefined)
  assert.equal(repositoryFromRanking(entry(undefined)).languageColor, undefined)
})

test("the README badge snippet links the SVG badge to the repo page", async () => {
  const { badgeMarkdown } = await vite.ssrLoadModule("/src/components/BadgeSnippet.tsx")

  assert.equal(
    badgeMarkdown("https://repo-therapist.alo.quest", "org", "my repo"),
    "[![Repo Health](https://repo-therapist.alo.quest/api/badge/org/my%20repo.svg)](https://repo-therapist.alo.quest/repo/org/my%20repo)",
  )
})

test("README snippets cover the category badges, the widget, shields and the JSON API", async () => {
  const { BADGE_CATEGORIES, categoryBadgeMarkdown, widgetMarkdown, scoreApiUrl, shieldsMarkdown } = await vite.ssrLoadModule(
    "/src/components/BadgeSnippet.tsx",
  )
  const origin = "https://repo-therapist.alo.quest"

  assert.deepEqual([...BADGE_CATEGORIES], ["activity", "issues", "documentation", "code_health"])
  assert.equal(
    categoryBadgeMarkdown(origin, "org", "my repo", "code_health"),
    "[![Repo Health: code_health](https://repo-therapist.alo.quest/api/badge/org/my%20repo/code_health.svg)](https://repo-therapist.alo.quest/repo/org/my%20repo)",
  )
  assert.equal(
    widgetMarkdown(origin, "org", "repo"),
    "[![Repo Health](https://repo-therapist.alo.quest/api/widget/org/repo.svg)](https://repo-therapist.alo.quest/repo/org/repo)",
  )
  assert.equal(scoreApiUrl(origin, "org", "my repo"), "https://repo-therapist.alo.quest/api/score/org/my%20repo")
  assert.equal(
    shieldsMarkdown(origin, "org", "repo"),
    "[![Repo Health](https://img.shields.io/endpoint?url=https%3A%2F%2Frepo-therapist.alo.quest%2Fapi%2Fbadge%2Forg%2Frepo.json)](https://repo-therapist.alo.quest/repo/org/repo)",
  )
})

test("the README snippets are a dropdown that starts collapsed and loads nothing", async () => {
  const { default: BadgeSnippet } = await vite.ssrLoadModule("/src/components/BadgeSnippet.tsx")
  const html = renderToStaticMarkup(createElement(BadgeSnippet, { org: "org", repo: "repo" }))

  assert.match(html, /aria-expanded="false"/)
  assert.match(html, />Показать</)
  assert.doesNotMatch(html, /badge-snippet__item|<img|api\/badge|api\/widget|api\/score/)
})

test("badge links use the configured public URL's origin and fall back to the page origin", async () => {
  const { publicOrigin } = await vite.ssrLoadModule("/src/components/BadgeSnippet.tsx")
  const page = "http://localhost:5173"

  assert.equal(publicOrigin("https://repo-therapist.alo.quest/", page), "https://repo-therapist.alo.quest")
  assert.equal(publicOrigin("  http://localhost:8081/x?y  ", page), "http://localhost:8081")
  assert.equal(publicOrigin(undefined, page), page)
  assert.equal(publicOrigin("", page), page)
  assert.equal(publicOrigin("not a url", page), page)
  assert.equal(publicOrigin("javascript:alert(1)", page), page)
})

const analyticsBody = {
  status: "FINISHED",
  graded_at: "2026-10-01T12:00:00Z",
  analytics: {
    version: 2,
    window_days: 180,
    reviews: { status: "ok", sampled: 10, merged: 8, unreviewed: 3, unreviewed_share: 0.38, median_merge_hours: 30 },
    bus_factor: {
      status: "ok",
      value: 2,
      commits: 40,
      authors: 5,
      top_authors: [
        { name: "Ann", commits: 22, share: 0.55 },
        { name: "Bob", commits: 8, share: 0.2 },
        { commits: 3, share: 0.1 },
      ],
    },
    ownership: {
      status: "ok",
      areas: [
        { path: "src/", files: 12, lines: 900, top_owner: "Ann", top_owner_share: 0.8, owners: 2 },
        { files: 1 },
      ],
      codeowners: { present: true, path: ".github/CODEOWNERS", rules: 3, covered_share: 0.75 },
    },
  },
}

test("analytics: the response is read defensively and no-data blocks stay unavailable, not zero", async () => {
  const { analyticsFromResponse } = await vite.ssrLoadModule("/src/api/mapper.ts")

  const mapped = analyticsFromResponse(analyticsBody)
  assert.equal(mapped.windowDays, 180)
  assert.deepEqual(mapped.busFactor, {
    available: true,
    value: 2,
    commits: 40,
    authors: 5,
    topAuthors: [
      { name: "Ann", commits: 22, share: 0.55 },
      { name: "Bob", commits: 8, share: 0.2 },
    ],
  })
  assert.deepEqual(mapped.reviews, {
    available: true,
    reason: "unavailable",
    sampled: 10,
    merged: 8,
    unreviewed: 3,
    unreviewedShare: 0.38,
    medianMergeHours: 30,
    // The fixture has no bottlenecks block (an object stored before version 3): unavailable, not zeros.
    bottlenecks: { available: false, reason: "unavailable", reviewed: 0, open: 0, waiting: 0, waitingDays: 0 },
  })
  assert.equal(mapped.ownership.areas.length, 1)
  assert.deepEqual(mapped.ownership.codeowners, { present: true, path: ".github/CODEOWNERS", rules: 3, coveredShare: 0.75 })

  const noData = analyticsFromResponse({
    status: "FINISHED",
    analytics: {
      window_days: 180,
      bus_factor: { status: "no_data", commits: 2, authors: 1, top_authors: [] },
      ownership: { status: "no_data", areas: [], codeowners: { present: false, rules: 0, covered_share: null } },
    },
  })
  // Analytics stored before the reviews block existed show it as unavailable, never as zeros.
  assert.equal(noData.reviews.available, false)
  assert.equal("unreviewedShare" in noData.reviews, false)
  assert.equal(noData.busFactor.available, false)
  assert.equal(noData.busFactor.value, 0)
  assert.equal(noData.ownership.available, false)
  assert.equal("coveredShare" in noData.ownership.codeowners, false)

  assert.equal(analyticsFromResponse({ status: "NO_DATA", analytics: null }), null)
  assert.equal(analyticsFromResponse(undefined), null)
  assert.equal(analyticsFromResponse({ status: "FINISHED", analytics: "oops" }), null)
  assert.equal(analyticsFromResponse({ status: "FINISHED", analytics: { bus_factor: { status: "ok", value: 99, top_authors: "x" } } }).busFactor.topAuthors.length, 0)
})

test("analytics: the API client reads /analytics with the owner's PAT only when given", async () => {
  const calls = []
  const api = new HttpRepoHealthApi({
    fetcher: async (url, init) => {
      calls.push([url, init.headers.PAT])
      return Response.json(analyticsBody)
    },
  })

  await api.getAnalytics("org", "my repo")
  await api.getAnalytics("org", "repo", "pat-1")

  assert.deepEqual(calls, [
    ["/api/analytics/org/my%20repo", undefined],
    ["/api/analytics/org/repo", "pat-1"],
  ])
})

test("analytics section: renders bus factor, owners and CODEOWNERS, and every empty state in words", async () => {
  const { default: AnalyticsSection } = await vite.ssrLoadModule("/src/components/AnalyticsSection.tsx")
  const { analyticsFromResponse } = await vite.ssrLoadModule("/src/api/mapper.ts")
  const render = (analytics, extra = {}) => renderToStaticMarkup(createElement(AnalyticsSection, { analytics, ...extra }))

  const full = render(analyticsFromResponse(analyticsBody))
  assert.match(full, /Углублённая аналитика/)
  assert.match(full, /Половину коммитов за последние 180 дн\. сделали 2 автора из 5/)
  assert.match(full, />Ann</)
  assert.match(full, />55%</)
  assert.match(full, /<code>src\/<\/code>/)
  assert.match(full, />75%</)
  assert.match(full, /3 правила/)
  assert.match(full, /на оценку не влияют/)
  assert.match(full, />38%</)
  assert.match(full, /влитых pull request&#x27;ов без ревью: 3 из 8 среди последних 10/)
  assert.match(full, /Медианное время до слияния: 30 ч/)

  const reviewReason = (reason) =>
    render(analyticsFromResponse({ status: "FINISHED", analytics: { window_days: 180, reviews: { status: "no_data", reason } } }))
  assert.match(reviewReason("few_merged"), /слишком мало влитых/)
  assert.match(reviewReason("imported"), /импорт истории/)
  assert.match(reviewReason("unavailable"), /получить не удалось/)
  assert.match(reviewReason("something-new"), /получить не удалось/)

  const single = analyticsFromResponse({ status: "FINISHED", analytics: { window_days: 90, bus_factor: { status: "ok", value: 1, commits: 20, authors: 1, top_authors: [{ name: "Solo", commits: 20, share: 1 }] } } })
  assert.match(render(single), /Проект сильно зависит от одного человека/)
  assert.match(render(single), /Файл CODEOWNERS не найден/)

  const noData = analyticsFromResponse({ status: "FINISHED", analytics: { window_days: 180, bus_factor: { status: "no_data" }, ownership: { status: "no_data", areas: [] } } })
  const noDataHtml = render(noData)
  assert.equal(noDataHtml.match(/Нет данных: за период слишком мало коммитов/g)?.length, 2)
  assert.doesNotMatch(noDataHtml, /analytics-card__value/)

  assert.match(render(undefined), /Загружаем аналитику/)
  assert.match(render(null), /Аналитики для этого репозитория пока нет/)
  assert.match(render(null, { isRunning: true }), /Аналитика появится после завершения анализа/)
})

test("public grade toggle drops the token only for a public repository opened by its owner", async () => {
  const { gradingToken } = await vite.ssrLoadModule("/src/metrics.ts")

  assert.equal(gradingToken("pat", true, "public", true), undefined)
  assert.equal(gradingToken("pat", true, "public", false), "pat")
  assert.equal(gradingToken("pat", true, "private", true), "pat")
  assert.equal(gradingToken("pat", false, "public", false), undefined)

  const sent = []
  const api = new HttpRepoHealthApi({
    fetcher: async (url, init) => {
      sent.push([url, "PAT" in init.headers])
      return Response.json({ status: "FINISHED" })
    },
  })
  await api.getGrading("org", "repo", gradingToken("pat", true, "public", true))
  await api.getRecommendations("org", "repo", gradingToken("pat", true, "public", true))
  assert.deepEqual(sent, [
    ["/api/grading/org/repo", false],
    ["/api/recommendations/org/repo", false],
  ])
})

test("overview shows the public grade toggle and which grade is displayed", () => {
  const props = { analysis: idleAnalysis, onBackToRanking() {} }
  const toggle = (checked) => ({ checked, onChange() {} })

  const privateHtml = render(Overview, {
    ...props,
    repository: { ...baseline, visibility: "public", analysisMode: "private" },
    publicView: toggle(false),
  })
  assert.match(privateHtml, /Публичная оценка/)
  assert.match(privateHtml, /показана приватная оценка \(6 категорий\)/)

  const publicHtml = render(Overview, {
    ...props,
    repository: { ...baseline, visibility: "public", analysisMode: "public" },
    publicView: toggle(true),
  })
  assert.match(publicHtml, /показана публичная оценка \(4 категории\)/)
  assert.match(publicHtml, /checked=""/)

  const noToggle = render(Overview, { ...props, repository: { ...baseline, analysisMode: "public" } })
  assert.doesNotMatch(noToggle, /Публичная оценка/)
})

test("private analysis of a private repository hints at Security Add-on", () => {
  const graded = {
    ...baseline,
    visibility: "private",
    analysisMode: "private",
    categoryScores: { activity: { score: 80, trend: null }, security: { score: null, trend: null } },
    gradedAt: "2026-09-28",
  }
  const security = missingCategories(graded).find((item) => item.id === "security")
  assert.equal(security.reason, "Анализ безопасности для приватных репозиториев доступен только с Security Add-on")

  const publicRepo = missingCategories({ ...graded, visibility: "public" }).find((item) => item.id === "security")
  assert.doesNotMatch(publicRepo.reason, /Add-on/)
  for (const item of missingCategories({ ...graded, analysisMode: "public" })) {
    assert.doesNotMatch(item.reason, /Add-on/)
  }
})

test("AI report carries the model that produced the summary", () => {
  const ai = aiReportFromResponse({ status: "FINISHED", model_used: " yandexgpt-5-pro ", raw_response: { summary: "Итог." } })
  assert.equal(ai.modelUsed, "yandexgpt-5-pro")
  assert.equal(aiReportFromResponse({ status: "FINISHED", raw_response: { summary: "Итог." } }).modelUsed, undefined)
  assert.equal(aiReportFromResponse({ status: "FINISHED", model_used: "  " }).modelUsed, undefined)
})

test("documentation card marks an algorithmic grade as simplified", () => {
  const scores = normalizeCategoryScores({
    documentation: { id: "documentation", score: 52, method: "algorithmic", metrics: [] },
    activity: { id: "activity", score: 80, metrics: [] },
    issues: { id: "issues", score: 70, method: "magic", metrics: [] },
  })

  assert.equal(scores.documentation.method, "algorithmic")
  assert.equal(scores.activity.method, undefined)
  assert.equal(scores.issues.method, undefined)

  const simplified = results(scores, "FINISHED")
  assert.equal(simplified.match(/Упрощённая оценка: проверено только наличие разделов/g)?.length, 1)

  const llm = results(normalizeCategoryScores({ documentation: { score: 80, method: "llm" } }), "FINISHED")
  assert.doesNotMatch(llm, /Упрощённая оценка/)
})

test("critical recommendations rank above high and keep their own label", () => {
  const advice = adviceToRecommendations([
    { category_id: "security", priority: "high", text: "[high] B" },
    { category_id: "ci_cd", priority: "critical", text: "[critical] A" },
  ])
  assert.deepEqual(advice.map((item) => item.priority), ["critical", "high"])
  const page = render(Overview, {
    repository: { ...baseline, gradeStatus: "FINISHED", healthScore: 50, advice },
    analysis: idleAnalysis,
  })
  assert.match(page, /Критический приоритет/)
})

test("dev sign-in opens a Gateway session without calling Yandex", async () => {
  const calls = []
  const profile = await devSignIn(async (url, init) => {
    calls.push(url)
    assert.equal(init.method, "POST")
    assert.equal(init.headers.Authorization, "OAuth dev")
    return Response.json({ session_id: "dev-session" })
  })
  assert.deepEqual(calls, ["/api/auth/yandex"])
  assert.equal(profile.id, "dev")
})
