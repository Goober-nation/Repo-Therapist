package http

import (
	"encoding/json"
	"errors"
	"fmt"
	stdhttp "net/http"
	"strconv"
	"time"

	"gateway-service/internal/domain"
)

// maxBatchBodyBytes bounds a filter body: up to 200 hand-picked slugs fit well inside it.
const maxBatchBodyBytes = 32 * 1024

type batchFilterBody struct {
	Languages     []string `json:"languages"`
	MinScore      *int32   `json:"min_score"`
	MaxScore      *int32   `json:"max_score"`
	OlderThanDays *int32   `json:"older_than_days"`
	Repos         []string `json:"repos"`
	TopRated      *int32   `json:"top_rated"`
	All           bool     `json:"all"`
	Force         bool     `json:"force"`
}

func (b batchFilterBody) domain() domain.BatchFilter {
	return domain.BatchFilter{Languages: b.Languages, MinScore: b.MinScore, MaxScore: b.MaxScore,
		OlderThanDays: b.OlderThanDays, Repos: b.Repos, TopRated: b.TopRated, All: b.All, Force: b.Force}
}

func filterBody(f domain.BatchFilter) batchFilterBody {
	return batchFilterBody{Languages: f.Languages, MinScore: f.MinScore, MaxScore: f.MaxScore,
		OlderThanDays: f.OlderThanDays, Repos: f.Repos, TopRated: f.TopRated, All: f.All, Force: f.Force}
}

type batchResponse struct {
	ID         int64           `json:"id"`
	CreatedAt  time.Time       `json:"created_at"`
	FinishedAt *time.Time      `json:"finished_at"`
	Status     string          `json:"status"`
	Actor      string          `json:"actor"`
	Filter     batchFilterBody `json:"filter"`
	Total      int32           `json:"total"`
	Pending    int32           `json:"pending"`
	Running    int32           `json:"running"`
	OK         int32           `json:"ok"`
	Failed     int32           `json:"failed"`
	Skipped    int32           `json:"skipped"`
}

func batchBody(b domain.RegradeBatch) batchResponse {
	return batchResponse{ID: b.ID, CreatedAt: b.CreatedAt, FinishedAt: b.FinishedAt, Status: b.Status, Actor: b.ActorHash,
		Filter: filterBody(b.Filter), Total: b.Total, Pending: b.Pending, Running: b.Running, OK: b.OK,
		Failed: b.Failed, Skipped: b.Skipped}
}

func readFilter(w stdhttp.ResponseWriter, r *stdhttp.Request) (domain.BatchFilter, bool) {
	var body batchFilterBody

	dec := json.NewDecoder(stdhttp.MaxBytesReader(w, r.Body, maxBatchBodyBytes))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&body); err != nil {
		writeError(w, httpError{status: stdhttp.StatusBadRequest, err: fmt.Errorf("%w: body", domain.ErrInvalidRequest)})
		return domain.BatchFilter{}, false
	}

	return body.domain(), true
}

// PostRegradePreview serves POST /admin/regrade/preview: the dry run of a filter. Starts nothing.
func (h *Handler) PostRegradePreview(w stdhttp.ResponseWriter, r *stdhttp.Request, _ domain.AdminIdentity) {
	filter, ok := readFilter(w, r)
	if !ok {
		return
	}

	p, err := h.service.PreviewRegradeBatch(r.Context(), filter)
	if err != nil {
		writeError(w, err)
		return
	}

	nonNil := func(s []string) []string {
		if s == nil {
			return []string{}
		}
		return s
	}

	writeJSON(w, stdhttp.StatusOK, map[string]any{
		"count": p.Count, "capped": p.Capped, "sample": nonNil(p.Sample),
		"unmatched": nonNil(p.Unmatched), "estimate_seconds": p.EstimateSeconds,
		"inactive": p.Inactive,
	})
}

// PostRegradeBatch serves POST /admin/regrade/batches: starts a batch.
func (h *Handler) PostRegradeBatch(w stdhttp.ResponseWriter, r *stdhttp.Request, admin domain.AdminIdentity) {
	filter, ok := readFilter(w, r)
	if !ok {
		return
	}

	batch, err := h.service.StartRegradeBatch(r.Context(), admin.ActorHash, filter)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, stdhttp.StatusCreated, batchBody(batch))
}

// GetRegradeBatches serves GET /admin/regrade/batches?limit=: recent batches, newest first.
func (h *Handler) GetRegradeBatches(w stdhttp.ResponseWriter, r *stdhttp.Request, _ domain.AdminIdentity) {
	var limit int32

	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 0 {
			writeError(w, invalidAuditParam("limit"))
			return
		}

		limit = int32(n)
	}

	batches, err := h.service.ListRegradeBatches(r.Context(), limit)
	if err != nil {
		writeError(w, err)
		return
	}

	out := make([]batchResponse, 0, len(batches))
	for _, b := range batches {
		out = append(out, batchBody(b))
	}

	writeJSON(w, stdhttp.StatusOK, map[string]any{"batches": out})
}

// GetRegradeBatch serves GET /admin/regrade/batches/{id}; id "current" is the active batch, or the latest
// (200 with a null batch when there has never been one).
func (h *Handler) GetRegradeBatch(w stdhttp.ResponseWriter, r *stdhttp.Request, _ domain.AdminIdentity) {
	id, ok := batchID(w, r)
	if !ok {
		return
	}

	detail, err := h.service.GetRegradeBatch(r.Context(), id)
	if err != nil {
		// "current" before the first batch is the normal state of a fresh stand, not an error.
		if id == 0 && errors.Is(err, domain.ErrNotFound) {
			writeJSON(w, stdhttp.StatusOK, map[string]any{"batch": nil, "failures": []string{}})
			return
		}

		writeError(w, err)
		return
	}

	failures := make([]map[string]string, 0, len(detail.Failures))
	for _, f := range detail.Failures {
		failures = append(failures, map[string]string{"repo": f.OrgSlug + "/" + f.RepoSlug, "error": f.Error})
	}

	writeJSON(w, stdhttp.StatusOK, map[string]any{"batch": batchBody(detail.Batch), "failures": failures})
}

// PostRegradeControl serves POST /admin/regrade/batches/{id}/{action} for pause, resume and cancel.
func (h *Handler) PostRegradeControl(w stdhttp.ResponseWriter, r *stdhttp.Request, admin domain.AdminIdentity) {
	id, ok := batchID(w, r)
	if !ok || id == 0 {
		if ok {
			writeError(w, invalidAuditParam("id"))
		}
		return
	}

	batch, err := h.service.ControlRegradeBatch(r.Context(), admin.ActorHash, id, r.PathValue("action"))
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, stdhttp.StatusOK, batchBody(batch))
}

func batchID(w stdhttp.ResponseWriter, r *stdhttp.Request) (int64, bool) {
	raw := r.PathValue("id")
	if raw == "current" {
		return 0, true
	}

	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, invalidAuditParam("id"))
		return 0, false
	}

	return id, true
}
