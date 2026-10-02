import type { Content, TDocumentDefinitions } from "pdfmake/interfaces"
import type { AiReport, Recommendation, RepositorySummary } from "../types"
import { reportContent, reportFileName } from "./downloadReport"

const colors = {
  text: "#17191D",
  muted: "#6B7280",
  accent: "#315F9D",
  border: "#E5E7EB",
  surface: "#FFFFFF",
  track: "#EDF0F3",
  critical: "#B42332",
}

function card(content: Content[], marginBottom = 9): Content {
  return {
    table: {
      widths: ["*"],
      body: [
        [{ stack: content, margin: [13, 9, 13, 9], fillColor: colors.surface }],
      ],
      dontBreakRows: true,
    },
    layout: {
      hLineColor: () => colors.border,
      vLineColor: () => colors.border,
      hLineWidth: () => 0.7,
      vLineWidth: () => 0.7,
    },
    margin: [0, 0, 0, marginBottom],
  }
}

function section(title: string, description?: string): Content {
  return {
    stack: [
      { text: title, style: "sectionTitle" },
      ...(description ? [{ text: description, style: "muted" }] : []),
    ],
    margin: [0, 14, 0, 9],
  }
}

function scoreBar(score: number, width = 460): Content {
  const filled = (Math.max(0, Math.min(100, score)) * width) / 100
  return {
    svg: `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="7" viewBox="0 0 ${width} 7"><rect width="${width}" height="7" rx="3.5" fill="${colors.track}"/><rect width="${filled}" height="7" rx="3.5" fill="${colors.text}"/></svg>`,
    width,
    margin: [0, 7, 0, 0],
  }
}

