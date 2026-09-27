package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"strings"
	"time"

	"gateway-service/internal/domain"
)

const sessionCookieName = "session_id"

type GatewayService interface {
	GetMyRepos(
		ctx context.Context,
		sessionID string,
		clientIP string,
	) (domain.GetMyReposDTO, error)

	GetGrading(
		ctx context.Context,
		clientIP string,
		request domain.GetGradingDTO,
	) (domain.GetGradingResponse, error)

	RequestLLMRecommendations(
		ctx context.Context,
		clientIP string,
		request domain.RequestLLMRecommendationsDTO,
	) (domain.RequestLLMRecommendationsResponse, error)

	GetRanking(
		ctx context.Context,
		clientIP string,
		request domain.GetRankingDTO,
	) (domain.GetRankingResponse, error)

	LoginWithYandex(
		ctx context.Context,
		request domain.YandexLoginDTO,
	) (domain.LoginResponse, error)
}

type Handler struct {
	service GatewayService
}

func NewHandler(service GatewayService) *Handler {
	return &Handler{
		service: service,
	}
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

func yandexOAuthToken(
	r *stdhttp.Request,
) (string, error) {
	header := strings.TrimSpace(
		r.Header.Get("Authorization"),
	)

	const prefix = "OAuth "

	if !strings.HasPrefix(header, prefix) {
		return "", httpError{
			status: stdhttp.StatusUnauthorized,
			err:    domain.ErrUnauthenticated,
		}
	}

	token := strings.TrimSpace(
		strings.TrimPrefix(header, prefix),
	)

	if token == "" {
		return "", httpError{
			status: stdhttp.StatusUnauthorized,
			err:    domain.ErrUnauthenticated,
		}
	}

	return token, nil
}

func (h *Handler) GetMyRepos(
	w stdhttp.ResponseWriter,
	r *stdhttp.Request,
) {

	sessionID, err := sessionIDFromRequest(r)
	if err != nil {
		writeError(w, err)
		return
	}

	result, err := h.service.GetMyRepos(
		r.Context(),
		sessionID,
		clientIP(r),
	)
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

type getGradingRequest struct {
	OrgSlug  string `json:"org_slug"`
	RepoSlug string `json:"repo_slug"`
}

func (h *Handler) GetGrading(
	w stdhttp.ResponseWriter,
	r *stdhttp.Request,
) {
	var req getGradingRequest

	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}

	result, err := h.service.GetGrading(
		r.Context(),
		clientIP(r),
		mapGetGradingRequest(req),
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

func mapGetGradingRequest(
	req getGradingRequest,
) domain.GetGradingDTO {
	return domain.GetGradingDTO{
		OrgSlug:  strings.TrimSpace(req.OrgSlug),
		RepoSlug: strings.TrimSpace(req.RepoSlug),
	}
}

type requestLLMRecommendationsRequest struct {
	OrgSlug  string `json:"org_slug"`
	RepoSlug string `json:"repo_slug"`
}

func (h *Handler) RequestLLMRecommendations(
	w stdhttp.ResponseWriter,
	r *stdhttp.Request,
) {
	var req requestLLMRecommendationsRequest

	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}

	result, err := h.service.RequestLLMRecommendations(
		r.Context(),
		clientIP(r),
		mapRequestLLMRecommendationsRequest(req),
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

func mapRequestLLMRecommendationsRequest(
	req requestLLMRecommendationsRequest,
) domain.RequestLLMRecommendationsDTO {
	return domain.RequestLLMRecommendationsDTO{
		OrgSlug:  strings.TrimSpace(req.OrgSlug),
		RepoSlug: strings.TrimSpace(req.RepoSlug),
	}
}

type getRankingRequest struct {
	Page     int32  `json:"page"`
	PageSize int32  `json:"page_size"`
	Language string `json:"language"`
	SortBy   string `json:"sort_by"`
}

func (h *Handler) GetRanking(
	w stdhttp.ResponseWriter,
	r *stdhttp.Request,
) {
	var req getRankingRequest

	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}

	dto, err := mapGetRankingRequest(req)
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

func mapGetRankingRequest(
	req getRankingRequest,
) (domain.GetRankingDTO, error) {
	sortBy, err := mapHTTPRankingSortBy(req.SortBy)
	if err != nil {
		return domain.GetRankingDTO{}, err
	}

	return domain.GetRankingDTO{
		Page:     req.Page,
		PageSize: req.PageSize,
		Language: strings.TrimSpace(req.Language),
		SortBy:   sortBy,
	}, nil
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
				"%w: invalid sort_by",
				domain.ErrInvalidRequest,
			),
		}
	}
}

type loginResponse struct {
	SessionID string `json:"session_id"`
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
	Grade repoGradeResponse `json:"grade"`
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
	RawResponse json.RawMessage `json:"raw_response"`
	ModelUsed   string          `json:"model_used"`
	EvaluatedAt string          `json:"evaluated_at"`
}

type getRankingResponse struct {
	Entries    []rankingEntryResponse `json:"entries"`
	TotalCount int32                  `json:"total_count"`
	Page       int32                  `json:"page"`
	PageSize   int32                  `json:"page_size"`
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
		Grade: mapRepoGradeResponse(result.Grade),
	}
}

