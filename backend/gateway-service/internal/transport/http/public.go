package http

import (
	"fmt"
	"html"
	"log"
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"

	"gateway-service/internal/domain"
)

// publicCategory is a category exposed by the public badges, the widget and the score API. Only
// the categories graded in the public mode are listed; Security and CI/CD are private-only.
type publicCategory struct {
	ID string
	// BadgeLabel is the ASCII label of the category badge, WidgetLabel the one on the widget card.
	BadgeLabel  string
	WidgetLabel string
}

var publicCategories = []publicCategory{
	{ID: "activity", BadgeLabel: "activity", WidgetLabel: "Активность"},
	{ID: "issues", BadgeLabel: "issues", WidgetLabel: "Issues"},
	{ID: "documentation", BadgeLabel: "documentation", WidgetLabel: "Документация"},
	{ID: "code_health", BadgeLabel: "code health", WidgetLabel: "Состояние кода"},
}

func findPublicCategory(id string) (publicCategory, bool) {
	for _, category := range publicCategories {
		if category.ID == id {
			return category, true
		}
	}

	return publicCategory{}, false
}

// healthLevel mirrors the frontend levels (frontend/src/metrics.ts): a stable machine-readable
// key for the API and the Russian label for the widget.
func healthLevel(score *int32) (key, label string) {
	switch {
	case score == nil:
		return "no_data", "Нет данных"
	case *score >= 80:
		return "excellent", "Отличное состояние"
	case *score >= 70:
		return "healthy", "Здоровый"
	case *score >= 40:
		return "attention", "Требует внимания"
	default:
		return "at_risk", "Под риском"
	}
}

// shieldsColor maps a score to a named shields.io colour, matching badgeColor's thresholds.
func shieldsColor(score int32) string {
	switch {
	case score >= 80:
		return "brightgreen"
	case score >= 70:
		return "green"
	case score >= 40:
		return "yellow"
	default:
		return "red"
	}
}

// setPublicCORS lets any web page read a public, unauthenticated GET route.
func setPublicCORS(w stdhttp.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
}

// setImageHeaders are the headers shared by every SVG served for README embedding.
func setImageHeaders(w stdhttp.ResponseWriter, cacheSeconds int) {
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", cacheSeconds))
	// Embedded on sourcecraft.dev, so the image must be loadable cross-origin.
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	setPublicCORS(w)
}

// publicSummaryOrNone reads a repo's public summary for an image or shields response. Unknown,
// ungraded and unreadable repos all yield ok=false, so a README never shows a broken picture;
// only an unreadable repo gets the short cache, to be retried sooner.
func (h *Handler) publicSummaryOrNone(r *stdhttp.Request, org, repo string) (summary domain.PublicSummary, ok bool, cacheSeconds int) {
	summary, err := h.service.GetPublicSummary(r.Context(), org, repo)
	switch {
	case err != nil && !isNotFound(err):
		log.Printf("public summary %s/%s: %v", org, repo, err)
		return domain.PublicSummary{}, false, badgeErrorCacheSeconds
	case err != nil:
		return domain.PublicSummary{}, false, badgeCacheSeconds
	}

	return summary, true, badgeCacheSeconds
}

// shieldsEndpoint is the JSON a shields.io "endpoint" badge reads.
type shieldsEndpoint struct {
	SchemaVersion int    `json:"schemaVersion"`
	Label         string `json:"label"`
	Message       string `json:"message"`
	Color         string `json:"color"`
}

// getBadgeJSON serves GET /badge/{org}/{repo}.json for shields.io's endpoint badge. Like the SVG
// badge it always answers 200: unknown or ungraded repos get a light-grey "n/a".
func (h *Handler) getBadgeJSON(w stdhttp.ResponseWriter, r *stdhttp.Request, org, repo string) {
	body := shieldsEndpoint{SchemaVersion: 1, Label: badgeLabel, Message: "n/a", Color: "lightgrey"}

	summary, ok, cacheSeconds := h.publicSummaryOrNone(r, org, repo)
	if ok && summary.Score != nil {
		body.Message, body.Color = strconv.Itoa(int(*summary.Score)), shieldsColor(*summary.Score)
	}

	setPublicCORS(w)
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", cacheSeconds))
	writeJSON(w, stdhttp.StatusOK, body)
}

