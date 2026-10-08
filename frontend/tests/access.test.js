import assert from "node:assert/strict"

import { fileURLToPath } from "node:url"

import { after, test } from "node:test"

import { createElement } from "react"

import { renderToStaticMarkup } from "react-dom/server"

import { createServer } from "vite"

const vite = await createServer({
  server: { middlewareMode: true, watch: null, hmr: false },
  appType: "custom",
})

after(() => vite.close())

const view = await vite.ssrLoadModule("/src/auditView.ts")
const { parseRoute, routePath } = await vite.ssrLoadModule("/src/route.ts")
const { default: AdminAccessPage } = await vite.ssrLoadModule("/src/pages/AdminAccessPage.tsx")
const { default: Sidebar } = await vite.ssrLoadModule("/src/components/Sidebar.tsx")
const { HttpRepoHealthApi, ApiRequestError } = await vite.ssrLoadModule("/src/api/client.ts")

const status = (extra = {}) => ({ is_admin: false, signed_in: true, unlock_available: true, via_password: false, ...extra })

const render = (value, extra = {}) =>
  renderToStaticMarkup(
    createElement(AdminAccessPage, {
      api: new HttpRepoHealthApi(),
      status: value,
      onRetry() {},
      onStatusChange() {},
      ...extra,
    }),
  )

test("the tab has one route; the old /admin link opens it too", () => {
  assert.deepEqual(parseRoute("/admin-access"), { page: "access" })
  assert.deepEqual(parseRoute("/admin"), { page: "access" })
  assert.equal(routePath({ page: "access" }), "/admin-access")
})

test("the side panel has the access tab for everyone and no separate admin tab", () => {
  const html = renderToStaticMarkup(createElement(Sidebar, { activePage: "ranking", onPageChange() {} }))
  assert.match(html, /Доступ админа/)
  assert.doesNotMatch(html, /админк/i)
})

test("no text of the interface uses the word «админка» in any form", async () => {
  const { readdir, readFile } = await import("node:fs/promises")
  const files = []
  const walk = async (dir) => {
    for (const entry of await readdir(dir, { withFileTypes: true })) {
      const path = `${dir}/${entry.name}`
      if (entry.isDirectory()) await walk(path)
      else if (/\.(tsx?|css)$/.test(entry.name)) files.push(path)
    }
  }
  await walk(fileURLToPath(new URL("../src", import.meta.url)))
  assert.ok(files.length > 20, `the scan found only ${files.length} source files`)
  for (const file of files) {
    assert.doesNotMatch(await readFile(file, "utf8"), /админк/i, `${file} uses the word «админка»`)
  }
})

test("a signed-in non-admin sees the password form with a masked field and the lock rule", () => {
  const html = render(status())
  assert.match(html, /type="password"/)
  assert.match(html, /Получить доступ/)
  assert.match(html, /5 неверных паролей/)
  assert.doesNotMatch(html, /value="[^"]/)
  assert.doesNotMatch(html, /Журнал действий/)
})

test("a visitor is asked to sign in first and gets no password field", () => {
  const html = render(status({ signed_in: false }))
  assert.match(html, /Сначала войдите через Яндекс/)
  assert.doesNotMatch(html, /type="password"/)
})

test("a stand without a configured password says so and shows no form", () => {
  const html = render(status({ unlock_available: false }))
  assert.match(html, /Вход по паролю не настроен/)
  assert.doesNotMatch(html, /type="password"/)
})

test("while the status loads the page says it is checking", () => {
  const html = render(undefined)
  assert.match(html, /Проверяем доступ/)
  assert.doesNotMatch(html, /type="password"/)
  assert.doesNotMatch(html, /Повторить/)
})

test("when the status cannot be loaded the page says so and offers a retry instead of waiting forever", () => {
  const html = render(undefined, { error: "Не удалось выполнить запрос (HTTP 404)." })
  assert.match(html, /Не удалось проверить доступ/)
  assert.match(html, /HTTP 404/)
  assert.match(html, /Повторить/)
  assert.doesNotMatch(html, /Проверяем доступ…/)
})

test("an admin by password gets the admin tools in the same tab, with the way to take the rights off", () => {
  const html = render(status({ is_admin: true, via_password: true }))
  assert.match(html, /Вы администратор/)
  assert.match(html, /Права получены по паролю/)
  assert.match(html, /до конца этого входа/)
  assert.match(html, /Снять права/)
  assert.match(html, /Журнал действий/)
  assert.doesNotMatch(html, /type="password"/)
})

