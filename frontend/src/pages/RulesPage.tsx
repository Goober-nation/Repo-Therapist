import { Marked } from "marked"
import { useMemo } from "react"
import { Card, PageHeading } from "../components/Shell"

/** Grading docs copied from docs/grading by scripts/sync-rules.mjs. */
const files = import.meta.glob("../rules/*.md", { query: "?raw", import: "default", eager: true }) as Record<
  string,
  string
>

const ORDER = ["00-overview", "activity", "issues", "code-health", "ci-cd", "security", "documentation"]
const OVERVIEW_LABEL = "Общие принципы"

interface RuleSection {
  slug: string
  title: string
  markdown: string
}

const sections: RuleSection[] = Object.entries(files)
  .map(([path, markdown]) => {
    const slug = path.replace(/^.*\/(.+)\.md$/, "$1")
    const heading = markdown.match(/^#\s+(.+)$/m)?.[1]?.trim() ?? slug
    return { slug, title: slug === "00-overview" ? OVERVIEW_LABEL : heading, markdown }
  })
  .sort((a, b) => rank(a.slug) - rank(b.slug))

function rank(slug: string) {
  const index = ORDER.indexOf(slug)
  return index === -1 ? ORDER.length : index
}

const slugs = new Set(sections.map((section) => section.slug))

// Links between the grading docs open the matching section; links to other repo docs are not
// part of the site, so they render as plain text.
const marked = new Marked({
  renderer: {
    link({ href, tokens }) {
      const text = this.parser.parseInline(tokens)
      const doc = href.match(/^(?:\.\/)?([\w-]+)\.md(#.*)?$/)
      if (doc && slugs.has(doc[1])) return `<a href="/rules/${doc[1]}" data-rules-link="${doc[1]}">${text}</a>`
      if (/^https?:\/\//.test(href)) return `<a href="${href}" target="_blank" rel="noreferrer">${text}</a>`
      return text
    },
  },
})

export default function RulesPage({
  section,
  onSectionChange,
}: {
  section?: string
  onSectionChange: (slug: string) => void
}) {
  const current = sections.find((item) => item.slug === section) ?? sections[0]
  const html = useMemo(() => (current ? (marked.parse(current.markdown) as string) : ""), [current])

  if (!current) {
    return (
      <Card className="empty-state">
        <strong>Правила оценки пока не добавлены</strong>
      </Card>
    )
  }

  return (
    <div className="rules-page">
      <PageHeading
        eyebrow="Как считается Repo Health Score"
        title="Правила оценки"
        description="Полное описание каждой категории: какие данные собираются, как считаются метрики и что означает «нет данных»."
      />
      <div className="rules-layout">
        <nav className="rules-nav" aria-label="Разделы правил">
          {sections.map((item) => (
            <button
              key={item.slug}
              type="button"
              className={`rules-nav__link ${item.slug === current.slug ? "rules-nav__link--active" : ""}`}
              aria-current={item.slug === current.slug ? "page" : undefined}
              onClick={() => onSectionChange(item.slug)}
            >
              {item.title}
            </button>
          ))}
        </nav>
        <Card className="rules-content">
          <article
            className="markdown"
            onClick={(event) => {
              const link = (event.target as HTMLElement).closest<HTMLAnchorElement>("a[data-rules-link]")
              if (!link) return
              event.preventDefault()
              onSectionChange(link.dataset.rulesLink!)
            }}
            dangerouslySetInnerHTML={{ __html: html }}
          />
        </Card>
      </div>
    </div>
  )
}
