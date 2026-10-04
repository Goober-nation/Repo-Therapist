package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"

	"gateway-service/internal/domain"
)

const (
	sessionCookieName = "session_id"

	// patHeader carries the caller's SourceCraft PAT. Without it only public data is returned.
	patHeader = "PAT"

	statusPending  = "PENDING"
	statusFinished = "FINISHED"
	statusNoData   = "NO_DATA"
)

type GatewayService interface {
	GetMyRepos(
		ctx context.Context,
		clientIP string,
		pat string,
	) (domain.GetMyReposDTO, error)

	GetMyOrgs(
		ctx context.Context,
		clientIP string,
		pat string,
	) (domain.GetMyOrgsDTO, error)

	GetOrgRepos(
		ctx context.Context,
		clientIP string,
		pat string,
		orgSlug string,
		pageToken string,
	) (domain.GetMyReposDTO, error)

	GetGrading(
		ctx context.Context,
		clientIP string,
		pat string,
		request domain.GetGradingDTO,
	) (domain.GetGradingResponse, error)

	RequestLLMRecommendations(
		ctx context.Context,
		clientIP string,
		pat string,
		request domain.RequestLLMRecommendationsDTO,
	) (domain.RequestLLMRecommendationsResponse, error)

	GetRanking(
		ctx context.Context,
		clientIP string,
		request domain.GetRankingDTO,
	) (domain.GetRankingResponse, error)

	GetAnalytics(
		ctx context.Context,
		clientIP string,
		pat string,
		orgSlug string,
		repoSlug string,
	) (domain.RepoAnalytics, error)

	GetScoreHistory(
		ctx context.Context,
		clientIP string,
		orgSlug string,
		repoSlug string,
	) ([]domain.HistoryPoint, error)

	GetPositionExplanation(
		ctx context.Context,
		clientIP string,
		orgSlug string,
		repoSlug string,
	) (domain.PositionExplanation, error)

	LoginWithYandex(
		ctx context.Context,
		clientIP string,
		request domain.YandexLoginDTO,
	) (domain.LoginResponse, error)

	GetPublicScore(
		ctx context.Context,
		orgSlug string,
		repoSlug string,
	) (*int32, error)

	// GetPublicSummary backs the category badges and the widget; it is never rate limited.
	GetPublicSummary(
		ctx context.Context,
		orgSlug string,
		repoSlug string,
	) (domain.PublicSummary, error)

	// GetScore backs the public JSON score API, behind its own per-IP limit.
	GetScore(
		ctx context.Context,
		clientIP string,
		orgSlug string,
		repoSlug string,
	) (domain.PublicSummary, error)
}

type Handler struct {
	service GatewayService
}

func NewHandler(service GatewayService) *Handler {
	return &Handler{
		service: service,
	}
}

// Register mounts the gateway's routes on mux.
func (h *Handler) Register(mux *stdhttp.ServeMux) {
	mux.HandleFunc("POST /auth/yandex", h.LoginWithYandex)
	mux.HandleFunc("GET /repos", h.GetMyRepos)
	mux.HandleFunc("GET /orgs", h.GetMyOrgs)
	mux.HandleFunc("GET /grading/{org}/{repo}", h.GetGrading)
	mux.HandleFunc("GET /recommendations/{org}/{repo}", h.RequestLLMRecommendations)
	mux.HandleFunc("GET /analytics/{org}/{repo}", h.GetAnalytics)
	mux.HandleFunc("GET /ranking", h.GetRanking)
	mux.HandleFunc("GET /badge/{org}/{repo}", h.GetBadge)
	mux.HandleFunc("GET /badge/{org}/{repo}/{category}", h.GetCategoryBadge)
	mux.HandleFunc("GET /widget/{org}/{repo}", h.GetWidget)
	mux.HandleFunc("GET /score/{org}/{repo}", h.GetScore)
	mux.HandleFunc("GET /history/{org}/{repo}", h.GetHistory)
	mux.HandleFunc("GET /position/{org}/{repo}", h.GetPosition)
}

