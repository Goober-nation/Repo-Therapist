package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"aggregator-service/internal/domain"

	"repo-therapist/runtimeconfig"
)

// Settings is the admin settings page (docs/config.md): the admin-editable keys of config/app.yaml,
// their overrides in settings_override, delivered to the services over config.updated. Set before
// the service runs; a nil Settings leaves the page off.
type Settings struct {
	Repo      domain.SettingsRepository
	Publisher domain.SettingsPublisher
	// File returns the app.yaml the Aggregator loaded last (it knows every section's keys).
	File func() *runtimeconfig.File
	// Processes returns what each running process last reported on config.status.
	Processes func() []domain.ConfigProcess
	// OwnEnvLocked reports whether the Aggregator's own environment sets the key (for keys it reads
	// itself, before its own status report has come back from Kafka).
	OwnEnvLocked func(key string) bool
}

// ListSettings returns every admin-editable key with its file value, override and env locks.
func (as *AggregatorService) ListSettings(ctx context.Context) (domain.SettingsPage, error) {
	s := as.Settings
	if s == nil {
		return domain.SettingsPage{}, fmt.Errorf("%w: settings are off", domain.ErrSettingUnknown)
	}

	overrides, err := s.Repo.ListOverrides(ctx)
	if err != nil {
		return domain.SettingsPage{}, err
	}

	byKey := map[string]domain.SettingOverride{}
	for _, o := range overrides {
		byKey[o.Key] = o
	}

	file := s.File()
	processes := s.Processes()
	keys := make([]string, 0, len(file.Editable))

	for key := range file.Editable {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	page := domain.SettingsPage{Processes: processes}
	for _, key := range keys {
		var override *domain.SettingOverride
		if o, ok := byKey[key]; ok {
			override = &o
		}

		page.Settings = append(page.Settings, settingView(file, key, override, envLockedBy(s, processes, key)))
	}

	// Editable keys keep their order of app.yaml groups for the page: rate limits, grading, LLM.
	sort.SliceStable(page.Settings, func(i, j int) bool {
		return groupRank(page.Settings[i].Group) < groupRank(page.Settings[j].Group)
	})

	return page, nil
}

// SetSetting stores and publishes an override, then audits it with the old and new values. Values of
// admin-editable keys are not secrets, so they go to the audit log as they are.
func (as *AggregatorService) SetSetting(ctx context.Context, key, value, actorHash string) (domain.SettingView, error) {
	s := as.Settings
	if s == nil {
		return domain.SettingView{}, fmt.Errorf("%w: settings are off", domain.ErrSettingUnknown)
	}

	value = strings.TrimSpace(value)
	file := s.File()

	editable, ok := file.Editable[key]
	if !ok {
		return domain.SettingView{}, fmt.Errorf("%w: %s", domain.ErrSettingUnknown, key)
	}

	if err := editable.Validate(value); err != nil {
		as.auditSetting(ctx, domain.AuditActionSettingSet, key, actorHash, domain.AuditOutcomeDenied,
			map[string]any{"reason": "invalid", "new": value})

		return domain.SettingView{}, fmt.Errorf("%w: %s: %v", domain.ErrSettingInvalid, key, err)
	}

	processes := s.Processes()
	if locked := envLockedBy(s, processes, key); len(locked) > 0 {
		as.auditSetting(ctx, domain.AuditActionSettingSet, key, actorHash, domain.AuditOutcomeDenied,
			map[string]any{"reason": "env_locked", "new": value, "env_locked_by": locked})

		return domain.SettingView{}, fmt.Errorf("%w: %s by %s", domain.ErrSettingEnvLocked, key, strings.Join(locked, ", "))
	}

	now := as.currentTime().UTC()
	override := domain.SettingOverride{Key: key, Value: value, UpdatedBy: actorHash, UpdatedAt: now}

	prev, err := s.Repo.UpsertOverride(ctx, override)
	if err != nil {
		return domain.SettingView{}, err
	}

	details := map[string]any{"new": value}
	addOld(details, file, key, prev)

	if err := s.Publisher.Set(ctx, key, value, now); err != nil {
		// Stored but not delivered: the next Aggregator start republishes every stored override, and
		// the admin may simply save again.
		as.auditSetting(ctx, domain.AuditActionSettingSet, key, actorHash, domain.AuditOutcomeError, details)

		return domain.SettingView{}, fmt.Errorf("%w: publish %s: %v", domain.ErrExternalAPI, key, err)
	}

	as.auditSetting(ctx, domain.AuditActionSettingSet, key, actorHash, domain.AuditOutcomeOK, details)

	return settingView(file, key, &override, nil), nil
}

// ResetSetting drops the override so the app.yaml value applies again, and audits the old value.
func (as *AggregatorService) ResetSetting(ctx context.Context, key, actorHash string) (domain.SettingView, error) {
	s := as.Settings
	if s == nil {
		return domain.SettingView{}, fmt.Errorf("%w: settings are off", domain.ErrSettingUnknown)
	}

	file := s.File()
	if _, ok := file.Editable[key]; !ok {
		return domain.SettingView{}, fmt.Errorf("%w: %s", domain.ErrSettingUnknown, key)
	}

	prev, err := s.Repo.DeleteOverride(ctx, key)
	if err != nil {
		return domain.SettingView{}, err
	}

	details := map[string]any{}
	addOld(details, file, key, prev)

	if setting := file.Settings[key]; setting.Set {
		details["new"] = setting.Value
	} else {
		details["new"] = nil
	}

	// A tombstone even without a stored row: it also clears a key the topic still carries.
	if err := s.Publisher.Delete(ctx, key); err != nil {
		as.auditSetting(ctx, domain.AuditActionSettingReset, key, actorHash, domain.AuditOutcomeError, details)

		return domain.SettingView{}, fmt.Errorf("%w: publish reset of %s: %v", domain.ErrExternalAPI, key, err)
	}

	as.auditSetting(ctx, domain.AuditActionSettingReset, key, actorHash, domain.AuditOutcomeOK, details)

	return settingView(file, key, nil, envLockedBy(s, s.Processes(), key)), nil
}

// ReconcileSettings makes config.updated match settings_override at startup: every stored override
// is published again and every key the topic still carries without a row gets a tombstone. The table
// is the source of truth; Kafka compaction is not a backup.
func (as *AggregatorService) ReconcileSettings(ctx context.Context, onTopic map[string]string) error {
	s := as.Settings
	if s == nil {
		return nil
	}

	overrides, err := s.Repo.ListOverrides(ctx)
	if err != nil {
		return err
	}

	stored := map[string]bool{}
	for _, o := range overrides {
		stored[o.Key] = true
		if v, ok := onTopic[o.Key]; ok && v == o.Value {
			continue
		}

		if err := s.Publisher.Set(ctx, o.Key, o.Value, o.UpdatedAt); err != nil {
			return fmt.Errorf("republish %s: %w", o.Key, err)
		}
	}

	for key := range onTopic {
		if !stored[key] {
			if err := s.Publisher.Delete(ctx, key); err != nil {
				return fmt.Errorf("tombstone %s: %w", key, err)
			}
		}
	}

	log.Printf("config: %d stored admin override(s) reconciled with config.updated", len(overrides))

	return nil
}

func settingView(file *runtimeconfig.File, key string, override *domain.SettingOverride, locked []string) domain.SettingView {
	e := file.Editable[key]
	setting := file.Settings[key]

	return domain.SettingView{
		Key: key, Group: e.Group, Title: e.Title, Type: e.Type, Min: e.Min, Max: e.Max, Values: e.Enum,
		FileValue: setting.Value, FileSet: setting.Set, Override: override, EnvLockedBy: locked,
	}
}

// envLockedBy lists the processes that read key's section and whose environment sets the key.
func envLockedBy(s *Settings, processes []domain.ConfigProcess, key string) []string {
	section, _, _ := strings.Cut(key, ".")
	seen := map[string]bool{}

	var out []string

	for _, p := range processes {
		reads := false
		for _, sec := range p.Sections {
			reads = reads || sec == section
		}

		for _, k := range p.EnvLocked {
			if reads && k == key && !seen[p.Name] {
				seen[p.Name] = true
				out = append(out, p.Name)
			}
		}
	}

	if s.OwnEnvLocked != nil && s.OwnEnvLocked(key) && !seen["aggregator"] {
		out = append(out, "aggregator")
	}

	sort.Strings(out)

	return out
}

func addOld(details map[string]any, file *runtimeconfig.File, key string, prev *domain.SettingOverride) {
	switch setting := file.Settings[key]; {
	case prev != nil:
		details["old"], details["old_source"] = prev.Value, "override"
	case setting.Set:
		details["old"], details["old_source"] = setting.Value, "file"
	default:
		details["old"], details["old_source"] = nil, "default"
	}
}

func (as *AggregatorService) auditSetting(ctx context.Context, action, key, actorHash, outcome string, details map[string]any) {
	if as.AuditRepo == nil {
		return
	}

	raw, _ := json.Marshal(details)

	err := as.RecordAudit(ctx, domain.AuditEvent{
		ActorKind: domain.AuditActorAdmin, ActorHash: actorHash, Action: action,
		TargetKind: domain.AuditTargetSetting, Target: key, Outcome: outcome, Details: raw,
	})
	if err != nil {
		log.Printf("audit %s of %s: %v", action, key, err)
	}
}

func groupRank(group string) int {
	switch group {
	case "rate_limits":
		return 0
	case "grading":
		return 1
	case "llm":
		return 2
	}

	return 3
}

// ProcessesFromStatuses is the Settings.Processes adapter for kafkasync statuses.
func ProcessesFromStatuses(statuses map[string]ProcessStatus) []domain.ConfigProcess {
	out := make([]domain.ConfigProcess, 0, len(statuses))
	for name, st := range statuses {
		out = append(out, domain.ConfigProcess{Name: name, Sections: st.Sections, EnvLocked: st.EnvLocked,
			RestartPending: st.RestartPending, ReportedAt: st.ReportedAt})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out
}

// ProcessStatus mirrors kafkasync.Status so this package does not depend on Kafka.
type ProcessStatus struct {
	Sections       []string
	EnvLocked      []string
	RestartPending []string
	ReportedAt     time.Time
}
