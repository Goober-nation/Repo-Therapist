import type { Content, TDocumentDefinitions } from "pdfmake/interfaces"
import {
  DEFAULT_REPOSITORY_ID,
  DEMO_ATTENTION_ITEMS,
  DEMO_SCORE_HISTORY,
  getHealthLabel,
  getLastActivityLabel,
} from "./demoData"
import type { Recommendation, RepositorySummary } from "../types"

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

function historyChart(): Content {
  const width = 460
  const height = 92
  const points = DEMO_SCORE_HISTORY.map((entry, index) => ({
    x: 12 + (index / (DEMO_SCORE_HISTORY.length - 1)) * (width - 24),
    y: height - 10 - ((entry.score - 65) / 35) * (height - 20),
  }))
  const path = points
    .map(
      (point, index) =>
        `${index === 0 ? "M" : "L"}${point.x.toFixed(1)} ${point.y.toFixed(1)}`,
    )
    .join(" ")
  const grid = [70, 80, 90, 100]
    .map((score) => {
      const y = height - 10 - ((score - 65) / 35) * (height - 20)
      return `<line x1="0" x2="${width}" y1="${y}" y2="${y}" stroke="${colors.border}" stroke-width="1"/>`
    })
    .join("")
  const dots = points
    .map(
      (point) =>
        `<circle cx="${point.x.toFixed(1)}" cy="${point.y.toFixed(1)}" r="3" fill="${colors.accent}"/>`,
    )
    .join("")
  return card([
    { text: "Repo Health Score во времени", style: "cardTitle" },
    { text: "Последние шесть месяцев", style: "muted", margin: [0, 2, 0, 11] },
    {
      svg: `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}">${grid}<path d="${path}" fill="none" stroke="${colors.accent}" stroke-width="2.5"/>${dots}</svg>`,
      width,
    },
    {
      columns: DEMO_SCORE_HISTORY.map((entry) => ({
        stack: [
          { text: entry.month, color: colors.muted, fontSize: 8 },
          { text: String(entry.score), bold: true, fontSize: 9 },
        ],
        alignment: "center" as const,
      })),
      margin: [0, 5, 0, 0],
    },
  ])
}