// GetCategoryBadge serves GET /badge/{org}/{repo}/{category}.svg: the same flat badge as the
// overall one, for a single public category. A category without data shows a grey "n/a", which is
// not a bad score. Unknown categories (including the private-only ones) are 404.
func (h *Handler) GetCategoryBadge(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	org, repo := r.PathValue("org"), r.PathValue("repo")
	categoryID, hasSuffix := strings.CutSuffix(r.PathValue("category"), ".svg")
	category, known := findPublicCategory(categoryID)
	if !hasSuffix || !known || org == "" || repo == "" {
		stdhttp.NotFound(w, r)
		return
	}

	value, color := "n/a", badgeNoDataColor
	summary, ok, cacheSeconds := h.publicSummaryOrNone(r, org, repo)
	if ok {
		for _, entry := range summary.Categories {
			if entry.ID == category.ID && entry.Score != nil {
				value, color = strconv.Itoa(int(*entry.Score)), badgeColor(*entry.Score)
			}
		}
	}

	setImageHeaders(w, cacheSeconds)
	_, _ = w.Write([]byte(renderBadge(badgeLabel+": "+category.BadgeLabel, value, color)))
}

// GetWidget serves GET /widget/{org}/{repo}.svg: a self-contained card with the score, the
// health level and the public category scores, for a README.
func (h *Handler) GetWidget(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	org := r.PathValue("org")
	repo, ok := strings.CutSuffix(r.PathValue("repo"), ".svg")
	if !ok || org == "" || repo == "" {
		stdhttp.NotFound(w, r)
		return
	}

	summary, found, cacheSeconds := h.publicSummaryOrNone(r, org, repo)
	if !found {
		summary = domain.PublicSummary{}
	}

	setImageHeaders(w, cacheSeconds)
	_, _ = w.Write([]byte(renderWidget(org+"/"+repo, summary)))
}

// scoreCategory is one category in the score API response; no_data distinguishes "not
// available" from a real score of 0.
type scoreCategory struct {
	ID     string `json:"id"`
	Score  *int32 `json:"score"`
	NoData bool   `json:"no_data"`
}

type scoreResponse struct {
	Org         string          `json:"org"`
	Repo        string          `json:"repo"`
	Name        string          `json:"name"`
	WebURL      string          `json:"web_url"`
	Status      string          `json:"status"`
	Score       *int32          `json:"score"`
	Level       string          `json:"level"`
	Categories  []scoreCategory `json:"categories"`
	MissingData []string        `json:"missing_data"`
	GradedAt    *time.Time      `json:"graded_at"`
	// LowConfidence marks a grade that rests on little data; null when unknown. It never changes
	// the score or the ranking order.
	LowConfidence     *bool                      `json:"low_confidence"`
	ConfidenceReasons []confidenceReasonResponse `json:"confidence_reasons"`
}

// GetScore serves GET /score/{org}/{repo}: the stored public Repo Health Score as JSON. Unlike
// /grading it is read-only: it never starts a grading, so a repo that has not been graded yet
// answers 200 with status NO_DATA instead of PENDING.
func (h *Handler) GetScore(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	setPublicCORS(w)

	org, repo := r.PathValue("org"), r.PathValue("repo")
	if org == "" || repo == "" {
		stdhttp.NotFound(w, r)
		return
	}

	summary, err := h.service.GetScore(r.Context(), clientIP(r), org, repo)
	if err != nil {
		writeError(w, err)
		return
	}

	level, _ := healthLevel(summary.Score)
	status := statusNoData
	if summary.Score != nil {
		status = statusFinished
	}

	categories := make([]scoreCategory, 0, len(summary.Categories))
	for _, category := range summary.Categories {
		categories = append(categories, scoreCategory{ID: category.ID, Score: category.Score, NoData: category.Score == nil})
	}

	missing := summary.MissingData
	if missing == nil {
		missing = []string{}
	}

	low, reasons := mapConfidenceResponse(summary.Confidence)

	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", scoreCacheSeconds))
	writeJSON(w, stdhttp.StatusOK, scoreResponse{
		Org:         org,
		Repo:        repo,
		Name:        summary.Name,
		WebURL:      summary.WebURL,
		Status:      status,
		Score:       summary.Score,
		Level:       level,
		Categories:  categories,
		MissingData: missing,
		GradedAt:    summary.GradedAt,

		LowConfidence:     low,
		ConfidenceReasons: reasons,
	})
}

const scoreCacheSeconds = 60

type historyCategory struct {
	ID     string `json:"id"`
	Score  *int32 `json:"score"`
	NoData bool   `json:"no_data"`
}

type historyPointResponse struct {
	GradedAt   time.Time         `json:"graded_at"`
	Score      int32             `json:"score"`
	Categories []historyCategory `json:"categories"`
	// LowConfidence is null when the flag was unknown for that grading.
	LowConfidence *bool `json:"low_confidence"`
}

