import { formatHours, ISSUE_NO_DATA, plural, REVIEW_NO_DATA } from "../analyticsText"
import {
  categoryLabel,
  categoryWeight,
  formatDateTime,
  formatMetricValue,
  formatShortDate,
  getLastActivityLabel,
  healthLevel,
  isGradeStale,
  isPrivateAnalysis,
  metricLabel,
  missingCategories,
  PRIORITY_LABELS,
  PRIORITY_RANK,
  signed,
  splitStrengths,
  STALE_GRADE_DAYS,
  type Tone,
} from "../metrics"
import type {
  AiReport,
  PositionExplanation,
  RepoAnalytics,
  Recommendation,
  RepositorySummary,
  ScorePoint,
} from "../types"

/** Data the report shows besides the grade itself; each part is optional and left out when absent. */
export interface ReportExtras {
  /** undefined / null = none loaded, the section is skipped. */
  analytics?: RepoAnalytics | null
  /** Place in the public ranking; only for a public grade. */
  position?: PositionExplanation | null
  /** Stored public gradings, oldest first. */
  history?: ScorePoint[] | null
}

/** One analytics card as plain text: a title and its lines; `muted` lines are caveats, not findings. */
export interface ReportBlock {
  title: string
  lines: string[]
  muted?: string[]
}

const percent = (value: number) => `${Math.round(value * 100)}%`

/** Plain-text version of the analytics cards (the page shows the same facts as widgets). */
export function analyticsBlocks(analytics: RepoAnalytics): ReportBlock[] {
  const { busFactor, ownership, reviews, issueReplies } = analytics
  const blocks: ReportBlock[] = []

  blocks.push(
    busFactor.available
      ? {
          title: "Фактор автобуса",
          lines: [
            `${busFactor.value} — половину коммитов за ${analytics.windowDays} дн. сделали ${busFactor.value} ${plural(busFactor.value, ["автор", "автора", "авторов"])} из ${busFactor.authors}${busFactor.value === 1 ? ". Проект сильно зависит от одного человека" : ""}.`,
            ...busFactor.topAuthors.map((author) => `${author.name}: ${percent(author.share)} коммитов`),
          ],
        }
      : { title: "Фактор автобуса", lines: [], muted: ["Нет данных: за период слишком мало коммитов, чтобы судить о распределении работы."] },
  )

  const reviewLines: string[] = []
  const reviewMuted: string[] = []
  if (reviews.available && reviews.unreviewedShare !== undefined) {
    reviewLines.push(
      `Влитых pull request'ов без ревью: ${percent(reviews.unreviewedShare)} (${reviews.unreviewed} из ${reviews.merged} среди последних ${reviews.sampled}).`,
    )
    if (reviews.medianMergeHours !== undefined) {
      reviewLines.push(`Медианное время до слияния: ${formatHours(reviews.medianMergeHours)} (приблизительно).`)
    }
  } else if (!reviews.bottlenecks.available) {
    reviewMuted.push(REVIEW_NO_DATA[reviews.reason ?? ""] ?? REVIEW_NO_DATA.unavailable)
  }
  if (reviews.bottlenecks.available) {
    reviewLines.push(
      reviews.bottlenecks.medianFirstReviewHours !== undefined
        ? `Медианное время до первого ревью: ${formatHours(reviews.bottlenecks.medianFirstReviewHours)} (по ${reviews.bottlenecks.reviewed} pull request'ам с ревью).`
        : "Время до первого ревью не посчитано: pull request'ов с ревью слишком мало.",
    )
    if (reviews.bottlenecks.open > 0) {
      reviewLines.push(
        `Открытых без ревью${reviews.bottlenecks.waitingDays > 0 ? ` дольше ${reviews.bottlenecks.waitingDays} дн.` : ""}: ${reviews.bottlenecks.waiting} из ${reviews.bottlenecks.open} открытых.`,
      )
    }
  } else if (reviews.available) {
    reviewMuted.push(REVIEW_NO_DATA[reviews.bottlenecks.reason ?? ""] ?? REVIEW_NO_DATA.unavailable)
  }
  blocks.push({ title: "Ревью pull request'ов", lines: reviewLines, ...(reviewMuted.length ? { muted: reviewMuted } : {}) })

  blocks.push(
    issueReplies.available && issueReplies.medianFirstReplyHours !== undefined
      ? {
          title: "Ответы на задачи",
          lines: [
            `Медианное время до первого ответа: ${formatHours(issueReplies.medianFirstReplyHours)} среди последних ${issueReplies.sampled} задач: отвечено ${issueReplies.replied}, без ответа ${issueReplies.unanswered}.`,
          ],
          muted: [
            `Ответ — комментарий не автора и не бота. Задача без ответа считается за время ожидания, не больше ${issueReplies.capDays} дн.`,
          ],
        }
      : { title: "Ответы на задачи", lines: [], muted: [ISSUE_NO_DATA[issueReplies.reason ?? ""] ?? ISSUE_NO_DATA.unavailable] },
  )

  const { codeowners } = ownership
  blocks.push(
    codeowners.present
      ? {
          title: "CODEOWNERS",
          lines: [
            codeowners.coveredShare !== undefined
              ? `${percent(codeowners.coveredShare)} изменённых файлов закреплены за владельцем (${codeowners.path}: ${codeowners.rules} ${plural(codeowners.rules, ["правило", "правила", "правил"])}).`
              : `Файл ${codeowners.path ?? ""} есть, но сравнить не с чем: за период нет изменённых файлов.`,
          ],
        }
      : { title: "CODEOWNERS", lines: ["Файл CODEOWNERS не найден: владельцы кода не закреплены."] },
  )

  if (ownership.available && (ownership.areas.length > 0 || ownership.files.length > 0)) {
    const areaLine = (row: { path: string; topOwner: string; topOwnerShare: number; owners: number }) =>
      `${row.path} — ${row.topOwner}, ${percent(row.topOwnerShare)} изменений (владельцев: ${row.owners})`
    blocks.push({
      title: "Карта владельцев кода",
      lines: [
        ...ownership.areas.slice(0, 8).map(areaLine),
        ...(ownership.files.length > 0 ? ["Самые изменяемые файлы:", ...ownership.files.slice(0, 5).map(areaLine)] : []),
      ],
    })
  } else {
    blocks.push({
      title: "Карта владельцев кода",
      lines: [],
      muted: ["Нет данных: за период слишком мало коммитов, чтобы построить карту."],
    })
  }

  return blocks
}