func mapRepoGradeResponse(
	grade domain.RepoGrade,
) repoGradeResponse {
	return repoGradeResponse{
		SourcecraftUUID: grade.SourcecraftUUID.String(),
		Score:           grade.Score,
		CategoryScores:  append([]byte(nil), grade.CategoryScores...),
		Advice:          append([]byte(nil), grade.Advice...),
		MissingData:     append([]string(nil), grade.MissingData...),
		GradedAt:        grade.GradedAt.Format(time.RFC3339Nano),
	}
}

func mapRequestLLMRecommendationsResponse(
	result domain.RequestLLMRecommendationsResponse,
) requestLLMRecommendationsResponse {
	return requestLLMRecommendationsResponse{
		RawResponse: append([]byte(nil), result.RawResponse...),
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

	return result
}

func sessionIDFromRequest(
	r *stdhttp.Request,
) (string, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		if errors.Is(err, stdhttp.ErrNoCookie) {
			return "", httpError{
				status: stdhttp.StatusUnauthorized,
				err:    domain.ErrUnauthenticated,
			}
		}

		return "", httpError{
			status: stdhttp.StatusBadRequest,
			err:    err,
		}
	}

	sessionID := strings.TrimSpace(cookie.Value)

	if sessionID == "" {
		return "", httpError{
			status: stdhttp.StatusUnauthorized,
			err:    domain.ErrUnauthenticated,
		}
	}

	return sessionID, nil
}

func clientIP(
	r *stdhttp.Request,
) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}

	return r.RemoteAddr
}

func decodeJSON(
	r *stdhttp.Request,
	dst any,
) error {
	if r.Body == nil {
		return httpError{
			status: stdhttp.StatusBadRequest,
			err: fmt.Errorf(
				"%w: empty request body",
				domain.ErrInvalidRequest,
			),
		}
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return httpError{
			status: stdhttp.StatusBadRequest,
			err: fmt.Errorf(
				"%w: invalid json",
				domain.ErrInvalidRequest,
			),
		}
	}

	var extra any

	if err := decoder.Decode(&extra); err != io.EOF {
		return httpError{
			status: stdhttp.StatusBadRequest,
			err: fmt.Errorf(
				"%w: request body must contain exactly one json value",
				domain.ErrInvalidRequest,
			),
		}
	}

	return nil
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

func writeError(
	w stdhttp.ResponseWriter,
	err error,
) {
	status := mapErrorStatus(err)

	writeJSON(
		w,
		status,
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
	case errors.Is(err, domain.ErrUnauthenticated):
		return stdhttp.StatusUnauthorized

	case errors.Is(err, domain.ErrSessionExpired):
		return stdhttp.StatusUnauthorized

	case errors.Is(err, domain.ErrSessionNotFound):
		return stdhttp.StatusUnauthorized

	case errors.Is(err, domain.ErrIdentityInvalid):
		return stdhttp.StatusUnauthorized

	case errors.Is(err, domain.ErrIdentityNotFound):
		return stdhttp.StatusUnauthorized

	case errors.Is(err, domain.ErrInvalidRequest):
		return stdhttp.StatusBadRequest

	case errors.Is(err, domain.ErrInvalidUser):
		return stdhttp.StatusBadRequest

	case errors.Is(err, domain.ErrUserAlreadyExists):
		return stdhttp.StatusConflict

	case errors.Is(err, domain.ErrUserNotFound):
		return stdhttp.StatusNotFound

	case errors.Is(err, domain.ErrLimitExceeded):
		return stdhttp.StatusTooManyRequests

	case errors.Is(err, domain.ErrTimeout):
		return stdhttp.StatusGatewayTimeout

	case errors.Is(err, domain.ErrRequestFailed):
		return stdhttp.StatusBadGateway

	default:
		return stdhttp.StatusInternalServerError
	}
}

func publicErrorMessage(err error) string {
	switch {
	case errors.Is(err, domain.ErrUnauthenticated):
		return "unauthenticated"

	case errors.Is(err, domain.ErrSessionExpired):
		return "session expired"

	case errors.Is(err, domain.ErrSessionNotFound):
		return "session expired"

	case errors.Is(err, domain.ErrIdentityInvalid):
		return "invalid identity"

	case errors.Is(err, domain.ErrIdentityNotFound):
		return "identity not found"

	case errors.Is(err, domain.ErrInvalidRequest):
		return "invalid request"

	case errors.Is(err, domain.ErrLimitExceeded):
		return "rate limit exceeded"

	case errors.Is(err, domain.ErrTimeout):
		return "upstream timeout"

	case errors.Is(err, domain.ErrRequestFailed):
		return "request failed"

	default:
		return "internal server error"
	}
}
