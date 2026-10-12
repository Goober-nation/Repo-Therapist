package http

import (
	"fmt"
	"log"
	stdhttp "net/http"
	"strconv"
	"strings"
)

const (
	badgeLabel = "repo health"
	// badgeCacheSeconds keeps README views cheap while a new grade still shows up within minutes.
	badgeCacheSeconds = 300
	// badgeErrorCacheSeconds retries sooner when the score could not be read.
	badgeErrorCacheSeconds = 60
)

// badgeColors mirrors the frontend health levels (frontend/src/metrics.ts).
func badgeColor(score int32) string {
	switch {
	case score >= 80:
		return "#2e9e4f"
	case score >= 70:
		return "#58b368"
	case score >= 40:
		return "#dfb317"
	default:
		return "#e05d44"
	}
}

const badgeNoDataColor = "#9f9f9f"

// GetBadge serves GET /badge/{org}/{repo}.svg: a shields-style SVG with the repo's stored public
// score, for embedding in a SourceCraft README. It always answers 200 with an image, so a README
// never shows a broken picture; unknown, ungraded or unreadable repos get a gray "n/a" badge.
func (h *Handler) GetBadge(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	org := r.PathValue("org")
	if jsonRepo, isJSON := strings.CutSuffix(r.PathValue("repo"), ".json"); isJSON && org != "" && jsonRepo != "" {
		h.getBadgeJSON(w, r, org, jsonRepo)
		return
	}

	repo, ok := strings.CutSuffix(r.PathValue("repo"), ".svg")
	if !ok || org == "" || repo == "" {
		stdhttp.NotFound(w, r)
		return
	}

	value, color, cacheSeconds := "n/a", badgeNoDataColor, badgeCacheSeconds
	score, err := h.service.GetPublicScore(r.Context(), org, repo)
	switch {
	case err != nil && !isNotFound(err):
		log.Printf("badge %s/%s: %v", org, repo, err)
		cacheSeconds = badgeErrorCacheSeconds
	case err == nil && score != nil:
		value, color = strconv.Itoa(int(*score)), badgeColor(*score)
	}

	setImageHeaders(w, cacheSeconds)
	_, _ = w.Write([]byte(renderBadge(badgeLabel, value, color)))
}

// renderBadge draws a flat two-part badge. Widths are estimated from character classes at
// 11px Verdana, which is close enough for short ASCII labels and numbers.
func renderBadge(label, value, color string) string {
	lw, vw := textWidth(label)+12, textWidth(value)+12
	total := lw + vw

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="%s: %s">`+
		`<title>%s: %s</title>`+
		`<linearGradient id="s" x2="0" y2="100%%"><stop offset="0" stop-color="#bbb" stop-opacity=".1"/><stop offset="1" stop-opacity=".1"/></linearGradient>`+
		`<clipPath id="r"><rect width="%d" height="20" rx="3" fill="#fff"/></clipPath>`+
		`<g clip-path="url(#r)"><rect width="%d" height="20" fill="#555"/><rect x="%d" width="%d" height="20" fill="%s"/><rect width="%d" height="20" fill="url(#s)"/></g>`+
		`<g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" font-size="11">`+
		`<text x="%d" y="15" fill="#010101" fill-opacity=".3">%s</text><text x="%d" y="14">%s</text>`+
		`<text x="%d" y="15" fill="#010101" fill-opacity=".3">%s</text><text x="%d" y="14">%s</text>`+
		`</g></svg>`,
		total, label, value,
		label, value,
		total,
		lw, lw, vw, color, total,
		lw/2, label, lw/2, label,
		lw+vw/2, value, lw+vw/2, value,
	)
}

func textWidth(text string) int {
	width := 0.0
	for _, r := range text {
		switch {
		case r == ' ':
			width += 3.9
		case r == '/' || r == 'i' || r == 'l' || r == 't' || r == 'r' || r == 'f':
			width += 4.4
		case r >= '0' && r <= '9':
			width += 7
		case r >= 'A' && r <= 'Z', r == 'm', r == 'w':
			width += 8.4
		default:
			width += 6.6
		}
	}
	return int(width + 0.5)
}
