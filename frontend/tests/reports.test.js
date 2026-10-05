import assert from "node:assert/strict"
import { after, test } from "node:test"
import { createServer } from "vite"

const vite = await createServer({
  optimizeDeps: { noDiscovery: true },
  server: { middlewareMode: true, watch: null, hmr: false },
  appType: "custom",
})
after(() => vite.close())

const { buildReport, mdEscape, reportContent } = await vite.ssrLoadModule("/src/reports/downloadReport.ts")
const { buildPdfDocument } = await vite.ssrLoadModule("/src/reports/pdfReport.ts")

const repository = (over = {}) => ({
  id: "x",
  orgSlug: "org",
  repoSlug: "demo",
  name: "demo",
  fullName: "org/demo",
  language: "Go",
  lastActivityDaysAgo: 3,
  webUrl: "https://sourcecraft.dev/org/demo",
  healthScore: 71,
  gradedAt: "2026-10-04T10:00:00Z",
  analysisMode: "private",
  categoryScores: {
    activity: { score: 82, trend: null, facts: [{ id: "commits_per_week", value: 4.25, score: 0.9 }] },
    issues: { score: null, trend: null, facts: [{ id: "share_resolved", value: null, score: null }] },
    documentation: { score: 64, trend: null, method: "algorithmic" },
  },
  ...over,
})

const recommendations = [
  { id: "1", priority: "low", title: "Низкая", description: "d1" },
  { id: "2", priority: "critical", title: "Критичная", description: "d2", location: "go.mod" },
  { id: "3", priority: "high", title: "Высокая", description: "d3" },
]

const analytics = {
  windowDays: 180,
  busFactor: { available: true, value: 1, commits: 100, authors: 4, topAuthors: [{ name: "Анна", share: 0.72 }] },
  reviews: { available: false, reason: "imported", sampled: 0, merged: 0, unreviewed: 0, bottlenecks: { available: false, reason: "unavailable", reviewed: 0, open: 0, waiting: 0, waitingDays: 7 } },
  issueReplies: { available: true, sampled: 25, replied: 14, unanswered: 11, medianFirstReplyHours: 57, capDays: 30 },
  ownership: {
    available: true,
    areas: [{ path: "src", files: 4, lines: 100, topOwner: "Анна", topOwnerShare: 0.8, owners: 2 }],
    subareas: [],
    files: [{ path: "src/main.go", lines: 90, topOwner: "Анна", topOwnerShare: 0.9, owners: 1 }],
    codeowners: { present: false, rules: 0 },
  },
}

