import { useState } from "react"
import { categoryLabel } from "../metrics"
import { Button, Card, SectionHeading } from "./Shell"

/** Categories graded in the public mode, which the category badges can show. */
export const BADGE_CATEGORIES = [
  "activity",
  "issues",
  "documentation",
  "code_health",
] as const

/**
 * Origin used in badge links: VITE_PUBLIC_URL (set per deployment, e.g. https://example.org) or,
 * when unset or invalid, the page's own origin.
 */
export function publicOrigin(configured: string | undefined, fallback: string): string {
  const value = (configured ?? "").trim()
  if (value) {
    try {
      const url = new URL(value)
      if (url.protocol === "http:" || url.protocol === "https:") return url.origin
    } catch {
      // Invalid value: use the page origin.
    }
  }
  return fallback
}

function repoPath(org: string, repo: string): string {
  return `${encodeURIComponent(org)}/${encodeURIComponent(repo)}`
}

/**
 * Markdown for the Repo Health badge (Gateway GET /badge/{org}/{repo}.svg). The badge shows the
 * stored public score, so it is offered for public analyses only.
 */
export function badgeMarkdown(
  origin: string,
  org: string,
  repo: string,
): string {
  const path = repoPath(org, repo)
  return `[![Repo Health](${origin}/api/badge/${path}.svg)](${origin}/repo/${path})`
}

/** Badge of one public category (Gateway GET /badge/{org}/{repo}/{category}.svg). */
export function categoryBadgeMarkdown(
  origin: string,
  org: string,
  repo: string,
  category: string,
): string {
  const path = repoPath(org, repo)
  return `[![Repo Health: ${category}](${origin}/api/badge/${path}/${category}.svg)](${origin}/repo/${path})`
}

/** README card with the score and the category bars (Gateway GET /widget/{org}/{repo}.svg). */
export function widgetMarkdown(
  origin: string,
  org: string,
  repo: string,
): string {
  const path = repoPath(org, repo)
  return `[![Repo Health](${origin}/api/widget/${path}.svg)](${origin}/repo/${path})`
}

/** Read-only JSON with the stored public score (Gateway GET /score/{org}/{repo}). */
export function scoreApiUrl(origin: string, org: string, repo: string): string {
  return `${origin}/api/score/${repoPath(org, repo)}`
}

/** shields.io endpoint badge that reads the Gateway's shields JSON (GET /badge/{org}/{repo}.json). */
export function shieldsMarkdown(
  origin: string,
  org: string,
  repo: string,
): string {
  const endpoint = `${origin}/api/badge/${repoPath(org, repo)}.json`
  return `[![Repo Health](https://img.shields.io/endpoint?url=${encodeURIComponent(endpoint)})](${origin}/repo/${repoPath(org, repo)})`
}

function SnippetRow({
  title,
  previewUrl,
  previewHeight,
  text,
}: {
  title: string
  previewUrl?: string
  previewHeight?: number
  text: string
}) {
  const [copied, setCopied] = useState(false)

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 2000)
    } catch {
      setCopied(false)
    }
  }

  return (
    <div className="badge-snippet__item">
      <strong className="badge-snippet__title">{title}</strong>
      {previewUrl ? (
        <img
          className={
            previewHeight
              ? "badge-snippet__preview badge-snippet__preview--card"
              : "badge-snippet__preview"
          }
          src={previewUrl}
          alt={title}
          height={previewHeight ?? 20}
          onError={(event) => {
            event.currentTarget.hidden = true
          }}
        />
      ) : null}
      <div className="badge-snippet__row">
        <code className="badge-snippet__code">{text}</code>
        <Button variant="secondary" onClick={copy}>
          {copied ? "Скопировано" : "Копировать"}
        </Button>
      </div>
    </div>
  )
}

export default function BadgeSnippet({
  org,
  repo,
}: {
  org: string
  repo: string
}) {
  // Absent outside the browser (server-side rendering in tests).
  const origin = publicOrigin(
    import.meta.env.VITE_PUBLIC_URL,
    typeof window === "undefined" ? "" : window.location.origin,
  )
  const path = repoPath(org, repo)
  // Collapsed by default: the snippets are a reference, and nothing below loads until asked for.
  const [open, setOpen] = useState(false)

  return (
    <section className="section-block">
      <SectionHeading
        title="Бейджи, виджет и API для README"
        description="Вставьте в README на SourceCraft: они показывают текущую публичную оценку и ведут на эту страницу. Приватные категории (Security, CI/CD) в них не попадают."
        action={
          <Button
            variant="secondary"
            aria-expanded={open}
            aria-controls="badge-snippets"
            onClick={() => setOpen((value) => !value)}
          >
            {open ? "Скрыть" : "Показать"}
            <span aria-hidden="true">{open ? "▴" : "▾"}</span>
          </Button>
        }
      />
      {open && (
        <div id="badge-snippets">
          <Card className="badge-snippet">
            <SnippetRow
              title="Оценка репозитория"
              previewUrl={`${origin}/api/badge/${path}.svg`}
              text={badgeMarkdown(origin, org, repo)}
            />
            <SnippetRow
              title="Виджет с категориями"
              previewUrl={`${origin}/api/widget/${path}.svg`}
              previewHeight={164}
              text={widgetMarkdown(origin, org, repo)}
            />
            {BADGE_CATEGORIES.map((category) => (
              <SnippetRow
                key={category}
                title={`Бейдж: ${categoryLabel(category)}`}
                previewUrl={`${origin}/api/badge/${path}/${category}.svg`}
                text={categoryBadgeMarkdown(origin, org, repo, category)}
              />
            ))}
            <SnippetRow
              title="Бейдж через shields.io"
              text={shieldsMarkdown(origin, org, repo)}
            />
            <SnippetRow
              title="JSON API (только чтение, без запуска оценки)"
              text={scoreApiUrl(origin, org, repo)}
            />
          </Card>
        </div>
      )}
    </section>
  )
}
