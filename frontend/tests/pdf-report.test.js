import assert from "node:assert/strict"
import { after, test } from "node:test"
import { createServer } from "vite"

const vite = await createServer({
  optimizeDeps: { noDiscovery: true },
  server: { middlewareMode: true, watch: null, hmr: { port: 24679 } },
  appType: "custom",
})
after(() => vite.close())
const { buildPdfDocument } = await vite.ssrLoadModule("/src/mocks/pdfReport.ts")
const { DEMO_REPOSITORIES, DEMO_RECOMMENDATIONS } = await vite.ssrLoadModule(
  "/src/mocks/demoData.ts",
)

test("PDF export generates a real document with Cyrillic content and current categories", async () => {
  const repository = {
    ...DEMO_REPOSITORIES[0],
    categoryScores: {
      Мяу: { score: 72, name: "Новая категория", description: "Описание" },
      absent: { score: null },
    },
  }
  const document = buildPdfDocument(repository, DEMO_RECOMMENDATIONS)
  const structure = JSON.stringify(document.content)
  assert.match(structure, /Новая категория/)
  assert.match(structure, /Нет данных/)
  assert.match(structure, /Обновить уязвимые зависимости/)

  const [{ default: pdfMake }, { default: fonts }] = await Promise.all([
    import("pdfmake/build/pdfmake.js"),
    import("pdfmake/build/vfs_fonts.js"),
  ])
  pdfMake.addVirtualFileSystem(fonts)
  const buffer = await pdfMake.createPdf(document).getBuffer()
  assert.equal(Buffer.from(buffer).subarray(0, 5).toString(), "%PDF-")
  assert.ok(buffer.length > 1000)
})
