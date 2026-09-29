import { useState } from "react"
import { Button, Card, SectionHeading } from "./Shell"

/**
 * Markdown for the Repo Health badge (Gateway GET /badge/{org}/{repo}.svg). The badge shows the
 * stored public score, so it is offered for public analyses only.
 */
export function badgeMarkdown(origin: string, org: string, repo: string): string {
  const path = `${encodeURIComponent(org)}/${encodeURIComponent(repo)}`
  return `[![Repo Health](${origin}/api/badge/${path}.svg)](${origin}/repo/${path})`
}

export default function BadgeSnippet({ org, repo }: { org: string; repo: string }) {
  // Absent outside the browser (server-side rendering in tests).
  const origin = typeof window === "undefined" ? "" : window.location.origin
  const markdown = badgeMarkdown(origin, org, repo)
  const [copied, setCopied] = useState(false)

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(markdown)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 2000)
    } catch {
      setCopied(false)
    }
  }

  return (
    <section className="section-block">
      <SectionHeading
        title="Бейдж для README"
        description="Вставьте в README на SourceCraft: бейдж показывает текущую публичную оценку и ведёт на эту страницу."
      />
      <Card className="badge-snippet">
        <img
          className="badge-snippet__preview"
          src={`${origin}/api/badge/${encodeURIComponent(org)}/${encodeURIComponent(repo)}.svg`}
          alt="Repo Health"
          height={20}
          onError={(event) => {
            event.currentTarget.hidden = true
          }}
        />
        <div className="badge-snippet__row">
          <code className="badge-snippet__code">{markdown}</code>
          <Button variant="secondary" onClick={copy}>
            {copied ? "Скопировано" : "Копировать"}
          </Button>
        </div>
      </Card>
    </section>
  )
}
