import type { Content, TableCell, TDocumentDefinitions } from "pdfmake/interfaces"
import type { Tone } from "../metrics"
import type { AiReport, Recommendation, RepositorySummary } from "../types"
import { reportContent, reportFileName, type ReportExtras } from "./downloadReport"

const colors = {
  text: "#17191D",
  muted: "#6B7280",
  accent: "#315F9D",
  border: "#E3E6EB",
  surface: "#FFFFFF",
  soft: "#F6F7F9",
  track: "#E9ECF0",
  good: "#247548",
  warning: "#956315",
  warningSoft: "#FBF3E5",
  critical: "#B42332",
}

const TONE: Record<Tone, string> = {
  good: colors.good,
  warning: colors.warning,
  critical: colors.critical,
  neutral: colors.muted,
}

/** Roboto has no emoji or pictographs: they would print as empty boxes, so they are dropped. */
const UNSUPPORTED_GLYPHS = /[\p{Extended_Pictographic}\u200B-\u200D\u2060\uFEFF\uFE0E\uFE0F]/gu
/** Exotic spaces and hyphens that models like to produce and Roboto cannot draw. */
const EXOTIC_SPACES = /[\u00A0\u1680\u2000-\u200A\u202F\u205F\u3000]/g
const EXOTIC_HYPHENS = /[\u2010\u2011\u2012]/g

function withoutUnsupportedGlyphs<T>(value: T): T {
  if (typeof value === "string") {
    return value.replace(UNSUPPORTED_GLYPHS, "").replace(EXOTIC_SPACES, " ").replace(EXOTIC_HYPHENS, "-").replace(/ {2,}/g, " ") as T
  }
  if (Array.isArray(value)) return value.map(withoutUnsupportedGlyphs) as T
  if (value && typeof value === "object") {
    return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, withoutUnsupportedGlyphs(item)])) as T
  }
  return value
}

/** Shortens a name for the page footer, which must stay on one line. */
const shorten = (text: string, limit: number) => (text.length > limit ? `${text.slice(0, limit - 1)}…` : text)

const PAGE_WIDTH = 595.28
const MARGIN = 40
const CONTENT_WIDTH = PAGE_WIDTH - MARGIN * 2
const COLUMN_GAP = 10
const HALF = (CONTENT_WIDTH - COLUMN_GAP) / 2

/** Cards taller than about a page may not be kept in one piece. */
const KEEP_TOGETHER_LIMIT = 900

const thinBorders = {
  hLineColor: () => colors.border,
  vLineColor: () => colors.border,
  hLineWidth: () => 0.7,
  vLineWidth: () => 0.7,
}

function card(content: Content[], options: { fill?: string; keep?: boolean; marginBottom?: number } = {}): Content {
  return {
    table: {
      widths: ["*"],
      body: [[{ stack: content, margin: [12, 10, 12, 10], fillColor: options.fill ?? colors.surface }]],
      dontBreakRows: options.keep ?? true,
    },
    layout: thinBorders,
    margin: [0, 0, 0, options.marginBottom ?? 8],
  }
}

const NO_BORDER: [boolean, boolean, boolean, boolean] = [false, false, false, false]
const ALL_BORDERS: [boolean, boolean, boolean, boolean] = [true, true, true, true]

/**
 * Cards two to a row. Each row is its own table, so both cards of a row are equally tall and a row moves
 * to the next page as a whole (pdfmake columns would split across pages and lose or misplace cards).
 */
function cardRows(cards: Content[][], options: { keep?: (index: number) => boolean; marginBottom?: number } = {}): Content[] {
  const rows: Content[] = []
  for (let index = 0; index < cards.length; index += 2) {
    const cell = (content: Content[] | undefined): TableCell =>
      content
        ? { stack: content, margin: [12, 10, 12, 10], border: ALL_BORDERS, fillColor: colors.surface }
        : { text: "", border: NO_BORDER }
    rows.push({
      table: {
        widths: ["*", COLUMN_GAP, "*"],
        body: [[cell(cards[index]), { text: "", border: NO_BORDER }, cell(cards[index + 1])]],
        dontBreakRows: (options.keep?.(index) ?? true) && (options.keep?.(index + 1) ?? true),
      },
      layout: thinBorders,
      margin: [0, 0, 0, options.marginBottom ?? 10],
    })
  }
  return rows
}

const headings = new WeakSet<object>()

function section(title: string, description?: string): Content {
  const node = {
    stack: [
      { text: title, style: "sectionTitle" },
      ...(description ? [{ text: description, style: "muted", margin: [0, 2, 0, 0] } as Content] : []),
    ],
    margin: [0, 16, 0, 8],
  } as Content
  headings.add(node as object)
  return node
}