export function buildPdfDocument(
  repository: RepositorySummary,
  recommendations: Recommendation[],
): TDocumentDefinitions {
  const isDemo = repository.id === DEFAULT_REPOSITORY_ID
  const categories = Object.entries(repository.categoryScores)
  const content: Content[] = [
    { text: repository.fullName, style: "eyebrow" },
    { text: repository.name, style: "title", margin: [0, 3, 0, 4] },
    {
      text: `${repository.language} · открытый репозиторий · активность ${getLastActivityLabel(repository.lastActivityDaysAgo).toLocaleLowerCase()}`,
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
                      text: String(repository.healthScore),
                      fontSize: 36,
                      bold: true,
                    },
                    { text: " /100", fontSize: 14, color: colors.muted },
                  ],
                },
                {
                  text: getHealthLabel(repository.healthScore),
                  color: colors.accent,
                  bold: true,
                  fontSize: 10,
                },
                {
                  text: `${
                    repository.trend > 0 ? "+" : ""
                  }${repository.trend} за месяц`,
                  style: "muted",
                  margin: [0, 3, 0, 0],
                },
                scoreBar(repository.healthScore, 155),
              ],
              margin: [14, 13, 8, 13],
            },
            {
              stack: [
                { text: "КРАТКИЙ РАЗБОР", style: "eyebrow" },
                {
                  text:
                    repository.healthScore >= 80
                      ? "Проект в хорошем состоянии"
                      : "Есть области для улучшения",
                  style: "cardTitle",
                  margin: [0, 6, 0, 7],
                },
                {
                  text: isDemo
                    ? "Активность проекта и CI/CD выглядят стабильно. Безопасность — главный фактор, который снижает общий результат."
                    : "Оценка собрана по доступным показателям. Категории ниже показывают разбивку.",
                  style: "body",
                },
                ...(isDemo
                  ? [
                      {
                        text: `Возможный результат после исправлений: ${repository.healthScore} до 89`,
                        color: colors.accent,
                        bold: true,
                        fontSize: 9,
                        margin: [
                          0, 9, 0, 0,
                        ] as [number, number, number, number],
                      },
                    ]
                  : []),
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
    section("Категории", "Оценки по доступным показателям."),
  ]

  if (categories.length === 0) {
    content.push(
      card([{ text: "Оценки по категориям отсутствуют.", style: "muted" }]),
    )
  } else {
    const categoryCard = ([id, result]: typeof categories[number]): Content =>
      card([
        {
          columns: [
            { text: result.name ?? id, style: "cardTitle" },
            {
              text:
                result.score === null ? "Нет данных" : `${result.score}/100`,
              bold: true,
              color: result.score === null ? colors.muted : colors.text,
              alignment: "right",
            },
          ],
        },
        ...(result.description
          ? [
              {
                text: result.description,
                style: "muted",
                margin: [0, 4, 0, 0] as [number, number, number, number],
              },
            ]
          : []),
        ...(result.score === null ? [] : [scoreBar(result.score, 190)]),
      ])
    for (let index = 0; index < categories.length; index += 2) {
      content.push({
        columns: [
          categoryCard(categories[index]),
          ...(categories[index + 1]
            ? [categoryCard(categories[index + 1])]
            : [{ text: "" }]),
        ],
        columnGap: 9,
      })
    }
  }

  content.push(
    section(
      "Требует внимания",
      "Факты и проблемы, на которые стоит посмотреть в первую очередь.",
    ),
  )
  if (isDemo) {
    for (const item of DEMO_ATTENTION_ITEMS) {
      content.push(
        card([
          {
            text: `${
              item.severity === "critical" ? "Критично" : "Рекомендация"
            } · ${item.category}`,
            color:
              item.severity === "critical" ? colors.critical : colors.accent,
            fontSize: 9,
            bold: true,
          },
          { text: item.title, style: "cardTitle", margin: [0, 5, 0, 4] },
          { text: item.description, style: "body" },
          {
            text: `Потенциальный эффект: +${item.impact}`,
            style: "muted",
            margin: [0, 5, 0, 0],
          },
        ]),
      )
    }
  } else {
    content.push(
      card([
        {
          text: "Детали проблем для этого репозитория пока недоступны.",
          style: "muted",
        },
      ]),
    )
  }

  content.push(
    section(
      "Рекомендации",
      "Действия отсортированы по ожидаемому влиянию на здоровье проекта.",
    ),
  )
  if (recommendations.length === 0) {
    content.push(
      card([
        {
          text: "Подробные рекомендации для этого репозитория пока недоступны.",
          style: "muted",
        },
      ]),
    )
  } else {
    for (const [index, item] of recommendations.entries()) {
      content.push(
        card([
          {
            text: `${String(index + 1).padStart(2, "0")}   ${item.title}`,
            style: "cardTitle",
          },
          {
            text:
              item.priority === "high"
                ? "Высокий приоритет"
                : "Средний приоритет",
            color: item.priority === "high" ? colors.critical : colors.muted,
            fontSize: 9,
            margin: [0, 3, 0, 4],
          },
          { text: item.description, style: "body" },
          {
            text: `Score: ${item.currentScore} до ${item.projectedScore}`,
            color: colors.accent,
            bold: true,
            fontSize: 9,
            margin: [0, 5, 0, 0],
          },
        ]),
      )
    }
  }

  if (isDemo) {
    content.push({ text: "", margin: [0, 2, 0, 0] }, historyChart())
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
      text: `Repo Health · ${page} / ${pageCount}`,
      alignment: "right",
      margin: [36, 10, 36, 0],
      color: colors.muted,
      fontSize: 8,
    }),
    info: { title: `Repo Health — ${repository.fullName}` },
  }
}

export async function downloadPdfReport(
  repository: RepositorySummary,
  recommendations: Recommendation[],
): Promise<void> {
  const [{ default: pdfMake }, { default: fonts }] = await Promise.all([
    import("pdfmake/build/pdfmake.js"),
    import("pdfmake/build/vfs_fonts.js"),
  ])
  pdfMake.addVirtualFileSystem(fonts)
  await pdfMake
    .createPdf(buildPdfDocument(repository, recommendations))
    .download(`${repository.id}-repo-health.pdf`)
}