func (h *Handler) LoginWithYandex(
	w stdhttp.ResponseWriter,
	r *stdhttp.Request,
) {
	token, err := yandexOAuthToken(r)
	if err != nil {
		writeError(w, err)
		return
	}

	result, err := h.service.LoginWithYandex(
		r.Context(),
		clientIP(r),
		domain.YandexLoginDTO{
			OAuthToken: token,
		},
	)
	if err != nil {
		writeError(w, err)
		return
	}

	stdhttp.SetCookie(w, &stdhttp.Cookie{
		Name:     sessionCookieName,
		Value:    result.SessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: stdhttp.SameSiteLaxMode,
	})

	writeJSON(
		w,
		stdhttp.StatusOK,
		loginResponse{
			SessionID: result.SessionID,
		},
	)
}

// yandexOAuthToken reads the Yandex OAuth token from an "OAuth: <token>" header, or from
// "Authorization: OAuth <token>".
func yandexOAuthToken(
	r *stdhttp.Request,
) (string, error) {
	token := strings.TrimSpace(r.Header.Get("OAuth"))

	if token == "" {
		header := strings.TrimSpace(r.Header.Get("Authorization"))

		const prefix = "OAuth "
		if strings.HasPrefix(header, prefix) {
			token = strings.TrimSpace(strings.TrimPrefix(header, prefix))
		}
	}

	if token == "" {
		return "", httpError{
			status: stdhttp.StatusUnauthorized,
			err:    domain.ErrUnauthenticated,
		}
	}

	return token, nil
}

// patFromRequest reads the PAT header; a "Bearer " prefix is tolerated.
func patFromRequest(r *stdhttp.Request) string {
	token := strings.TrimSpace(r.Header.Get(patHeader))
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimPrefix(token, "bearer ")

	return strings.TrimSpace(token)
}

func (h *Handler) GetMyRepos(
	w stdhttp.ResponseWriter,
	r *stdhttp.Request,
) {
	pat := patFromRequest(r)
	if pat == "" {
		writeError(w, httpError{
			status: stdhttp.StatusUnauthorized,
			err:    domain.ErrUnauthenticated,
		})
		return
	}

	var (
		result domain.GetMyReposDTO
		err    error
	)
	// ?org= limits the list to one organization (one batch, continued via ?page_token=);
	// without it the legacy all-orgs /me/repos listing is returned.
	if org := strings.TrimSpace(r.URL.Query().Get("org")); org != "" {
		result, err = h.service.GetOrgRepos(
			r.Context(),
			clientIP(r),
			pat,
			org,
			r.URL.Query().Get("page_token"),
		)
	} else {
		result, err = h.service.GetMyRepos(
			r.Context(),
			clientIP(r),
			pat,
		)
	}
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(
		w,
		stdhttp.StatusOK,
		mapGetMyReposResponse(result),
	)
}

func (h *Handler) GetGrading(
	w stdhttp.ResponseWriter,
	r *stdhttp.Request,
) {
	result, err := h.service.GetGrading(
		r.Context(),
		clientIP(r),
		patFromRequest(r),
		domain.GetGradingDTO{
			OrgSlug:  strings.TrimSpace(r.PathValue("org")),
			RepoSlug: strings.TrimSpace(r.PathValue("repo")),
		},
	)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(
		w,
		stdhttp.StatusOK,
		mapGetGradingResponse(result),
	)
}

// analyticsResponse is GET /analytics: the informational analytics object as stored (bus factor,
// ownership map), or NO_DATA with a null object when the repo has none yet.
type analyticsResponse struct {
	Status    string          `json:"status"`
	Analytics json.RawMessage `json:"analytics"`
	GradedAt  *string         `json:"graded_at"`
}