/** Everything a report shows, shared by the Markdown and PDF exports. */
export function reportContent(
  repository: RepositorySummary,
  recommendations: Recommendation[],
  ai?: AiReport,
  extras: ReportExtras = {},
) {
  const isPrivate = isPrivateAnalysis(repository)
  const { strengths, weaknesses } = splitStrengths(repository.categoryScores)
  const level = healthLevel(repository.healthScore)
  const points = (extras.history ?? []).slice(-8)
  const first = points.at(0)
  const last = points.at(-1)

  return {
    title: `Repo Therapist: отчёт о здоровье ${repository.fullName}`,
    link: repository.webUrl,
    generatedAt: formatDateTime(new Date().toISOString()) ?? "",
    gradedAt:
      (formatDateTime(repository.gradedAt) ?? "ещё не выполнялся") +
      (isGradeStale(repository.gradedAt) ? ` (оценка устарела: старше ${STALE_GRADE_DAYS} дней)` : ""),
    isPrivate,
    mode: isPrivate ? "приватный (все 6 категорий)" : "публичный",
    score: repository.healthScore === null ? "Нет данных" : `${repository.healthScore}/100`,
    level: repository.healthScore === null ? undefined : level.label,
    tone: level.tone as Tone,
    activity: repository.lastActivityDaysAgo < 0 ? undefined : getLastActivityLabel(repository.lastActivityDaysAgo),
    place: extras.position ? `${extras.position.place} из ${extras.position.total} в публичном рейтинге` : undefined,
    lowConfidence: repository.confidence?.low ? repository.confidence.reasons : undefined,
    summary: ai?.summary,
    modelUsed: ai?.modelUsed,
    strengths,
    weaknesses,
    categories: Object.entries(repository.categoryScores).map(([id, result]) => ({
      id,
      name: result.name ?? categoryLabel(id),
      score: result.score,
      scoreText: result.score === null ? "Нет данных" : `${result.score}/100`,
      weight: result.score === null ? undefined : categoryWeight(id, isPrivate, repository.categoryScores),
      tone: healthLevel(result.score).tone as Tone,
      description: result.description,
      note: result.method === "algorithmic" ? "Упрощённая оценка: проверено только наличие разделов." : undefined,
      // A category without a score has only "нет данных" facts: they are listed under "Нет данных" instead.
      facts: (result.score === null ? [] : result.facts ?? []).map(
        (fact) =>
          `${metricLabel(fact.id)}: ${formatMetricValue(fact.id, fact.value)}${
            fact.score === null ? "" : ` (оценка ${Math.round(fact.score * 100)}/100)`
          }`,
      ),
    })),
    missing: missingCategories(repository),
    recommendations: [...recommendations]
      .sort((a, b) => PRIORITY_RANK[a.priority] - PRIORITY_RANK[b.priority])
      .map((item) => ({
        title: item.title,
        priority: PRIORITY_LABELS[item.priority],
        urgent: item.priority === "critical" || item.priority === "high",
        description: item.description,
        location: item.location,
      })),
    analytics: extras.analytics ? analyticsBlocks(extras.analytics) : undefined,
    history:
      points.length > 0
        ? {
            points: points.map((point) => ({ date: formatShortDate(point.gradedAt) ?? point.gradedAt, score: point.score })),
            change: first && last && points.length > 1 ? signed(last.score - first.score) : undefined,
          }
        : undefined,
  }
}

