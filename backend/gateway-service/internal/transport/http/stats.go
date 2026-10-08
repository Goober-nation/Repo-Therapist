package http

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"

	"gateway-service/internal/domain"
)

// statsCacheSeconds matches the order of magnitude of the Rating service's own cache.
const statsCacheSeconds = 60

type statsBucket struct {
	Min   int32 `json:"min"`
	Max   int32 `json:"max"`
	Repos int32 `json:"repos"`
}

type statsLevel struct {
	Level string `json:"level"`
	Repos int32  `json:"repos"`
}

type statsLanguage struct {
	Name     string  `json:"name"`
	Color    string  `json:"color"`
	Repos    int32   `json:"repos"`
	AvgScore float64 `json:"avg_score"`
}

type statsCategory struct {
	ID       string  `json:"id"`
	AvgScore float64 `json:"avg_score"`
	Repos    int32   `json:"repos"`
}

type statsAdvice struct {
	CategoryID string `json:"category_id"`
	MetricID   string `json:"metric_id"`
	Priority   string `json:"priority"`
	Repos      int32  `json:"repos"`
}

type statsTrend struct {
	WeekStart *time.Time `json:"week_start"`
	AvgScore  float64    `json:"avg_score"`
	Repos     int32      `json:"repos"`
}

type statsImproved struct {
	Org       string `json:"org"`
	Name      string `json:"name"`
	Score     int32  `json:"score"`
	PastScore int32  `json:"past_score"`
	Change    int32  `json:"change"`
}

type statsResponse struct {
	GradedRepos  int32           `json:"graded_repos"`
	AvgScore     float64         `json:"avg_score"`
	MedianScore  float64         `json:"median_score"`
	Distribution []statsBucket   `json:"distribution"`
	Levels       []statsLevel    `json:"levels"`
	Languages    []statsLanguage `json:"languages"`
	Categories   []statsCategory `json:"categories"`
	TopAdvice    []statsAdvice   `json:"top_advice"`
	Trend        []statsTrend    `json:"trend"`
	MostImproved []statsImproved `json:"most_improved"`
	ImprovedDays int32           `json:"improved_days"`
	MinLanguage  int32           `json:"min_language_repos"`
	GeneratedAt  *time.Time      `json:"generated_at"`
}

// GetStats serves GET /stats: aggregate statistics over all graded public repositories (score
// distribution, average per language and category, common advice, weekly trend, most improved).
// Read-only, public and CORS-open; private grades never enter it.
func (h *Handler) GetStats(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	setPublicCORS(w)

	stats, err := h.service.GetPlatformStats(r.Context(), clientIP(r))
	if err != nil {
		writeError(w, err)
		return
	}

	response := statsResponse{
		GradedRepos:  stats.GradedRepos,
		AvgScore:     stats.AvgScore,
		MedianScore:  stats.MedianScore,
		Distribution: make([]statsBucket, 0, len(stats.Distribution)),
		Levels:       make([]statsLevel, 0, len(stats.Levels)),
		Languages:    make([]statsLanguage, 0, len(stats.Languages)),
		Categories:   make([]statsCategory, 0, len(stats.Categories)),
		TopAdvice:    make([]statsAdvice, 0, len(stats.TopAdvice)),
		Trend:        make([]statsTrend, 0, len(stats.Trend)),
		MostImproved: make([]statsImproved, 0, len(stats.MostImproved)),
		ImprovedDays: stats.ImprovedDays,
		MinLanguage:  stats.MinLanguage,
		GeneratedAt:  stats.GeneratedAt,
	}

	for _, b := range stats.Distribution {
		response.Distribution = append(response.Distribution, statsBucket{Min: b.Min, Max: b.Max, Repos: b.Repos})
	}

	for _, l := range stats.Levels {
		response.Levels = append(response.Levels, statsLevel{Level: l.Level, Repos: l.Repos})
	}

	for _, l := range stats.Languages {
		response.Languages = append(response.Languages, statsLanguage{Name: l.Name, Color: l.Color, Repos: l.Repos, AvgScore: l.AvgScore})
	}

	for _, c := range stats.Categories {
		response.Categories = append(response.Categories, statsCategory{ID: c.ID, AvgScore: c.AvgScore, Repos: c.Repos})
	}

	for _, a := range stats.TopAdvice {
		response.TopAdvice = append(response.TopAdvice, statsAdvice{
			CategoryID: a.CategoryID, MetricID: a.MetricID, Priority: a.Priority, Repos: a.Repos,
		})
	}

	for _, t := range stats.Trend {
		response.Trend = append(response.Trend, statsTrend{WeekStart: t.WeekStart, AvgScore: t.AvgScore, Repos: t.Repos})
	}

	for _, m := range stats.MostImproved {
		response.MostImproved = append(response.MostImproved, statsImproved{
			Org: m.OrgSlug, Name: m.Name, Score: m.Score, PastScore: m.PastScore, Change: m.Change,
		})
	}

	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", statsCacheSeconds))
	writeJSON(w, stdhttp.StatusOK, response)
}