// GetAnalytics serves GET /analytics/{org}/{repo}. Like /score it is read-only and never starts a
// grading; with the owner's PAT it returns the private view's analytics, otherwise the public one.
func (h *Handler) GetAnalytics(
	w stdhttp.ResponseWriter,
	r *stdhttp.Request,
) {
	result, err := h.service.GetAnalytics(
		r.Context(),
		clientIP(r),
		patFromRequest(r),
		strings.TrimSpace(r.PathValue("org")),
		strings.TrimSpace(r.PathValue("repo")),
	)
	if err != nil {
		writeError(w, err)
		return
	}

	response := analyticsResponse{Status: statusNoData, Analytics: json.RawMessage("null")}
	// Anything that is not a valid JSON object counts as no analytics: a broken stored value must
	// not turn the whole response into invalid JSON.
	if stored := rawJSON(result.Analytics); stored != nil && strings.TrimSpace(string(stored)) != "null" {
		response.Status, response.Analytics = statusFinished, stored
	}
	if result.GradedAt != nil {
		value := result.GradedAt.Format(time.RFC3339Nano)
		response.GradedAt = &value
	}

	writeJSON(w, stdhttp.StatusOK, response)
}

func (h *Handler) RequestLLMRecommendations(
	w stdhttp.ResponseWriter,
	r *stdhttp.Request,
) {
	result, err := h.service.RequestLLMRecommendations(
		r.Context(),
		clientIP(r),
		patFromRequest(r),
		domain.RequestLLMRecommendationsDTO{
			OrgSlug:  strings.TrimSpace(r.PathValue("org")),
			RepoSlug: strings.TrimSpace(r.PathValue("repo")),
		},
	)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(
		w,
		stdhttp.StatusOK,
		mapRequestLLMRecommendationsResponse(result),
	)
}

func (h *Handler) GetRanking(
	w stdhttp.ResponseWriter,
	r *stdhttp.Request,
) {
	setPublicCORS(w)

	dto, err := mapGetRankingRequest(r)
	if err != nil {
		writeError(w, err)
		return
	}

	result, err := h.service.GetRanking(
		r.Context(),
		clientIP(r),
		dto,
	)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(
		w,
		stdhttp.StatusOK,
		mapGetRankingResponse(result),
	)
}

// mapGetRankingRequest reads ?sortby=&filter=&page=&page_size=, all optional.
func mapGetRankingRequest(
	r *stdhttp.Request,
) (domain.GetRankingDTO, error) {
	query := r.URL.Query()

	sortBy, err := mapHTTPRankingSortBy(query.Get("sortby"))
	if err != nil {
		return domain.GetRankingDTO{}, err
	}

	page, err := queryInt32(query.Get("page"), "page")
	if err != nil {
		return domain.GetRankingDTO{}, err
	}

	pageSize, err := queryInt32(query.Get("page_size"), "page_size")
	if err != nil {
		return domain.GetRankingDTO{}, err
	}

	hideLowConfidence, err := queryBool(query.Get("hide_low_confidence"), "hide_low_confidence")
	if err != nil {
		return domain.GetRankingDTO{}, err
	}

	return domain.GetRankingDTO{
		Page:              page,
		PageSize:          pageSize,
		Language:          strings.TrimSpace(query.Get("filter")),
		SortBy:            sortBy,
		HideLowConfidence: hideLowConfidence,
	}, nil
}

// queryBool reads an optional boolean query value; empty means false.
func queryBool(value, name string) (bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return false, nil
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, httpError{
			status: stdhttp.StatusBadRequest,
			err: fmt.Errorf(
				"%w: %s must be true or false",
				domain.ErrInvalidRequest,
				name,
			),
		}
	}

	return parsed, nil
}

func queryInt32(value, name string) (int32, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}

	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil || parsed < 0 {
		return 0, httpError{
			status: stdhttp.StatusBadRequest,
			err: fmt.Errorf(
				"%w: %s must be a non-negative integer",
				domain.ErrInvalidRequest,
				name,
			),
		}
	}

	return int32(parsed), nil
}

func mapHTTPRankingSortBy(
	value string,
) (domain.RankingSortBy, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "score":
		return domain.RankingSortByScore, nil

	case "last_activity":
		return domain.RankingSortByLastActivity, nil

	case "likes":
		return domain.RankingSortByLikes, nil

	default:
		return 0, httpError{
			status: stdhttp.StatusBadRequest,
			err: fmt.Errorf(
				"%w: invalid sortby",
				domain.ErrInvalidRequest,
			),
		}
	}
}

