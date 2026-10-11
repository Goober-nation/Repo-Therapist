package http

import (
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"time"

	"gateway-service/internal/domain"
)

// maxSettingBodyBytes bounds PUT /admin/settings/{key}: one short value.
const maxSettingBodyBytes = 4 * 1024

type settingResponse struct {
	Key           string     `json:"key"`
	Group         string     `json:"group"`
	Title         string     `json:"title"`
	Type          string     `json:"type"`
	Min           string     `json:"min,omitempty"`
	Max           string     `json:"max,omitempty"`
	Values        []string   `json:"values,omitempty"`
	FileValue     *string    `json:"file_value"`
	Override      *string    `json:"override"`
	UpdatedAt     *time.Time `json:"updated_at"`
	UpdatedBy     string     `json:"updated_by,omitempty"`
	EnvLockedBy   []string   `json:"env_locked_by"`
	EffectiveFrom string     `json:"effective_from"`
}

type configProcessResponse struct {
	Name           string     `json:"name"`
	Sections       []string   `json:"sections"`
	RestartPending []string   `json:"restart_pending"`
	ReportedAt     *time.Time `json:"reported_at"`
}

func settingBody(v domain.Setting) settingResponse {
	out := settingResponse{
		Key: v.Key, Group: v.Group, Title: v.Title, Type: v.Type, Min: v.Min, Max: v.Max, Values: v.Values,
		UpdatedAt: v.UpdatedAt, UpdatedBy: v.UpdatedBy, EnvLockedBy: v.EnvLockedBy,
	}

	if out.EnvLockedBy == nil {
		out.EnvLockedBy = []string{}
	}

	if v.FileSet {
		value := v.FileValue
		out.FileValue = &value
	}

	if v.Overridden {
		value := v.OverrideValue
		out.Override = &value
	}

	// Where the running value comes from: env > override > app.yaml > code default.
	switch {
	case len(v.EnvLockedBy) > 0:
		out.EffectiveFrom = "env"
	case v.Overridden:
		out.EffectiveFrom = "override"
	case v.FileSet:
		out.EffectiveFrom = "file"
	default:
		out.EffectiveFrom = "default"
	}

	return out
}

// GetSettings serves GET /admin/settings: the admin-editable settings and the processes that
// reported their configuration.
func (h *Handler) GetSettings(w stdhttp.ResponseWriter, r *stdhttp.Request, _ domain.AdminIdentity) {
	page, err := h.service.ListSettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	settings := make([]settingResponse, 0, len(page.Settings))
	for _, v := range page.Settings {
		settings = append(settings, settingBody(v))
	}

	processes := make([]configProcessResponse, 0, len(page.Processes))
	for _, p := range page.Processes {
		process := configProcessResponse{Name: p.Name, Sections: p.Sections, RestartPending: p.RestartPending, ReportedAt: p.ReportedAt}
		if process.RestartPending == nil {
			process.RestartPending = []string{}
		}

		processes = append(processes, process)
	}

	writeJSON(w, stdhttp.StatusOK, map[string]any{"settings": settings, "processes": processes})
}

// PutSetting serves PUT /admin/settings/{key} with {"value": "..."}: stores an override.
func (h *Handler) PutSetting(w stdhttp.ResponseWriter, r *stdhttp.Request, admin domain.AdminIdentity) {
	var body struct {
		Value *string `json:"value"`
	}

	dec := json.NewDecoder(stdhttp.MaxBytesReader(w, r.Body, maxSettingBodyBytes))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&body); err != nil || body.Value == nil {
		writeError(w, httpError{status: stdhttp.StatusBadRequest, err: fmt.Errorf("%w: body", domain.ErrInvalidRequest)})
		return
	}

	v, err := h.service.SetSetting(r.Context(), admin.ActorHash, r.PathValue("key"), *body.Value)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, stdhttp.StatusOK, settingBody(v))
}

// DeleteSetting serves DELETE /admin/settings/{key}: drops the override, app.yaml applies again.
func (h *Handler) DeleteSetting(w stdhttp.ResponseWriter, r *stdhttp.Request, admin domain.AdminIdentity) {
	v, err := h.service.ResetSetting(r.Context(), admin.ActorHash, r.PathValue("key"))
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, stdhttp.StatusOK, settingBody(v))
}

// GetConfig serves GET /config: the SPA's runtime settings (the `frontend` section of app.yaml).
// Public and not secret; re-read on every request, so app.yaml edits apply on the next page load.
func (h *Handler) GetConfig(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
	w.Header().Set("Cache-Control", "no-cache")

	c := h.service.PublicConfig()
	writeJSON(w, stdhttp.StatusOK, map[string]any{
		"public_url": c.PublicURL, "yandex_client_id": c.YandexClientID,
		"dev_auth_bypass": c.DevAuthBypass, "poll_timeout_ms": c.PollTimeoutMS,
	})
}
