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

const { defaultOrganization, organizationLabel } = await vite.ssrLoadModule("/src/api/mapper.ts")
const { HttpRepoHealthApi } = await vite.ssrLoadModule("/src/api/client.ts")
const { default: MyRepositoriesPage } = await vite.ssrLoadModule("/src/pages/MyRepositoriesPage.tsx")

const ORGS = [
  { slug: "team", display_name: "Команда" },
  { slug: "alice", display_name: "alice Personal Organization", personal: true },
]

test("default organization: server choice, else personal, else first", () => {
  assert.equal(defaultOrganization({ organizations: ORGS, default_org_slug: "team" }), "team")
  assert.equal(defaultOrganization({ organizations: ORGS, default_org_slug: "gone" }), "alice")
  assert.equal(defaultOrganization({ organizations: [{ slug: "x" }, { slug: "y" }] }), "x")
  assert.equal(defaultOrganization({ organizations: [] }), undefined)
})

test("organization labels mark the personal org", () => {
  assert.equal(organizationLabel(ORGS[1]), "alice (личная)")
  assert.equal(organizationLabel(ORGS[0]), "Команда (team)")
  assert.equal(organizationLabel({ slug: "plain" }), "plain")
})

test("org and repo requests carry the PAT and the org query", async () => {
  const seen = []
  const api = new HttpRepoHealthApi({
    fetcher: async (url, init) => {
      seen.push({ url, pat: init.headers.PAT })
      return Response.json(url.endsWith("/orgs") ? { organizations: ORGS } : { repositories: [] })
    },
  })

  await api.getMyOrganizations("pat-1")
  await api.getMyRepositories("pat-1", "my org", "tok/1")
  await api.getMyRepositories("pat-1")

  assert.deepEqual(
    seen.map((item) => item.url),
    ["/api/orgs", "/api/repos?org=my+org&page_token=tok%2F1", "/api/repos"],
  )
  assert.ok(seen.every((item) => item.pat === "pat-1"))
  await assert.rejects(api.getMyOrganizations(" "), (error) => error.status === 401)
})

test("org repo errors keep the generic not-found-or-no-access message", async () => {
  for (const status of [403, 404]) {
    const api = new HttpRepoHealthApi({ fetcher: async () => new Response("", { status }) })
    await assert.rejects(api.getMyRepositories("pat", "team"), /не найден или нет доступа/)
  }
})

function renderPage(props) {
  return renderToStaticMarkup(
    createElement(MyRepositoriesPage, {
      isSignedIn: true,
      hasToken: true,
      repositories: [],
      isLoading: false,
      onSubmitToken() {},
      onForgetToken() {},
      onOpenRepository() {},
      ...props,
    }),
  )
}

test("my repositories page shows the organization dropdown and load-more", () => {
  const html = renderPage({
    organizations: ORGS,
    selectedOrganization: "alice",
    hasMore: true,
    repositories: [
      { id: "1", fullName: "alice/r", orgSlug: "alice", repoSlug: "r", language: "Go", visibility: "private" },
    ],
  })

  assert.match(html, /Организация/)
  assert.match(html, /<option value="alice" selected="">alice \(личная\)<\/option>/)
  assert.match(html, /<option value="team">Команда \(team\)<\/option>/)
  assert.match(html, /Показать ещё/)
  assert.match(html, /1 из 1\+/)
})

test("my repositories page hides the dropdown without organizations", () => {
  const html = renderPage({})
  assert.doesNotMatch(html, /Организация/)
  assert.doesNotMatch(html, /Показать ещё/)
})