test("the Markdown report lists categories with weights, sorts recommendations by priority and notes the simplified documentation grade", () => {
  const md = buildReport(repository(), recommendations)
  assert.match(md, /\| Repo Health Score \| \*\*71\/100\*\* — /)
  assert.match(md, /\| Режим анализа \| приватный \(все 6 категорий\) \|/)
  assert.match(md, /\| Activity \| 82\/100 \| \d+% \|/)
  assert.match(md, /\| Issues \| Нет данных \| — \|/)
  assert.match(md, /\*Упрощённая оценка: проверено только наличие разделов\.\*/)

  const order = ["Критичная", "Высокая", "Низкая"].map((title) => md.indexOf(`**${title}**`))
  assert.ok(order.every((index) => index >= 0) && order[0] < order[1] && order[1] < order[2], "critical, high, low")
  assert.match(md, /`go\.mod`/)
  assert.doesNotMatch(md, /undefined|NaN|\[object/)
})

test("a category without a score keeps its \"нет данных\" facts out of the category details", () => {
  const md = buildReport(repository(), [])
  assert.doesNotMatch(md, /Доля решённых issues: нет данных/)
  assert.match(md, /## Нет данных/)
})

test("history, place, low confidence and analytics appear only when given", () => {
  const plain = buildReport(repository(), [])
  for (const absent of ["История оценки", "Углублённая аналитика", "Мало данных", "| Место |"]) {
    assert.doesNotMatch(plain, new RegExp(absent.replace(/[|]/g, "\\$&")), absent)
  }

  const history = [60, 64, 70].map((score, index) => ({ gradedAt: `2026-09-2${index}T10:00:00Z`, score, categories: [], lowConfidence: null }))
  const full = buildReport(
    repository({ confidence: { low: true, reasons: ["Мало кода."] } }),
    [],
    undefined,
    { analytics, history, position: { place: 3, total: 27, score: 71, categories: [] } },
  )
  assert.match(full, /\| Место \| 3 из 27 в публичном рейтинге \|/)
  assert.match(full, /> \*\*Мало данных\.\*\*/)
  assert.match(full, /> - Мало кода\./)
  assert.match(full, /## История оценки[\s\S]*Изменение за показанный период: \+10\./)
  assert.match(full, /Фактор автобуса[\s\S]*Проект сильно зависит от одного человека/)
  assert.match(full, /Нет данных: pull request'ы созданы почти одновременно/)
  assert.match(full, /Медианное время до первого ответа: 2 дн\./)
  assert.match(full, /src — Анна, 80% изменений/)
})

test("an ungraded repository gives a readable report without repeated or invented data", () => {
  const empty = repository({ healthScore: null, gradedAt: undefined, lastActivityDaysAgo: -1, categoryScores: {}, webUrl: undefined })
  const md = buildReport(empty, [])
  assert.match(md, /\| Repo Health Score \| \*\*Нет данных\*\* \|/)
  assert.doesNotMatch(md, /Последняя активность/)
  assert.match(md, /Рекомендаций для этого репозитория нет/)
  assert.doesNotMatch(md, /Репозиторий: </)

  const content = reportContent(empty, [])
  assert.equal(content.level, undefined, "no level next to \"Нет данных\"")
  assert.equal(content.activity, undefined)
})

test("text from repositories and models cannot break the Markdown structure", () => {
  assert.equal(mdEscape("a|b *c* _d_ `e` <f> # [g]"), "a\\|b \\*c\\* \\_d\\_ \\`e\\` \\<f\\> \\# \\[g\\]")
  assert.equal(mdEscape("line1\nline2\r\nline3"), "line1 line2 line3")

  const md = buildReport(
    repository({ language: "C++ | Obj", categoryScores: { a: { score: 50, trend: null, name: "Имя | с * знаками" } } }),
    [{ id: "z", priority: "high", title: "T | *x*", description: "d\n\n# fake heading", location: "a`b`.go" }],
    { summary: "# заголовок\n| a | b |" },
  )
  assert.doesNotMatch(md, /^# заголовок/m)
  assert.doesNotMatch(md, /^# fake heading/m)
  assert.match(md, /\| Имя \\\| с \\\* знаками \| 50\/100 \|/)
  assert.match(md, /``a`b`\.go``/, "a code span that survives backticks")
})

test("the PDF drops glyphs Roboto cannot draw and still builds for extreme input", async () => {
  const long = "Очень длинное описание. ".repeat(400)
  const doc = buildPdfDocument(
    repository({ name: "name 😀", categoryScores: { a: { score: 100, trend: null, name: "Кат егория‑один", description: long } } }),
    Array.from({ length: 40 }, (_, index) => ({ id: String(index), priority: ["low", "medium", "high", "critical"][index % 4], title: `Рек ${index} 😀`, description: index === 3 ? long.repeat(6) : "Коротко." })),
    { summary: long },
    { analytics, history: [{ gradedAt: "2026-09-20T10:00:00Z", score: 50, categories: [], lowConfidence: null }] },
  )

  const structure = JSON.stringify(doc.content)
  assert.doesNotMatch(structure, /\u{1F600}| |‑/u)
  assert.match(structure, /Кат егория-один/)
  assert.match(structure, /Углублённая аналитика/)
  assert.match(structure, /История оценки/)

  const [{ default: pdfMake }, { default: fonts }] = await Promise.all([import("pdfmake/build/pdfmake.js"), import("pdfmake/build/vfs_fonts.js")])
  pdfMake.addVirtualFileSystem(fonts)
  const buffer = Buffer.from(await pdfMake.createPdf(doc).getBuffer())
  assert.equal(buffer.subarray(0, 5).toString(), "%PDF-")
})

test("the PDF has no score bar or level for an ungraded repository, and no sections it has no data for", async () => {
  const empty = repository({ healthScore: null, gradedAt: undefined, lastActivityDaysAgo: -1, categoryScores: {}, analysisMode: "public" })
  const structure = JSON.stringify(buildPdfDocument(empty, []).content)
  assert.doesNotMatch(structure, /Углублённая аналитика|История оценки|Мало данных/)
  assert.match(structure, /Нет данных/)
  assert.match(structure, /Оценки по категориям отсутствуют/)
  assert.match(structure, /Рекомендаций для этого репозитория нет/)
})