export function reportFileName(repository: RepositorySummary, extension: "md" | "pdf") {
  const date = new Date().toISOString().slice(0, 10)
  return `${repository.fullName.replace(/[^\w.-]+/g, "-")}-repo-therapist-${date}.${extension}`
}

/** Escapes what Markdown would read as formatting in text that comes from repositories or models. */
export function mdEscape(text: string): string {
  return text.replace(/[\\`*_{}[\]<>|#]/g, "\\$&").replace(/\r?\n+/g, " ").trim()
}

/** A code span that survives backticks inside the text. */
function mdCode(text: string): string {
  const fence = "`".repeat((text.match(/`+/g) ?? []).reduce((longest, run) => Math.max(longest, run.length), 0) + 1)
  return `${fence}${text.replace(/\r?\n+/g, " ")}${fence}`
}

const bulletList = (items: string[], empty: string) =>
  items.length ? items.map((item) => `- ${mdEscape(item)}`) : [`*${empty}*`]

export function buildReport(
  repository: RepositorySummary,
  recommendations: Recommendation[] = [],
  ai?: AiReport,
  extras: ReportExtras = {},
): string {
  const report = reportContent(repository, recommendations, ai, extras)
  const lines: string[] = [`# ${mdEscape(report.title)}`, ""]

  if (report.link) lines.push(`Репозиторий: <${report.link}>`, "")

  lines.push(
    "| Показатель | Значение |",
    "|---|---|",
    `| Repo Health Score | **${report.score}**${report.level ? ` — ${report.level}` : ""} |`,
    `| Режим анализа | ${report.mode} |`,
    ...(repository.language === "Не указан" ? [] : [`| Язык | ${mdEscape(repository.language)} |`]),
    `| Последний анализ | ${report.gradedAt} |`,
    ...(report.activity ? [`| Последняя активность | ${report.activity} |`] : []),
    ...(report.place ? [`| Место | ${report.place} |`] : []),
    "",
  )

  if (report.lowConfidence) {
    lines.push(
      "> **Мало данных.** Оценка опирается на небольшой объём данных и может быть неточной. Порядок в рейтинге она не меняет.",
      ...report.lowConfidence.map((reason) => `> - ${mdEscape(reason)}`),
      "",
    )
  }

  if (report.summary) {
    lines.push(
      "## Summary AI",
      "",
      mdEscape(report.summary),
      ...(report.modelUsed ? ["", `*Сформировано моделью ${mdEscape(report.modelUsed)}.*`] : []),
      "",
    )
  }

  lines.push(
    "## Сильные стороны",
    "",
    ...bulletList(report.strengths, "Нет категорий с высокой оценкой."),
    "",
    "## Требует внимания",
    "",
    ...bulletList(report.weaknesses, "Слабых категорий не найдено."),
    "",
    "## Категории",
    "",
  )

  if (report.categories.length === 0) {
    lines.push("*Оценки по категориям отсутствуют.*", "")
  } else {
    lines.push("| Категория | Оценка | Вес в итоге |", "|---|---|---|")
    for (const item of report.categories) {
      lines.push(`| ${mdEscape(item.name)} | ${item.scoreText} | ${item.weight === undefined ? "—" : `${item.weight}%`} |`)
    }
    lines.push("")
    for (const item of report.categories) {
      if (!item.description && item.facts.length === 0 && !item.note) continue
      lines.push(`### ${mdEscape(item.name)} — ${item.scoreText}`, "")
      if (item.description) lines.push(mdEscape(item.description), "")
      if (item.note) lines.push(`*${item.note}*`, "")
      if (item.facts.length > 0) lines.push(...item.facts.map((fact) => `- ${mdEscape(fact)}`), "")
    }
  }

  if (report.missing.length > 0) {
    lines.push(
      "## Нет данных",
      "",
      "Эти категории не участвуют в итоговой оценке, веса остальных пересчитаны.",
      "",
      ...report.missing.map((item) => `- **${mdEscape(item.label)}** — ${mdEscape(item.reason)}`),
      "",
    )
  }

  lines.push("## Рекомендации", "")
  if (report.recommendations.length === 0) {
    lines.push("*Рекомендаций для этого репозитория нет.*", "")
  } else {
    report.recommendations.forEach((item, index) => {
      lines.push(
        `${index + 1}. **${mdEscape(item.title)}** — ${item.priority.toLocaleLowerCase()}${item.location ? `, ${mdCode(item.location)}` : ""}`,
        `   ${mdEscape(item.description)}`,
      )
    })
    lines.push("")
  }

  if (report.history) {
    lines.push(
      "## История оценки",
      "",
      "| Дата | Score |",
      "|---|---|",
      ...report.history.points.map((point) => `| ${point.date} | ${point.score} |`),
      "",
      ...(report.history.change ? [`Изменение за показанный период: ${report.history.change}.`, ""] : []),
    )
  }

  if (report.analytics) {
    lines.push(
      "## Углублённая аналитика",
      "",
      "Справочные показатели: на оценку не влияют, кроме времени ответа на задачи — оно входит в категорию Issues.",
      "",
    )
    for (const block of report.analytics) {
      lines.push(`### ${block.title}`, "", ...block.lines.map((line) => `- ${mdEscape(line)}`))
      if (block.muted) lines.push(...block.muted.map((line) => `*${mdEscape(line)}*`))
      lines.push("")
    }
  }

  lines.push("---", "", `Отчёт сформирован ${report.generatedAt} · Repo Therapist`, "")
  return lines.join("\n")
}

export function downloadReport(
  repository: RepositorySummary,
  recommendations: Recommendation[],
  ai?: AiReport,
  extras: ReportExtras = {},
): void {
  const file = new Blob([buildReport(repository, recommendations, ai, extras)], { type: "text/markdown;charset=utf-8" })
  const fileUrl = URL.createObjectURL(file)
  const link = document.createElement("a")
  link.href = fileUrl
  link.download = reportFileName(repository, "md")
  link.click()
  window.setTimeout(() => URL.revokeObjectURL(fileUrl), 0)
}