type historyResponse struct {
	Org    string                 `json:"org"`
	Repo   string                 `json:"repo"`
	Status string                 `json:"status"`
	Points []historyPointResponse `json:"points"`
}

// GetHistory serves GET /history/{org}/{repo}: the stored public score history, oldest first. Like
// /score it is read-only, public and CORS-open; a repo without history answers 200 with NO_DATA.
func (h *Handler) GetHistory(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	setPublicCORS(w)

	org, repo := r.PathValue("org"), r.PathValue("repo")
	if org == "" || repo == "" {
		stdhttp.NotFound(w, r)
		return
	}

	points, err := h.service.GetScoreHistory(r.Context(), clientIP(r), org, repo)
	if err != nil {
		writeError(w, err)
		return
	}

	response := historyResponse{Org: org, Repo: repo, Status: statusNoData, Points: make([]historyPointResponse, 0, len(points))}
	if len(points) > 0 {
		response.Status = statusFinished
	}

	for _, point := range points {
		categories := make([]historyCategory, 0, len(point.Categories))
		for _, category := range point.Categories {
			categories = append(categories, historyCategory{ID: category.ID, Score: category.Score, NoData: category.Score == nil})
		}

		response.Points = append(response.Points, historyPointResponse{
			GradedAt:      point.GradedAt,
			Score:         point.Score,
			Categories:    categories,
			LowConfidence: point.LowConfidence,
		})
	}

	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", scoreCacheSeconds))
	writeJSON(w, stdhttp.StatusOK, response)
}

type regradeStatusResponse struct {
	Enabled         bool       `json:"enabled"`
	IntervalSeconds int64      `json:"interval_seconds"`
	NextRunAt       *time.Time `json:"next_run_at"`
	Running         bool       `json:"running"`
	StartedAt       *time.Time `json:"started_at"`
	LastFinishedAt  *time.Time `json:"last_finished_at"`
	Total           int32      `json:"total"`
	Done            int32      `json:"done"`
}

// GetRegradeStatus serves GET /regrade-status: whether the scheduled public regrade is on, when it
// runs next and its progress while it runs. Read-only, public and CORS-open; not cached, because
// the progress changes while a run is going.
func (h *Handler) GetRegradeStatus(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	setPublicCORS(w)

	status, err := h.service.GetRegradeStatus(r.Context(), clientIP(r))
	if err != nil {
		writeError(w, err)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, stdhttp.StatusOK, regradeStatusResponse(status))
}

type neighbourResponse struct {
	Place  int32  `json:"place"`
	Org    string `json:"org"`
	Name   string `json:"name"`
	Score  int32  `json:"score"`
	Points int32  `json:"points"`
}

type categoryComparisonResponse struct {
	ID          string `json:"id"`
	Score       int32  `json:"score"`
	Top10Median *int32 `json:"top10_median"`
	Gap         *int32 `json:"gap"`
}

type positionResponse struct {
	Org    string `json:"org"`
	Repo   string `json:"repo"`
	Status string `json:"status"`

	Place int32  `json:"place,omitempty"`
	Total int32  `json:"total,omitempty"`
	Score *int32 `json:"score,omitempty"`
	// Above is the repo one place higher, Below the repo one place lower; null at the ends.
	Above           *neighbourResponse           `json:"above"`
	Below           *neighbourResponse           `json:"below"`
	Categories      []categoryComparisonResponse `json:"categories"`
	WeakestCategory string                       `json:"weakest_category"`
}

func mapNeighbourResponse(n *domain.RankNeighbour) *neighbourResponse {
	if n == nil {
		return nil
	}

	return &neighbourResponse{Place: n.Place, Org: n.OrgSlug, Name: n.Name, Score: n.Score, Points: n.Points}
}