type exportCategory struct {
	ID     string `json:"id"`
	Score  *int32 `json:"score"`
	NoData bool   `json:"no_data"`
}

type exportGrade struct {
	Org            string           `json:"org"`
	Repo           string           `json:"repo"`
	WebURL         string           `json:"web_url"`
	Score          int32            `json:"score"`
	Level          string           `json:"level"`
	Language       string           `json:"language"`
	Likes          *float64         `json:"likes"`
	LastActivityAt *time.Time       `json:"last_activity_at"`
	GradedAt       *time.Time       `json:"graded_at"`
	LowConfidence  *bool            `json:"low_confidence"`
	Categories     []exportCategory `json:"categories"`
}

// gradeEncoder writes one export format: begin once with the total, write per page, end once.
type gradeEncoder interface {
	contentType() string
	extension() string
	begin(w stdhttp.ResponseWriter, total int32, generatedAt time.Time) error
	write(w stdhttp.ResponseWriter, grades []domain.ExportedGrade) error
	end(w stdhttp.ResponseWriter) error
}

// ExportGradesCSV serves GET /export/grades.csv: every public grade, one row per repository.
func (h *Handler) ExportGradesCSV(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	h.exportGrades(w, r, &csvEncoder{})
}

// ExportGradesJSON serves GET /export/grades.json: every public grade as {generated_at, total, grades}.
func (h *Handler) ExportGradesJSON(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	h.exportGrades(w, r, &jsonEncoder{})
}

// exportGrades streams the export. Headers go out with the first page, so an error before it is an
// ordinary error response. After that the status is already sent, so a failure aborts the
// connection: a cut-off JSON is invalid and a cut-off CSV transfer is incomplete, and neither can
// be mistaken for a finished export.
func (h *Handler) exportGrades(w stdhttp.ResponseWriter, r *stdhttp.Request, enc gradeEncoder) {
	setPublicCORS(w)

	started := false
	now := time.Now().UTC()

	err := h.service.ExportPublicGrades(r.Context(), clientIP(r), func(page domain.ExportPage) error {
		if !started {
			started = true

			w.Header().Set("Content-Type", enc.contentType())
			w.Header().Set("Content-Disposition",
				fmt.Sprintf(`attachment; filename="repo-health-grades-%s.%s"`, now.Format("2006-01-02"), enc.extension()))
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(stdhttp.StatusOK)

			if err := enc.begin(w, page.Total, now); err != nil {
				return err
			}
		}

		return enc.write(w, page.Grades)
	})

	if err != nil && !started {
		writeError(w, err)
		return
	}

	if err != nil {
		log.Printf("export public grades aborted mid-stream: %v", err)
		panic(stdhttp.ErrAbortHandler)
	}

	if err := enc.end(w); err != nil {
		log.Printf("export public grades: finish: %v", err)
	}
}

