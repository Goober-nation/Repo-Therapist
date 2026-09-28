import assert from "node:assert/strict"
import { after, test } from "node:test"
import { createServer } from "vite"

const vite = await createServer({
  optimizeDeps: { noDiscovery: true },
  server: { middlewareMode: true, watch: null, hmr: { port: 24679 } },
  appType: "custom",
})
after(() => vite.close())
const { buildPdfDocument } = await vite.ssrLoadModule(
  "/src/reports/pdfReport.ts",
)

test("PDF export generates a real document with Cyrillic content and current categories", async () => {
  const repository = {
    id: "fixture-repository",
    name: "project",
    fullName: "team/project",
    language: "Go",
    healthScore: 82,
    trend: 0,
    likes: 5,
    lastActivityDaysAgo: 1,
    categoryScores: {
      Мяу: { score: 72, name: "Новая категория", description: "Описание" },
      absent: { score: null },
    },
  }
  const document = buildPdfDocument(repository, [
    {
      id: "recommendation-1",
      priority: "high",
      title: "Проверить безопасность",
      description: "Обновить зависимость.",
    },
  ])
  const structure = JSON.stringify(document.content)
  assert.match(structure, /Новая категория/)
  assert.match(structure, /Нет данных/)
  assert.match(structure, /Проверить безопасность/)

  const [{ default: pdfMake }, { default: fonts }] = await Promise.all([
    import("pdfmake/build/pdfmake.js"),
    import("pdfmake/build/vfs_fonts.js"),
  ])
  pdfMake.addVirtualFileSystem(fonts)
  const buffer = await pdfMake.createPdf(document).getBuffer()
  assert.equal(Buffer.from(buffer).subarray(0, 5).toString(), "%PDF-")
  assert.ok(buffer.length > 1000)
})