type loginResponse struct {
	SessionID string `json:"session_id"`
}

type pendingResponse struct {
	Status string `json:"status"`
}

type getMyReposResponse struct {
	Repositories  []repositoryResponse `json:"repositories"`
	NextPageToken string               `json:"next_page_token"`
}

type repositoryResponse struct {
	SourcecraftUUID string           `json:"sourcecraft_uuid"`
	OrgSlug         string           `json:"org_slug"`
	RepoSlug        string           `json:"repo_slug"`
	WebURL          string           `json:"web_url"`
	Visibility      string           `json:"visibility"`
	LastActivityAt  *string          `json:"last_activity_at,omitempty"`
	Rating          float64          `json:"rating"`
	Language        languageResponse `json:"language"`
}

type languageResponse struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type getGradingResponse struct {
	Status string            `json:"status"`
	Grade  repoGradeResponse `json:"grade"`
}

type repoGradeResponse struct {
	SourcecraftUUID string          `json:"sourcecraft_uuid"`
	Score           *int32          `json:"score,omitempty"`
	CategoryScores  json.RawMessage `json:"category_scores,omitempty"`
	Advice          json.RawMessage `json:"advice,omitempty"`
	MissingData     []string        `json:"missing_data,omitempty"`
	GradedAt        string          `json:"graded_at"`
}

type requestLLMRecommendationsResponse struct {
	Status      string          `json:"status"`
	RawResponse json.RawMessage `json:"raw_response"`
	ModelUsed   string          `json:"model_used"`
	EvaluatedAt string          `json:"evaluated_at"`
}

type getRankingResponse struct {
	Entries    []rankingEntryResponse `json:"entries"`
	TotalCount int32                  `json:"total_count"`
	Page       int32                  `json:"page"`
	PageSize   int32                  `json:"page_size"`
	// DynamicsDays is the window of the entries' score_change and place_change.
	DynamicsDays int32 `json:"dynamics_days"`
}

type rankingEntryResponse struct {
	Place           int32            `json:"place"`
	SourcecraftUUID string           `json:"sourcecraft_uuid"`
	Name            string           `json:"name"`
	WebURL          string           `json:"web_url"`
	PublicScore     *int32           `json:"public_score,omitempty"`
	Likes           *float64         `json:"likes,omitempty"`
	Language        languageResponse `json:"language"`
	LastActivityAt  *string          `json:"last_activity_at,omitempty"`
	// LowConfidence is null when the flag is unknown; the reasons are empty unless it is true.
	LowConfidence     *bool                      `json:"low_confidence"`
	ConfidenceReasons []confidenceReasonResponse `json:"confidence_reasons"`
	// ScoreChange and PlaceChange are null when the repo was not graded dynamics_days ago ("new");
	// PlaceChange is also null unless the list is sorted by score. Positive PlaceChange = moved up.
	ScoreChange *int32 `json:"score_change"`
	PlaceChange *int32 `json:"place_change"`
}

type confidenceReasonResponse struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// mapConfidenceResponse returns the wire form of a flag: a null low_confidence when unknown and
// always a (possibly empty) reasons list.
func mapConfidenceResponse(confidence *domain.Confidence) (*bool, []confidenceReasonResponse) {
	reasons := []confidenceReasonResponse{}
	if confidence == nil {
		return nil, reasons
	}

	for _, reason := range confidence.Reasons {
		reasons = append(reasons, confidenceReasonResponse{Code: reason.Code, Text: reason.Text})
	}

	return &confidence.Low, reasons
}

func mapGetMyReposResponse(
	result domain.GetMyReposDTO,
) getMyReposResponse {
	repositories := make(
		[]repositoryResponse,
		0,
		len(result.Repositories),
	)

	for _, repo := range result.Repositories {
		repositories = append(
			repositories,
			mapRepositoryResponse(repo),
		)
	}

	return getMyReposResponse{
		Repositories:  repositories,
		NextPageToken: result.NextPageToken,
	}
}