func exportRow(grade domain.ExportedGrade) exportGrade {
	score := grade.Score
	level, _ := healthLevel(&score)

	categories := make([]exportCategory, 0, len(grade.Categories))
	for _, category := range grade.Categories {
		categories = append(categories, exportCategory{ID: category.ID, Score: category.Score, NoData: category.Score == nil})
	}

	return exportGrade{
		Org: grade.OrgSlug, Repo: grade.Repo, WebURL: grade.WebURL, Score: score, Level: level,
		Language: grade.Language, Likes: grade.Likes, LastActivityAt: grade.LastActivityAt,
		GradedAt: grade.GradedAt, LowConfidence: grade.LowConfidence, Categories: categories,
	}
}

type jsonEncoder struct{ wrote bool }

func (*jsonEncoder) contentType() string { return "application/json; charset=utf-8" }
func (*jsonEncoder) extension() string   { return "json" }

func (*jsonEncoder) begin(w stdhttp.ResponseWriter, total int32, generatedAt time.Time) error {
	_, err := fmt.Fprintf(w, `{"generated_at":%q,"total":%d,"grades":[`, generatedAt.Format(time.RFC3339), total)
	return err
}

func (e *jsonEncoder) write(w stdhttp.ResponseWriter, grades []domain.ExportedGrade) error {
	for _, grade := range grades {
		encoded, err := json.Marshal(exportRow(grade))
		if err != nil {
			return err
		}

		prefix := ","
		if !e.wrote {
			prefix, e.wrote = "", true
		}

		if _, err := w.Write(append([]byte(prefix), encoded...)); err != nil {
			return err
		}
	}

	return nil
}

func (*jsonEncoder) end(w stdhttp.ResponseWriter) error {
	_, err := w.Write([]byte("]}\n"))
	return err
}

type csvEncoder struct{ out *csv.Writer }

func (*csvEncoder) contentType() string { return "text/csv; charset=utf-8" }
func (*csvEncoder) extension() string   { return "csv" }

func (e *csvEncoder) begin(w stdhttp.ResponseWriter, _ int32, _ time.Time) error {
	e.out = csv.NewWriter(w)
	header := []string{"org", "repo", "url", "score", "level", "language", "likes", "last_activity_at", "graded_at", "low_confidence"}

	for _, category := range publicCategories {
		header = append(header, category.ID)
	}

	return e.out.Write(header)
}

func (e *csvEncoder) write(_ stdhttp.ResponseWriter, grades []domain.ExportedGrade) error {
	for _, grade := range grades {
		row := exportRow(grade)

		cells := []string{
			csvText(row.Org), csvText(row.Repo), csvText(row.WebURL), strconv.Itoa(int(row.Score)), row.Level,
			csvText(row.Language), csvFloat(row.Likes), csvTime(row.LastActivityAt), csvTime(row.GradedAt), csvBool(row.LowConfidence),
		}

		for _, category := range publicCategories {
			cell := ""

			for _, entry := range row.Categories {
				if entry.ID == category.ID && entry.Score != nil {
					cell = strconv.Itoa(int(*entry.Score))
				}
			}

			cells = append(cells, cell)
		}

		if err := e.out.Write(cells); err != nil {
			return err
		}
	}

	e.out.Flush()

	return e.out.Error()
}

func (e *csvEncoder) end(_ stdhttp.ResponseWriter) error {
	e.out.Flush()
	return e.out.Error()
}

// csvText neutralises a cell a spreadsheet would run as a formula (CSV injection): text starting
// with = + - @ or a control character gets a leading apostrophe.
func csvText(value string) string {
	if value != "" && strings.ContainsRune("=+-@\t\r", rune(value[0])) {
		return "'" + value
	}

	return value
}

func csvFloat(value *float64) string {
	if value == nil {
		return ""
	}

	return strconv.FormatFloat(*value, 'f', -1, 64)
}

func csvTime(value *time.Time) string {
	if value == nil {
		return ""
	}

	return value.UTC().Format(time.RFC3339)
}

func csvBool(value *bool) string {
	if value == nil {
		return ""
	}

	return strconv.FormatBool(*value)
}