export function buildPdfDocument(
  repository: RepositorySummary,
  recommendations: Recommendation[],
  ai?: AiReport,
): TDocumentDefinitions {
  const report = reportContent(repository, recommendations, ai)
  const muted = (text: string, marginTop = 0): Content => ({ text, style: "muted", margin: [0, marginTop, 0, 0] })
  const bullets = (items: string[], empty: string): Content =>
    card(items.length ? items.map((item, index) => muted(`• ${item}`, index === 0 ? 0 : 5)) : [muted(empty)])

  const content: Content[] = [
    { text: repository.fullName, style: "eyebrow", ...(report.link ? { link: report.link } : {}) },
    { text: repository.name, style: "title", margin: [0, 3, 0, 4] },
    {
      text: `${repository.language} · ${report.mode} анализ · последний анализ: ${report.gradedAt}`,
      style: "muted",
      margin: [0, 0, 0, 15],
    },
    {
      table: {
        widths: [176, "*"],
        body: [
          [
            {
              stack: [
                { text: "REPO HEALTH SCORE", style: "eyebrow" },
                {
                  text: [
                    {
                      text: repository.healthScore === null ? "Нет данных" : String(repository.healthScore),
                      fontSize: 36,
                      bold: true,
                    },
                    ...(repository.healthScore === null ? [] : [{ text: " /100", fontSize: 14, color: colors.muted }]),
                  ],
                },
                { text: report.level, color: colors.accent, bold: true, fontSize: 10 },
                ...(repository.healthScore === null ? [] : [scoreBar(repository.healthScore, 155)]),
              ],
              margin: [14, 13, 8, 13],
            },
            {
              stack: [
                { text: "SUMMARY AI", style: "eyebrow" },
                {
                  text: report.summary ?? "AI-сводка недоступна. Оценки и рекомендации ниже рассчитаны по правилам.",
                  style: "body",
                  margin: [0, 6, 0, 0],
                },
              ],
              margin: [8, 13, 14, 13],
            },
          ],
        ],
      },
      layout: {
        hLineColor: () => colors.border,
        vLineColor: () => colors.border,
        hLineWidth: () => 0.7,
        vLineWidth: () => 0.7,
      },
      margin: [0, 0, 0, 8],
    },
    section("Сильные стороны"),
    bullets(report.strengths, "Нет категорий с высокой оценкой."),
    section("Требует внимания"),
    bullets(report.weaknesses, "Слабых категорий не найдено."),
    section("Категории", "Оценки по доступным показателям и факты, на которых они основаны."),
  ]

  if (report.categories.length === 0) {
    content.push(card([muted("Оценки по категориям отсутствуют.")]))
  } else {
    const categories = Object.values(repository.categoryScores)
    const categoryCard = (index: number): Content => {
      const item = report.categories[index]
      const score = categories[index].score
      return card([
        {
          columns: [
            { text: item.name, style: "cardTitle" },
            { text: item.score, bold: true, color: score === null ? colors.muted : colors.text, alignment: "right" },
          ],
        },
        ...(item.description ? [muted(item.description, 4)] : []),
        ...(score === null ? [] : [scoreBar(score, 190)]),
        ...item.facts.map((fact, factIndex) => muted(fact, factIndex === 0 ? 6 : 2)),
      ])
    }
    for (let index = 0; index < report.categories.length; index += 2) {
      content.push({
        columns: [categoryCard(index), index + 1 < report.categories.length ? categoryCard(index + 1) : { text: "" }],
        columnGap: 9,
      })
    }
  }

  if (report.missing.length > 0) {
    content.push(
      section("Нет данных", "Категории не участвуют в оценке, веса остальных пересчитаны."),
      bullets(
        report.missing.map((item) => `${item.label} — ${item.reason}`),
        "",
      ),
    )
  }

  content.push(section("Рекомендации", "Отсортированы по приоритету."))
  if (report.recommendations.length === 0) {
    content.push(card([muted("Рекомендаций для этого репозитория нет.")]))
  } else {
    report.recommendations.forEach((item, index) => {
      content.push(
        card([
          { text: `${String(index + 1).padStart(2, "0")}   ${item.title}`, style: "cardTitle" },
          {
            text: item.priority,
            color: item.priority === "Критический приоритет" || item.priority === "Высокий приоритет" ? colors.critical : colors.muted,
            fontSize: 9,
            margin: [0, 3, 0, 4],
          },
          ...(item.location ? [muted(`Место: ${item.location}`)] : []),
          { text: item.description, style: "body" },
        ]),
      )
    })
  }

  return {
    pageSize: "A4",
    pageMargins: [36, 35, 36, 42],
    content,
    defaultStyle: { font: "Roboto", fontSize: 9, color: colors.text },
    styles: {
      eyebrow: {
        fontSize: 8,
        bold: true,
        color: colors.muted,
        characterSpacing: 0.7,
      },
      title: { fontSize: 22, bold: true, color: colors.text },
      sectionTitle: { fontSize: 14, bold: true, color: colors.text },
      cardTitle: { fontSize: 11, bold: true, color: colors.text },
      body: { fontSize: 9, color: colors.text, lineHeight: 1.3 },
      muted: { fontSize: 9, color: colors.muted, lineHeight: 1.3 },
    },
    footer: (page, pageCount) => ({
      text: `Repo Therapist · ${page} / ${pageCount}`,
      alignment: "right",
      margin: [36, 10, 36, 0],
      color: colors.muted,
      fontSize: 8,
    }),
    info: { title: `Repo Therapist — ${repository.fullName}` },
  }
}

export async function downloadPdfReport(
  repository: RepositorySummary,
  recommendations: Recommendation[],
  ai?: AiReport,
): Promise<void> {
  const [{ default: pdfMake }, { default: fonts }] = await Promise.all([
    import("pdfmake/build/pdfmake.js"),
    import("pdfmake/build/vfs_fonts.js"),
  ])
  pdfMake.addVirtualFileSystem(fonts)
  await pdfMake.createPdf(buildPdfDocument(repository, recommendations, ai)).download(reportFileName(repository, "pdf"))
}