func mapRepositoryResponse(
	repo domain.Repo,
) repositoryResponse {
	result := repositoryResponse{
		SourcecraftUUID: repo.SourcecraftUUID.String(),
		OrgSlug:         repo.OrgSlug,
		RepoSlug:        repo.RepoSlug,
		WebURL:          repo.WebURL,
		Visibility:      repo.Visibility,
		Rating:          repo.RatingValue,
		Language: languageResponse{
			Name:  repo.Language.Name,
			Color: repo.Language.Color,
		},
	}

	if repo.LastActivityAt != nil {
		value := repo.LastActivityAt.Format(time.RFC3339Nano)
		result.LastActivityAt = &value
	}

	return result
}

func mapGetGradingResponse(
	result domain.GetGradingResponse,
) getGradingResponse {
	return getGradingResponse{
		Status: statusFinished,
		Grade:  mapRepoGradeResponse(result.Grade),
	}
}

func mapRepoGradeResponse(
	grade domain.RepoGrade,
) repoGradeResponse {
	return repoGradeResponse{
		SourcecraftUUID: grade.SourcecraftUUID.String(),
		Score:           grade.Score,
		CategoryScores:  rawJSON(grade.CategoryScores),
		Advice:          rawJSON(grade.Advice),
		MissingData:     append([]string(nil), grade.MissingData...),
		GradedAt:        grade.GradedAt.Format(time.RFC3339Nano),
	}
}

// rawJSON passes stored JSON through unchanged; empty or invalid input is dropped rather than
// breaking the whole response.
func rawJSON(value []byte) json.RawMessage {
	if len(value) == 0 || !json.Valid(value) {
		return nil
	}

	return append(json.RawMessage(nil), value...)
}

func mapRequestLLMRecommendationsResponse(
	result domain.RequestLLMRecommendationsResponse,
) requestLLMRecommendationsResponse {
	return requestLLMRecommendationsResponse{
		Status:      statusFinished,
		RawResponse: rawJSON(result.RawResponse),
		ModelUsed:   result.ModelUsed,
		EvaluatedAt: result.EvaluatedAt.Format(time.RFC3339Nano),
	}
}

func mapGetRankingResponse(
	result domain.GetRankingResponse,
) getRankingResponse {
	entries := make(
		[]rankingEntryResponse,
		0,
		len(result.Entries),
	)

	for _, entry := range result.Entries {
		entries = append(
			entries,
			mapRankingEntryResponse(entry),
		)
	}

	return getRankingResponse{
		Entries:    entries,
		TotalCount: result.TotalCount,
		Page:       result.Page,
		PageSize:   result.PageSize,

		DynamicsDays: result.DynamicsDays,
	}
}

func mapRankingEntryResponse(
	entry domain.RankingEntry,
) rankingEntryResponse {
	result := rankingEntryResponse{
		Place:           entry.Place,
		SourcecraftUUID: entry.SourcecraftUUID.String(),
		Name:            entry.Name,
		WebURL:          entry.WebURL,
		PublicScore:     entry.PublicScore,
		Likes:           entry.Likes,
		Language: languageResponse{
			Name:  entry.Language.Name,
			Color: entry.Language.Color,
		},
	}

	if entry.LastActivityAt != nil {
		value := entry.LastActivityAt.Format(time.RFC3339Nano)
		result.LastActivityAt = &value
	}

	result.LowConfidence, result.ConfidenceReasons = mapConfidenceResponse(entry.Confidence)
	result.ScoreChange, result.PlaceChange = entry.ScoreChange, entry.PlaceChange

	return result
}

// clientIP identifies the caller for rate limiting. Behind the frontend nginx every request
// arrives from the proxy's address, which would make the limit global, so X-Real-IP set by
// nginx is used — but only when the direct peer is a loopback or private address (the proxy),
// never from a client connecting straight to the gateway.
func clientIP(
	r *stdhttp.Request,
) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	peer := net.ParseIP(host)
	if peer != nil && (peer.IsLoopback() || peer.IsPrivate()) {
		if forwarded := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); forwarded != nil {
			return forwarded.String()
		}
	}

	return host
}

