import { useEffect, useRef, useState } from "react"
import { downloadReport } from "../reports/downloadReport"
import type { AiReport, Recommendation, RepositorySummary } from "../types"
import { Button } from "./Shell"

interface ReportDownloadMenuProps {
  repository: RepositorySummary
  recommendations: Recommendation[]
  ai?: AiReport
}

export default function ReportDownloadMenu({
  repository,
  recommendations,
  ai,
}: ReportDownloadMenuProps) {
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const rootRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return

    const closeOnOutsideClick = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setOpen(false)
        rootRef.current?.querySelector("button")?.focus()
      }
    }

    document.addEventListener("pointerdown", closeOnOutsideClick)
    document.addEventListener("keydown", closeOnEscape)
    return () => {
      document.removeEventListener("pointerdown", closeOnOutsideClick)
      document.removeEventListener("keydown", closeOnEscape)
    }
  }, [open])

  const downloadPdf = async () => {
    setOpen(false)
    setError("")
    setBusy(true)
    try {
      const { downloadPdfReport } = await import("../reports/pdfReport")
      await downloadPdfReport(repository, recommendations, ai)
    } catch (cause) {
      console.error("PDF report download failed", cause)
      setError("Не удалось скачать PDF. Попробуйте ещё раз.")
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="report-download" ref={rootRef}>
      <Button
        variant="secondary"
        aria-expanded={open}
        aria-controls="report-download-options"
        onClick={() => setOpen((value) => !value)}
        disabled={busy}
      >
        {busy ? "Готовим PDF…" : "Скачать отчёт"}
        <span aria-hidden="true">▾</span>
      </Button>
      {open && (
        <div className="report-download__options" id="report-download-options">
          <button
            type="button"
            onClick={() => {
              setOpen(false)
              setError("")
              downloadReport(repository, recommendations, ai)
            }}
          >
            Скачать MD
          </button>
          <button type="button" onClick={() => void downloadPdf()}>
            Скачать PDF
          </button>
        </div>
      )}
      {error && (
        <span className="report-download__error" role="alert">
          {error}
        </span>
      )}
    </div>
  )
}