/** Glues every section heading to the block after it, so a heading is never left alone at a page bottom. */
function keepHeadingsWithContent(content: Content[]): Content[] {
  const merged: Content[] = []
  for (let index = 0; index < content.length; index += 1) {
    const item = content[index]
    const next = content[index + 1]
    if (typeof item === "object" && headings.has(item as object) && next !== undefined) {
      merged.push({ stack: [item, next], unbreakable: true } as Content)
      index += 1
    } else {
      merged.push(item)
    }
  }
  return merged
}

function bar(score: number, width: number, color: string): Content {
  const filled = Math.max(0, Math.min(100, score)) * (width / 100)
  return {
    svg: `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="6" viewBox="0 0 ${width} 6"><rect width="${width}" height="6" rx="3" fill="${colors.track}"/>${filled > 0 ? `<rect width="${Math.max(filled, 6)}" height="6" rx="3" fill="${color}"/>` : ""}</svg>`,
    width,
    margin: [0, 6, 0, 0],
  }
}

/** Score history as a small line chart: a 0–100 grid, the line and a dot per grading. */
function historyChart(scores: number[], width: number, height = 96): Content {
  const x = (index: number) => ((index + 0.5) * width) / scores.length
  const y = (score: number) => 6 + ((100 - Math.max(0, Math.min(100, score))) * (height - 12)) / 100
  const grid = [0, 25, 50, 75, 100]
    .map((level) => `<line x1="0" x2="${width}" y1="${y(level)}" y2="${y(level)}" stroke="${colors.track}" stroke-width="0.8"/>`)
    .join("")
  const line =
    scores.length > 1
      ? `<polyline fill="none" stroke="${colors.accent}" stroke-width="1.6" points="${scores.map((score, index) => `${x(index)},${y(score)}`).join(" ")}"/>`
      : ""
  const dots = scores
    .map((score, index) => `<circle cx="${x(index)}" cy="${y(score)}" r="2.8" fill="${colors.surface}" stroke="${colors.accent}" stroke-width="1.4"/>`)
    .join("")
  return {
    svg: `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}">${grid}${line}${dots}</svg>`,
    width,
  }
}