func writeJSON(
	w stdhttp.ResponseWriter,
	status int,
	value any,
) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(value)
}

type httpError struct {
	status int
	err    error
}

func (e httpError) Error() string {
	return e.err.Error()
}

func (e httpError) Unwrap() error {
	return e.err
}

type errorResponse struct {
	Error string `json:"error"`
}

// writeError writes a pending result as 202 {"status":"PENDING"}, anything else as an error.
func writeError(
	w stdhttp.ResponseWriter,
	err error,
) {
	if errors.Is(err, domain.ErrPending) {
		writeJSON(
			w,
			stdhttp.StatusAccepted,
			pendingResponse{
				Status: statusPending,
			},
		)
		return
	}

	var limit domain.GradingLimitError
	if errors.As(err, &limit) {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(limit.RetryAfter.Seconds()))))
	}

	writeJSON(
		w,
		mapErrorStatus(err),
		errorResponse{
			Error: publicErrorMessage(err),
		},
	)
}

func mapErrorStatus(err error) int {
	var httpErr httpError

	if errors.As(err, &httpErr) {
		return httpErr.status
	}

	switch {
	case errors.Is(err, domain.ErrUnauthenticated),
		errors.Is(err, domain.ErrSessionExpired),
		errors.Is(err, domain.ErrSessionNotFound),
		errors.Is(err, domain.ErrIdentityInvalid),
		errors.Is(err, domain.ErrIdentityNotFound):
		return stdhttp.StatusUnauthorized

	case errors.Is(err, domain.ErrInvalidRequest),
		errors.Is(err, domain.ErrInvalidUser):
		return stdhttp.StatusBadRequest

	case errors.Is(err, domain.ErrForbidden):
		return stdhttp.StatusForbidden

	case errors.Is(err, domain.ErrNotFound),
		errors.Is(err, domain.ErrUserNotFound):
		return stdhttp.StatusNotFound

	case errors.Is(err, domain.ErrConflict),
		errors.Is(err, domain.ErrUserAlreadyExists):
		return stdhttp.StatusConflict

	case errors.Is(err, domain.ErrLimitExceeded):
		return stdhttp.StatusTooManyRequests

	case errors.Is(err, domain.ErrUnavailable):
		return stdhttp.StatusServiceUnavailable

	case errors.Is(err, domain.ErrTimeout):
		return stdhttp.StatusGatewayTimeout

	case errors.Is(err, domain.ErrRequestFailed),
		errors.Is(err, domain.ErrGradingInterrupted):
		return stdhttp.StatusBadGateway

	default:
		return stdhttp.StatusInternalServerError
	}
}

func publicErrorMessage(err error) string {
	switch {
	case errors.Is(err, domain.ErrUnauthenticated):
		return "unauthenticated"

	case errors.Is(err, domain.ErrSessionExpired),
		errors.Is(err, domain.ErrSessionNotFound):
		return "session expired"

	case errors.Is(err, domain.ErrIdentityInvalid):
		return "invalid identity"

	case errors.Is(err, domain.ErrIdentityNotFound):
		return "identity not found"

	case errors.Is(err, domain.ErrInvalidRequest):
		return "invalid request"

	case errors.Is(err, domain.ErrForbidden):
		return "access denied"

	case errors.Is(err, domain.ErrNotFound):
		return "not found"

	case errors.Is(err, domain.ErrConflict):
		return "conflict"

	case errors.Is(err, domain.ErrGradingInterrupted):
		return "grading interrupted"

	case errors.As(err, new(domain.GradingLimitError)):
		return "grading limit reached"

	case errors.Is(err, domain.ErrLimitExceeded):
		return "rate limit exceeded"

	case errors.Is(err, domain.ErrUnavailable):
		return "upstream unavailable"

	case errors.Is(err, domain.ErrTimeout):
		return "upstream timeout"

	case errors.Is(err, domain.ErrRequestFailed):
		return "request failed"

	default:
		return "internal server error"
	}
}

func isNotFound(err error) bool {
	return errors.Is(err, domain.ErrNotFound)
}
