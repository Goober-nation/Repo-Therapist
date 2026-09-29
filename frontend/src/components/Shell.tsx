import type { ButtonHTMLAttributes, CSSProperties, ReactNode } from "react"
import type { Tone } from "../metrics"

export function Card({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <section className={`card ${className}`}>{children}</section>
}

export function Label({ children }: { children: ReactNode }) {
  return <div className="eyebrow">{children}</div>
}

export function Badge({ children, tone = "neutral" }: { children: ReactNode; tone?: Tone }) {
  return <span className={`badge badge--${tone}`}>{children}</span>
}

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "primary" | "secondary" | "quiet"
}

export function Button({ children, className = "", variant = "secondary", type = "button", ...props }: ButtonProps) {
  return (
    <button className={`button button--${variant} ${className}`} type={type} {...props}>
      {children}
    </button>
  )
}

export function ScoreBar({ score, label }: { score: number; label?: string }) {
  const barStyle = { width: `${score}%` } satisfies CSSProperties
  return (
    <div
      className="score-bar"
      role="progressbar"
      aria-label={label ?? "Оценка"}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={score}
    >
      <span className="score-bar__fill" style={barStyle} />
    </div>
  )
}

export function PageHeading({
  eyebrow,
  title,
  description,
  actions,
}: {
  eyebrow?: ReactNode
  title: string
  description?: ReactNode
  actions?: ReactNode
}) {
  return (
    <div className="page-heading">
      <div>
        {eyebrow && <div className="eyebrow page-heading__eyebrow">{eyebrow}</div>}
        <h1>{title}</h1>
        {description && <p className="page-heading__description">{description}</p>}
      </div>
      {actions && <div className="page-heading__actions">{actions}</div>}
    </div>
  )
}

export function SectionHeading({
  title,
  description,
  action,
}: {
  title: string
  description?: string
  action?: ReactNode
}) {
  return (
    <div className="section-heading">
      <div>
        <h2>{title}</h2>
        {description && <p>{description}</p>}
      </div>
      {action}
    </div>
  )
}

/** Language name with its SourceCraft color as a small dot, like GitHub's language label. */
export function LanguageLabel({ name, color }: { name: string; color?: string }) {
  return (
    <span className="language-label">
      {color && <span className="language-label__dot" style={{ backgroundColor: color }} aria-hidden="true" />}
      {name}
    </span>
  )
}