test("an admin from the list gets the tools but no lock button, since the rights are not from a password", () => {
  const html = render(status({ is_admin: true, via_password: false }))
  assert.match(html, /указаны в списке администраторов/)
  assert.match(html, /Журнал действий/)
  assert.doesNotMatch(html, /Снять права/)
})

test("the admin tools are never rendered for a non-admin, whatever else is known", () => {
  for (const value of [status(), status({ signed_in: false }), status({ unlock_available: false }), undefined]) {
    assert.doesNotMatch(render(value), /Журнал действий/)
  }
})

test("the unlock sends the password in the body only, with the admin header and the cookie", async () => {
  let seen
  const api = new HttpRepoHealthApi({
    fetcher: async (url, init) => {
      seen = { url, init }
      return new Response(JSON.stringify(status({ is_admin: true, via_password: true })), { status: 200 })
    },
  })

  const result = await api.unlockAdmin("s3cret pass&=?")

  assert.equal(seen.url, "/api/admin/unlock")
  assert.equal(seen.init.method, "POST")
  assert.equal(seen.init.headers["X-Admin-Request"], "1")
  assert.equal(seen.init.credentials, "include")
  assert.deepEqual(JSON.parse(seen.init.body), { password: "s3cret pass&=?" })
  assert.doesNotMatch(seen.url, /s3cret|password/)
  assert.equal(result.is_admin, true)
})

test("the lock request carries the admin header and no body", async () => {
  let seen
  const api = new HttpRepoHealthApi({
    fetcher: async (url, init) => {
      seen = { url, init }
      return new Response(JSON.stringify(status()), { status: 200 })
    },
  })

  const result = await api.lockAdmin()
  assert.equal(seen.url, "/api/admin/lock")
  assert.equal(seen.init.method, "POST")
  assert.equal(seen.init.headers["X-Admin-Request"], "1")
  assert.equal(seen.init.body, undefined)
  assert.equal(result.is_admin, false)
})

const fails = (init) =>
  new HttpRepoHealthApi({ fetcher: async () => new Response(JSON.stringify({ error: "x" }), init) }).unlockAdmin("pw")

test("refusals read as passwords and locks, not as repository errors", async () => {
  await assert.rejects(
    () => fails({ status: 403 }),
    (error) => error instanceof ApiRequestError && error.status === 403 && error.message === "Неверный пароль.",
  )
  await assert.rejects(
    () => fails({ status: 401 }),
    (error) => error.status === 401 && /войдите через Яндекс/.test(error.message),
  )
})

test("a lock shows the wait from Retry-After in minutes, never less than one", async () => {
  await assert.rejects(
    () => fails({ status: 429, headers: { "Retry-After": "870" } }),
    (error) => error.status === 429 && /15 мин/.test(error.message),
  )
  await assert.rejects(
    () => fails({ status: 429, headers: { "Retry-After": "5" } }),
    (error) => /1 мин/.test(error.message),
  )
  await assert.rejects(
    () => fails({ status: 429 }),
    (error) => /15 мин/.test(error.message),
    "a missing header falls back to the default lock",
  )
})

test("other failures keep the generic wording", async () => {
  await assert.rejects(
    () => fails({ status: 500 }),
    (error) => error.status === 500 && !/Неверный пароль/.test(error.message),
  )
})


test("the log reads the unlock, the lock and their reasons in Russian", () => {
  const row = (action, outcome, details) =>
    view.auditRow({
      id: 1, at: "2026-10-07T14:00:00Z", actor_kind: "user", actor: "ab12", action,
      target_kind: "route", target: "/admin/unlock", outcome, details,
    })

  assert.equal(row("admin.unlock", "ok", { via: "password" }).action, "Вход по паролю администратора")
  assert.equal(row("admin.unlock", "ok", { via: "password" }).note, "по паролю")
  assert.equal(row("admin.unlock", "denied", { reason: "wrong_password" }).note, "неверный пароль")
  assert.match(row("admin.unlock", "denied", { reason: "locked" }).note, /заблокирован/)
  assert.equal(row("admin.lock", "ok", { via: "password" }).action, "Снятие прав администратора")
  assert.equal(view.reasonLabel({ via: "something-new" }), "something-new", "an unknown value is shown as it is")
})
