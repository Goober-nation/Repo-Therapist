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

const view = await vite.ssrLoadModule("/src/catalogView.ts")
const audit = await vite.ssrLoadModule("/src/auditView.ts")
const { HttpRepoHealthApi, ApiRequestError } = await vite.ssrLoadModule("/src/api/client.ts")
const { default: CatalogPanel } = await vite.ssrLoadModule("/src/components/CatalogPanel.tsx")

test("catalog calls use the admin routes and the guard header on actions", async () => {
  const calls = []
  const api = new HttpRepoHealthApi({
    fetcher: async (url, init) => {
      calls.push([init.method ?? "GET", url, init.headers?.["X-Admin-Request"]])
      return new Response(JSON.stringify({ size: 0 }), { status: 200 })
    },
  })
  await api.getCatalog()
  await api.refreshCatalog()
  await api.cancelCatalogRefresh()
  assert.deepEqual(calls, [
    ["GET", "/api/admin/catalog", undefined],
    ["POST", "/api/admin/catalog/refresh", "1"],
    ["POST", "/api/admin/catalog/refresh/cancel", "1"],
  ])
})

test("a refresh while one runs keeps its reason and gets Russian wording", async () => {
  const api = new HttpRepoHealthApi({
    fetcher: async () => new Response(JSON.stringify({ error: "conflict", code: "refresh_active" }), { status: 409 }),
  })
  await assert.rejects(
    () => api.refreshCatalog(),
    (error) => error instanceof ApiRequestError && error.code === "refresh_active" && /уже обновляется/.test(error.message),
  )
})

test("refresh states and failure codes read in Russian", () => {
  assert.equal(view.catalogStatusLabel("running").label, "Идёт")
  assert.equal(view.catalogStatusLabel("failed").tone, "critical")
  assert.equal(view.catalogStatusLabel("odd").label, "odd")
  assert.match(view.catalogErrorLabel("wrapper_error"), /SourceCraft/)
  assert.equal(view.catalogErrorLabel("unknown_code"), "unknown_code")
  assert.match(view.catalogTriggerLabel({ trigger: "startup" }), /автоматически/)
  assert.equal(view.isRefreshRunning({ status: "running" }), true)
  assert.equal(view.isRefreshRunning(null), false)
})

test("catalog actions have audit-log labels", () => {
  for (const action of ["start", "cancel", "complete", "fail"]) {
    assert.match(audit.actionLabel(`catalog.refresh.${action}`), /спис/i)
  }
})

test("the panel starts in its loading state and explains what the list is for", () => {
  const api = new HttpRepoHealthApi({ fetcher: async () => new Response("{}", { status: 200 }) })
  const html = renderToStaticMarkup(createElement(CatalogPanel, { api }))
  assert.match(html, /Список репозиториев/)
  assert.match(html, /пакетная оценка выбирают репозитории из этого списка/)
  assert.match(html, /Загружаем/)
})