// GetPosition serves GET /position/{org}/{repo}: why the repo has its place in the default
// ranking (neighbours and the per-category gap to the top-10 median). Read-only, public and
// CORS-open; a public repo that is not graded yet answers 200 with NO_DATA.
func (h *Handler) GetPosition(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	setPublicCORS(w)

	org, repo := r.PathValue("org"), r.PathValue("repo")
	if org == "" || repo == "" {
		stdhttp.NotFound(w, r)
		return
	}

	explanation, err := h.service.GetPositionExplanation(r.Context(), clientIP(r), org, repo)
	if err != nil {
		writeError(w, err)
		return
	}

	response := positionResponse{Org: org, Repo: repo, Status: statusNoData, Categories: []categoryComparisonResponse{}}

	if explanation.Ranked {
		score := explanation.Score
		response.Status = statusFinished
		response.Place, response.Total, response.Score = explanation.Place, explanation.Total, &score
		response.Above, response.Below = mapNeighbourResponse(explanation.Above), mapNeighbourResponse(explanation.Below)
		response.WeakestCategory = explanation.WeakestCategory

		for _, category := range explanation.Categories {
			response.Categories = append(response.Categories, categoryComparisonResponse{
				ID: category.ID, Score: category.Score, Top10Median: category.Top10Median, Gap: category.Gap,
			})
		}
	}

	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", scoreCacheSeconds))
	writeJSON(w, stdhttp.StatusOK, response)
}

// Widget geometry. The card is a fixed 380x164 box: the score on the left, one bar per public
// category on the right.
const (
	widgetWidth     = 380
	widgetHeight    = 164
	widgetNameLimit = 38
	widgetBarX      = 250
	widgetBarWidth  = 90
)

// renderWidget draws the README card. name is "org/repo" and is escaped and truncated; every
// other string comes from the fixed category and level lists.
func renderWidget(name string, summary domain.PublicSummary) string {
	_, levelLabel := healthLevel(summary.Score)

	scoreText, scoreColor := "n/a", badgeNoDataColor
	if summary.Score != nil {
		scoreText, scoreColor = strconv.Itoa(int(*summary.Score)), badgeColor(*summary.Score)
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-label="Repo Health: %s">`,
		widgetWidth, widgetHeight, widgetWidth, widgetHeight, html.EscapeString(scoreText))
	fmt.Fprintf(&b, `<title>Repo Health: %s</title>`, html.EscapeString(scoreText))
	fmt.Fprintf(&b, `<rect x="0.5" y="0.5" width="%d" height="%d" rx="8" fill="#ffffff" stroke="#d0d7de"/>`, widgetWidth-1, widgetHeight-1)
	b.WriteString(`<g font-family="Verdana,Geneva,DejaVu Sans,sans-serif">`)
	fmt.Fprintf(&b, `<text x="16" y="26" font-size="13" font-weight="bold" fill="#24292f">%s</text>`, html.EscapeString(truncateRunes(name, widgetNameLimit)))
	b.WriteString(`<text x="16" y="44" font-size="10" fill="#6e7781">Repo Health Score</text>`)
	fmt.Fprintf(&b, `<text x="16" y="100" font-size="44" font-weight="bold" fill="%s">%s</text>`, scoreColor, html.EscapeString(scoreText))
	if summary.Score != nil {
		fmt.Fprintf(&b, `<text x="%d" y="100" font-size="12" fill="#6e7781">/100</text>`, 16+textWidthAt(scoreText, 44)+4)
	}
	fmt.Fprintf(&b, `<text x="16" y="122" font-size="11" fill="#24292f">%s</text>`, html.EscapeString(levelLabel))

	for i, category := range publicCategories {
		y := 62 + i*21
		value, color := "n/a", ""
		fill := 0
		for _, entry := range summary.Categories {
			if entry.ID == category.ID && entry.Score != nil {
				value, color = strconv.Itoa(int(*entry.Score)), badgeColor(*entry.Score)
				fill = int(*entry.Score) * widgetBarWidth / 100
			}
		}

		fmt.Fprintf(&b, `<text x="140" y="%d" font-size="11" fill="#24292f">%s</text>`, y+6, html.EscapeString(category.WidgetLabel))
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="6" rx="3" fill="#eaeef2"/>`, widgetBarX, y, widgetBarWidth)
		if fill > 0 {
			fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="6" rx="3" fill="%s"/>`, widgetBarX, y, fill, color)
		}
		fmt.Fprintf(&b, `<text x="%d" y="%d" font-size="11" text-anchor="end" fill="#24292f">%s</text>`, widgetWidth-16, y+6, html.EscapeString(value))
	}

	if summary.GradedAt != nil {
		fmt.Fprintf(&b, `<text x="16" y="150" font-size="9" fill="#6e7781">Обновлено %s</text>`, summary.GradedAt.UTC().Format("2006-01-02"))
	}
	b.WriteString(`</g></svg>`)

	return b.String()
}

// textWidthAt estimates the width of digits or "n/a" at the given font size from the 11px
// estimate textWidth uses for badges.
func textWidthAt(text string, fontSize int) int {
	return textWidth(text) * fontSize / 11
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}

	return string(runes[:limit-1]) + "…"
}