export function buildPdfDocument(
  repository: RepositorySummary,
  recommendations: Recommendation[],
  ai?: AiReport,
  extras: ReportExtras = {},
): TDocumentDefinitions {
  const report = withoutUnsupportedGlyphs(reportContent(repository, recommendations, ai, extras))
  const toneColor = TONE[report.tone]
  const muted = (text: string, marginTop = 0): Content => ({ text, style: "muted", margin: [0, marginTop, 0, 0] })
  const bullets = (label: string, items: string[], empty: string): Content[] => [
    { text: label, style: "cardLabel" },
    ...(items.length ? items.map((item, index) => ({ text: `•  ${item}`, style: "body", margin: [0, index === 0 ? 0 : 4, 0, 0] }) as Content) : [muted(empty)]),
  ]

  const content: Content[] = [
    {
      text: [{ text: "REPO THERAPIST", color: colors.accent }, { text: "  ·  ОТЧЁТ О ЗДОРОВЬЕ РЕПОЗИТОРИЯ", color: colors.muted }],
      style: "eyebrow",
    },
    { text: withoutUnsupportedGlyphs(repository.name), style: "title", margin: [0, 6, 0, 2] },
    {
      text: withoutUnsupportedGlyphs(repository.fullName),
      style: "repoLink",
      ...(report.link ? { link: report.link } : {}),
    },
    {
      text: [...(repository.language === "Не указан" ? [] : [withoutUnsupportedGlyphs(repository.language)]), report.mode + " анализ", `последний анализ: ${report.gradedAt}`, ...(report.activity ? [`активность: ${report.activity.toLocaleLowerCase()}`] : [])].join("   ·   "),
      style: "muted",
      margin: [0, 5, 0, 14],
    },
    {
      table: {
        widths: [172, "*"],
        body: [
          [
            {
              stack: [
                { text: "REPO HEALTH SCORE", style: "eyebrow" },
                {
                  text: [
                    {
                      text: repository.healthScore === null ? "Нет данных" : String(repository.healthScore),
                      fontSize: repository.healthScore === null ? 20 : 40,
                      bold: true,
                      color: repository.healthScore === null ? colors.muted : colors.text,
                    },
                    ...(repository.healthScore === null ? [] : [{ text: " / 100", fontSize: 13, color: colors.muted }]),
                  ],
                  margin: [0, 4, 0, 0],
                },
                ...(report.level ? [{ text: report.level, color: toneColor, bold: true, fontSize: 11, margin: [0, 2, 0, 0] } as Content] : []),
                ...(repository.healthScore === null ? [] : [bar(repository.healthScore, 146, toneColor)]),
                ...(report.place ? [{ text: `Место: ${report.place}`, style: "muted", margin: [0, 8, 0, 0] } as Content] : []),
              ],
              margin: [14, 12, 10, 12],
              fillColor: colors.soft,
            },
            {
              stack: [
                { text: "SUMMARY AI", style: "eyebrow" },
                {
                  text: report.summary ?? "AI-сводка недоступна. Оценки и рекомендации ниже рассчитаны по правилам.",
                  style: "body",
                  margin: [0, 6, 0, 0],
                },
                ...(report.summary && report.modelUsed ? [muted(`Сформировано моделью ${report.modelUsed}`, 8)] : []),
              ],
              margin: [12, 12, 14, 12],
            },
          ],
        ],
        dontBreakRows: (report.summary?.length ?? 0) < 1500,
      },
      layout: thinBorders,
      margin: [0, 0, 0, 8],
    },
  ]

  if (report.lowConfidence) {
    content.push(
      card(
        [
          { text: "Мало данных", bold: true, color: colors.warning, fontSize: 10 },
          muted("Оценка опирается на небольшой объём данных и может быть неточной. Порядок в рейтинге она не меняет.", 3),
          ...report.lowConfidence.map((reason) => muted(`•  ${reason}`, 3)),
        ],
        { fill: colors.warningSoft },
      ),
    )
  }

  content.push(
    section("Сильные и слабые стороны"),
    ...cardRows([
      bullets("СИЛЬНЫЕ СТОРОНЫ", report.strengths, "Нет категорий с высокой оценкой."),
      bullets("ТРЕБУЕТ ВНИМАНИЯ", report.weaknesses, "Слабых категорий не найдено."),
    ]),
    section("Категории", "Оценка по каждой категории и факты, на которых она основана. «Вес» — доля категории в итоговом Score."),
  )

  if (report.categories.length === 0) {
    content.push(card([muted("Оценки по категориям отсутствуют.")]))
  } else {
    const cards = report.categories.map((item): Content[] => {
      const color = TONE[item.tone]
      return [
        {
          columns: [
            { text: item.name, style: "cardTitle", width: "*" },
            { text: item.scoreText, bold: true, color: item.score === null ? colors.muted : color, alignment: "right", width: "auto" },
          ],
          columnGap: 6,
        },
        ...(item.weight !== undefined ? [muted(`вес в итоге ${item.weight}%`, 2)] : []),
        ...(item.score === null ? [] : [bar(item.score, HALF - 40, color)]),
        ...(item.description ? [muted(item.description, 7)] : []),
        ...(item.note ? [{ text: item.note, fontSize: 8, italics: true, color: colors.warning, margin: [0, 5, 0, 0] } as Content] : []),
        ...item.facts.map((fact, factIndex) => ({ text: fact, fontSize: 8.5, color: colors.muted, lineHeight: 1.25, margin: [0, factIndex === 0 ? 7 : 2, 0, 0] }) as Content),
      ]
    })
    content.push(...cardRows(cards, { keep: (index) => (report.categories[index]?.facts.length ?? 0) < 14 }))
  }

  if (report.missing.length > 0) {
    content.push(
      section("Нет данных", "Эти категории не участвуют в итоговой оценке, веса остальных пересчитаны. Отсутствие данных не считается плохим результатом."),
      card(
        report.missing.map(
          (item, index) =>
            ({
              text: [{ text: item.label, bold: true }, { text: ` — ${item.reason}`, color: colors.muted }],
              style: "body",
              margin: [0, index === 0 ? 0 : 5, 0, 0],
            }) as Content,
        ),
      ),
    )
  }

  content.push(section("Рекомендации", "Отсортированы по приоритету."))
  if (report.recommendations.length === 0) {
    content.push(card([muted("Рекомендаций для этого репозитория нет.")]))
  } else {
    report.recommendations.forEach((item, index) => {
      const accent = item.urgent ? colors.critical : colors.muted
      content.push({
        table: {
          widths: [3, "*"],
          body: [
            [
              { text: "", fillColor: accent, border: [false, false, false, false] },
              {
                stack: [
                  {
                    columns: [
                      { text: `${String(index + 1).padStart(2, "0")}   ${item.title}`, style: "cardTitle", width: "*" },
                      { text: item.priority, color: accent, fontSize: 8, bold: true, alignment: "right", width: "auto", margin: [8, 2, 0, 0] },
                    ],
                  },
                  ...(item.location ? [{ text: `Место: ${item.location}`, fontSize: 8.5, color: colors.muted, margin: [0, 3, 0, 0] } as Content] : []),
                  { text: item.description, style: "body", margin: [0, 5, 0, 0] },
                ],
                margin: [11, 9, 11, 9],
              },
            ],
          ],
          dontBreakRows: item.description.length + item.title.length < KEEP_TOGETHER_LIMIT,
        },
        layout: { ...thinBorders, vLineWidth: (lineIndex: number) => (lineIndex === 1 ? 0 : 0.7) },
        margin: [0, 0, 0, 6],
      })
    })
  }

  if (report.history) {
    const { points, change } = report.history
    content.push(
      section("История оценки", "Публичные оценки репозитория, от старых к новым."),
      card([
        historyChart(
          points.map((point) => point.score),
          CONTENT_WIDTH - 34,
        ),
        {
          columns: points.map((point) => ({
            stack: [
              { text: String(point.score), bold: true, fontSize: 10, alignment: "center" },
              { text: point.date, fontSize: 7.5, color: colors.muted, alignment: "center" },
            ],
          })),
          margin: [0, 6, 0, 0],
        },
        ...(change ? [muted(`Изменение за показанный период: ${change}.`, 8)] : []),
      ]),
    )
  }

  if (report.analytics) {
    content.push(
      section(
        "Углублённая аналитика",
        "Справочные показатели: на оценку не влияют, кроме времени ответа на задачи — оно входит в категорию Issues.",
      ),
    )
    content.push(
      ...cardRows(
        report.analytics.map((block): Content[] => [
          { text: block.title, style: "cardTitle" },
          ...block.lines.map((line, index) => ({ text: line, style: "body", margin: [0, index === 0 ? 5 : 3, 0, 0] }) as Content),
          ...(block.muted ?? []).map((line, index) => ({ text: line, style: "muted", margin: [0, block.lines.length === 0 && index === 0 ? 5 : 4, 0, 0] }) as Content),
        ]),
      ),
    )
  }

  return {
    pageSize: "A4",
    pageMargins: [MARGIN, 42, MARGIN, 46],
    content: keepHeadingsWithContent(content),
    background: () => ({
      canvas: [{ type: "rect", x: 0, y: 0, w: PAGE_WIDTH, h: 5, color: colors.accent }],
    }),
    defaultStyle: { font: "Roboto", fontSize: 9, color: colors.text },
    styles: {
      eyebrow: { fontSize: 7.5, bold: true, color: colors.muted, characterSpacing: 0.8 },
      title: { fontSize: 24, bold: true, color: colors.text },
      repoLink: { fontSize: 11, color: colors.accent },
      sectionTitle: { fontSize: 14, bold: true, color: colors.text },
      cardTitle: { fontSize: 10.5, bold: true, color: colors.text },
      cardLabel: { fontSize: 8, bold: true, color: colors.muted, characterSpacing: 0.5, margin: [0, 0, 0, 4] },
      body: { fontSize: 9, color: colors.text, lineHeight: 1.3 },
      muted: { fontSize: 9, color: colors.muted, lineHeight: 1.3 },
    },
    footer: (page, pageCount) => ({
      columns: [
        { text: `${shorten(withoutUnsupportedGlyphs(repository.fullName), 44)} · отчёт от ${report.generatedAt}`, color: colors.muted, fontSize: 7.5 },
        { text: `${page} / ${pageCount}`, alignment: "right", color: colors.muted, fontSize: 7.5 },
      ],
      margin: [MARGIN, 14, MARGIN, 0],
    }),
    info: { title: `Repo Therapist — ${withoutUnsupportedGlyphs(repository.fullName)}`, creator: "Repo Therapist" },
  }
}

export async function downloadPdfReport(
  repository: RepositorySummary,
  recommendations: Recommendation[],
  ai?: AiReport,
  extras: ReportExtras = {},
): Promise<void> {
  const [{ default: pdfMake }, { default: fonts }] = await Promise.all([
    import("pdfmake/build/pdfmake.js"),
    import("pdfmake/build/vfs_fonts.js"),
  ])
  pdfMake.addVirtualFileSystem(fonts)
  await pdfMake
    .createPdf(buildPdfDocument(repository, recommendations, ai, extras))
    .download(reportFileName(repository, "pdf"))
}
