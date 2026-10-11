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

const view = await vite.ssrLoadModule("/src/settingsView.ts")
const audit = await vite.ssrLoadModule("/src/auditView.ts")
const config = await vite.ssrLoadModule("/src/publicConfig.ts")
const { HttpRepoHealthApi } = await vite.ssrLoadModule("/src/api/client.ts")
const { default: SettingsPanel } = await vite.ssrLoadModule("/src/components/SettingsPanel.tsx")

test("settings calls use the admin routes, the guard header and a JSON body", async () => {
  const calls = []
  const api = new HttpRepoHealthApi({
    fetcher: async (url, init) => {
      calls.push([init.method ?? "GET", url, init.headers?.["X-Admin-Request"], init.body])
      return new Response(JSON.stringify({ settings: [], processes: [] }), { status: 200 })
    },
  })
  await api.getSettings()
  await api.setSetting("gateway.rate_limit", "90")
  await api.resetSetting("gateway.rate_limit")
  assert.deepEqual(calls, [
    ["GET", "/api/admin/settings", undefined, undefined],
    ["PUT", "/api/admin/settings/gateway.rate_limit", "1", '{"value":"90"}'],
    ["DELETE", "/api/admin/settings/gateway.rate_limit", "1", undefined],
  ])
})

test("a refused setting reads in Russian", async () => {
  const api = new HttpRepoHealthApi({
    fetcher: async () => new Response(JSON.stringify({ error: "x", code: "setting_env_locked" }), { status: 409 }),
  })
  await assert.rejects(api.setSetting("gateway.rate_limit", "90"), /окружени/)
})

test("client-side validation mirrors the aggregator's bounds", () => {
  const int = { type: "int", min: "1", max: "100" }
  assert.equal(view.validateSetting(int, "50"), "")
  assert.match(view.validateSetting(int, "0"), /Меньше/)
  assert.match(view.validateSetting(int, "101"), /Больше/)
  assert.match(view.validateSetting(int, "1.5"), /целое/)
  assert.match(view.validateSetting(int, " "), /Введите/)

  const duration = { type: "duration", min: "1s", max: "24h" }
  assert.equal(view.validateSetting(duration, "1h30m"), "")
  assert.match(view.validateSetting(duration, "25h"), /Больше/)
  assert.match(view.validateSetting(duration, "soon"), /длительность/)

  assert.equal(view.validateSetting({ type: "enum", values: ["auto", "llm"] }, "llm"), "")
  assert.match(view.validateSetting({ type: "enum", values: ["auto", "llm"] }, "x"), /Допустимо/)
  assert.equal(view.validateSetting({ type: "bool" }, "false"), "")
  assert.equal(view.durationSeconds("1h30m"), 5400)
})

test("values and sources read in Russian", () => {
  assert.equal(view.formatValue({ type: "bool" }, "true"), "да")
  assert.equal(view.formatValue({ type: "int" }, null), "по умолчанию в коде")
  assert.equal(view.sourceLabel("env").label, "Окружение")
  assert.equal(view.shownValue({ override: "5", file_value: "60" }), "5")
  assert.equal(view.shownValue({ override: null, file_value: null }), null)
  assert.equal(view.boundsLabel({ min: "1", max: "9" }), "от 1 до 9")
})

test("the audit log shows a settings change as old → new", () => {
  assert.equal(audit.settingChangeNote({ old: "60", new: "90", old_source: "file" }), "60 → 90")
  assert.equal(audit.settingChangeNote({ old: null, new: "90" }), "по умолчанию → 90")
  assert.equal(audit.settingChangeNote({ reason: "x" }), "")
  const row = audit.auditRow({
    id: 1, at: "2026-10-11T00:00:00Z", actor_kind: "admin", actor: "ab", action: "settings.set",
    target_kind: "setting", target: "gateway.rate_limit", outcome: "denied",
    details: { reason: "env_locked", new: "90" },
  })
  assert.equal(row.action, "Изменение настройки")
  assert.equal(row.target, "Настройка: gateway.rate_limit")
  assert.match(row.note, /90 · задано в окружении/)
})

test("public config falls back to defaults and is fetched once", async () => {
  assert.deepEqual(config.fromResponse({ public_url: " https://x ", poll_timeout_ms: -1 }), {
    publicUrl: "https://x", yandexClientId: "", devAuthBypass: false, pollTimeoutMs: 15 * 60_000,
  })
  let calls = 0
  const fetcher = async () => {
    calls++
    return new Response(JSON.stringify({ public_url: "", yandex_client_id: "id", dev_auth_bypass: true, poll_timeout_ms: 1000 }))
  }
  const [a, b] = await Promise.all([config.loadPublicConfig(fetcher), config.loadPublicConfig(fetcher)])
  assert.equal(calls, 1)
  assert.equal(a, b)
  assert.equal(config.publicConfig().pollTimeoutMs, 1000)
  assert.equal(config.publicConfig().yandexClientId, "id")
})

test("the settings panel renders its loading state", () => {
  const api = new HttpRepoHealthApi({ fetcher: async () => new Response("{}") })
  const html = renderToStaticMarkup(createElement(SettingsPanel, { api }))
  assert.match(html, /Настройки сервисов/)
  assert.match(html, /Загружаем/)
})

test("layoutSettings pairs related keys into sections and keeps unknown keys", () => {
  const s = (key, group) => ({ key, group, title: `T ${key}` })
  const layout = view.layoutSettings([
    s("gateway.rate_limit_window", "rate_limits"),
    s("aggregator.grading_request_limit", "rate_limits"),
    s("gateway.rate_limit", "rate_limits"),
    s("gateway.new_key", "rate_limits"),
    s("llm.llm_priority_threshold", "llm"),
  ])
  assert.deepEqual(layout.map((g) => g.group), ["rate_limits", "llm"])
  const [limits] = layout
  assert.deepEqual(limits.sections.map((x) => x.title), ["Весь сайт, с одного IP", "Оценки по запросу, на весь сервис", "Прочее"])
  assert.deepEqual(limits.sections[0].items.map((i) => i.setting.key), ["gateway.rate_limit", "gateway.rate_limit_window"])
  assert.equal(limits.sections[2].items[0].label, "T gateway.new_key")
})
