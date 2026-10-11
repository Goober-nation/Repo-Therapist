package service

import (
	"context"
	"strconv"
	"strings"

	"gateway-service/internal/domain"
	agg "gateway-service/internal/transport/rpc/gen/agg"

	"repo-therapist/runtimeconfig"
)

// DefaultPollTimeoutMS is how long the SPA waits for one grading when app.yaml does not say.
const DefaultPollTimeoutMS = 15 * 60_000

// ListSettings returns the admin settings page; the caller has already been checked as an admin.
func (s *GatewayService) ListSettings(ctx context.Context) (domain.SettingsPage, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, s.aggregatorRPCTimeout())
	defer cancel()

	resp, err := s.aggregatorClient.ListSettings(rpcCtx, &agg.ListSettingsRequest{})
	if err != nil {
		return domain.SettingsPage{}, batchError("list settings", err)
	}

	page := domain.SettingsPage{}
	for _, v := range resp.GetSettings() {
		page.Settings = append(page.Settings, settingFromProto(v))
	}

	for _, p := range resp.GetProcesses() {
		process := domain.ConfigProcess{Name: p.GetName(), Sections: p.GetSections(), RestartPending: p.GetRestartPending()}
		if p.ReportedAt != nil {
			t := p.GetReportedAt().AsTime()
			process.ReportedAt = &t
		}

		page.Processes = append(page.Processes, process)
	}

	return page, nil
}

// SetSetting stores an admin override; the Aggregator validates it and records it in the audit log.
func (s *GatewayService) SetSetting(ctx context.Context, actorHash, key, value string) (domain.Setting, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, s.aggregatorRPCTimeout())
	defer cancel()

	resp, err := s.aggregatorClient.SetSetting(rpcCtx, &agg.SetSettingRequest{Key: key, Value: value, ActorHash: actorHash})
	if err != nil {
		return domain.Setting{}, batchError("set setting", err)
	}

	return settingFromProto(resp.GetSetting()), nil
}

// ResetSetting drops an admin override so the app.yaml value applies again.
func (s *GatewayService) ResetSetting(ctx context.Context, actorHash, key string) (domain.Setting, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, s.aggregatorRPCTimeout())
	defer cancel()

	resp, err := s.aggregatorClient.ResetSetting(rpcCtx, &agg.ResetSettingRequest{Key: key, ActorHash: actorHash})
	if err != nil {
		return domain.Setting{}, batchError("reset setting", err)
	}

	return settingFromProto(resp.GetSetting()), nil
}

// PublicConfig is the `frontend` section of app.yaml, resolved per request (env > admin override >
// app.yaml). The dev sign-in button is offered only while the Gateway really runs the bypass.
func (s *GatewayService) PublicConfig() domain.PublicConfig {
	pollMS, err := strconv.Atoi(strings.TrimSpace(runtimeconfig.Getenv("POLL_TIMEOUT_MS")))
	if err != nil || pollMS < 1 {
		pollMS = DefaultPollTimeoutMS
	}

	return domain.PublicConfig{
		PublicURL:      strings.TrimSpace(runtimeconfig.Getenv("VITE_PUBLIC_URL")),
		YandexClientID: strings.TrimSpace(runtimeconfig.Getenv("VITE_YANDEX_CLIENT_ID")),
		DevAuthBypass:  runtimeconfig.Getenv("VITE_DEV_AUTH_BYPASS") == "true" && s.devAuthBypass,
		PollTimeoutMS:  pollMS,
	}
}

func settingFromProto(v *agg.Setting) domain.Setting {
	out := domain.Setting{
		Key: v.GetKey(), Group: v.GetGroup(), Title: v.GetTitle(), Type: v.GetType(), Min: v.GetMin(), Max: v.GetMax(),
		Values: v.GetValues(), FileValue: v.GetFileValue(), FileSet: v.GetFileSet(), Overridden: v.GetOverridden(),
		OverrideValue: v.GetOverrideValue(), UpdatedBy: v.GetUpdatedBy(), EnvLockedBy: v.GetEnvLockedBy(),
	}

	if v.UpdatedAt != nil {
		t := v.GetUpdatedAt().AsTime()
		out.UpdatedAt = &t
	}

	return out
}
