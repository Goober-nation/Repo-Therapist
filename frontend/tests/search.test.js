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

const { parseRoute, routePath, directRepository } = await vite.ssrLoadModule("/src/route.ts")
const { suggestionOptions, repoCountLabel } = await vite.ssrLoadModule("/src/components/CatalogSearch.tsx")
const { default: RankingPage } = await vite.ssrLoadModule("/src/pages/RankingPage.tsx")
const { HttpRepoHealthApi } = await vite.ssrLoadModule("/src/api/client.ts")

test("the search route keeps the query, the organization and both result pages in the URL", () => {
  const route = { page: "search", q: "my repo", org: "yandex", reposPage: 3, orgsPage: 2 }
  const path = routePath(route)
  assert.equal(path, "/search?q=my+repo&org=yandex&rp=3&op=2")
  const [pathname, search] = path.split("?")
  assert.deepEqual(parseRoute(pathname, `?${search}`), route)
})

test("a search route drops the first page and bad page numbers", () => {
  assert.equal(routePath({ page: "search", q: "x", reposPage: 1 }), "/search?q=x")
  assert.deepEqual(parseRoute("/search", "?q=x&rp=abc&op=-2"), { page: "search", q: "x" })
  assert.deepEqual(parseRoute("/search"), { page: "search", q: "" })
})

test("links and exact org/repo slugs open the repository directly, other text is searched", () => {
  assert.deepEqual(directRepository("https://sourcecraft.dev/org/repo"), { org: "org", repo: "repo" })
  assert.deepEqual(directRepository(" org/repo.git "), { org: "org", repo: "repo" })
  assert.equal(directRepository("repo health"), null)
  assert.equal(directRepository("health"), null)
  assert.equal(directRepository("org/repo/extra"), null)
})

test("suggestions show at most three repositories followed by one organization", () => {
  const repo = (slug) => ({ sourcecraft_uuid: slug, org_slug: "o", repo_slug: slug })
  const options = suggestionOptions({
    repos: [repo("a"), repo("b"), repo("c"), repo("d")],
    orgs: [{ slug: "o1", repo_count: 1 }, { slug: "o2", repo_count: 2 }],
  })
  assert.deepEqual(
    options.map((option) => (option.kind === "repo" ? option.repo.repo_slug : `org:${option.org.slug}`)),
    ["a", "b", "c", "org:o1"],
  )
  assert.deepEqual(suggestionOptions(null), [])
})

test("organization repository counts use the right Russian plural", () => {
  assert.equal(repoCountLabel(1), "1 репозиторий")
  assert.equal(repoCountLabel(3), "3 репозитория")
  assert.equal(repoCountLabel(5), "5 репозиториев")
  assert.equal(repoCountLabel(11), "11 репозиториев")
  assert.equal(repoCountLabel(22), "22 репозитория")
})

test("the ranking shows the given search bar and no longer has the per-page search", () => {
  const html = renderToStaticMarkup(
    createElement(RankingPage, {
      repositories: [],
      total: 10,
      searchBar: createElement("div", { id: "catalog-search-slot" }),
    }),
  )
  assert.match(html, /catalog-search-slot/)
  assert.doesNotMatch(html, /Поиск на этой странице/)
})

test("the search client calls the Gateway search routes", async () => {
  const urls = []
  const api = new HttpRepoHealthApi({
    baseUrl: "/api",
    fetcher: async (url) => {
      urls.push(String(url))
      return new Response(JSON.stringify({ repos: [], orgs: [], total_count: 0, page: 1, page_size: 20 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      })
    },
  })

  await api.suggest("re po")
  await api.searchRepos("", "yandex", 2)
  await api.searchOrgs("yan", 3)

  assert.deepEqual(urls, [
    "/api/search/suggest?q=re+po",
    "/api/search/repos?page=2&page_size=20&org=yandex",
    "/api/search/orgs?q=yan&page=3&page_size=20",
  ])
})

test("the ranking's levels: excellent from 80, healthy from 70, attention from 40", async () => {
  const { rankingLevel, healthLevel } = await vite.ssrLoadModule("/src/metrics.ts")
  const label = (score) => rankingLevel(score).label
  assert.equal(label(80), "Отличное состояние")
  assert.equal(label(79), "Здоровый")
  assert.equal(label(70), "Здоровый")
  assert.equal(label(69), "Требует внимания")
  assert.equal(label(40), "Требует внимания")
  assert.equal(label(39), "Под риском")
  assert.equal(label(null), "Нет данных")
  assert.equal(healthLevel(80).label, "Здоровый", "the other pages keep their bands")
})
