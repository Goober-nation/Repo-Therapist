package domain

import (
	"context"
	"errors"
	"time"
)

// Admin overrides of config/app.yaml settings (docs/config.md).
var (
	// ErrSettingUnknown is a key that is not admin-editable in app.yaml.
	ErrSettingUnknown = errors.New("setting is not admin-editable")
	// ErrSettingInvalid is a value outside the key's type or bounds.
	ErrSettingInvalid = errors.New("invalid setting value")
	// ErrSettingEnvLocked is a key the environment of a process that uses it sets: the environment
	// always wins, so an override would never apply.
	ErrSettingEnvLocked = errors.New("setting is set in the environment")
)

// AuditTargetSetting is an app.yaml key; Target is its path.
const AuditTargetSetting = "setting"

// Audit actions of the settings page.
const (
	AuditActionSettingSet   = "settings.set"
	AuditActionSettingReset = "settings.reset"
)

// SettingOverride is one stored override.
type SettingOverride struct {
	Key       string
	Value     string
	UpdatedBy string
	UpdatedAt time.Time
}

// SettingsRepository stores the overrides (table settings_override).
type SettingsRepository interface {
	ListOverrides(ctx context.Context) ([]SettingOverride, error)
	// UpsertOverride stores o and returns the override it replaced, if any.
	UpsertOverride(ctx context.Context, o SettingOverride) (*SettingOverride, error)
	// DeleteOverride removes key's override and returns it, or nil when there was none.
	DeleteOverride(ctx context.Context, key string) (*SettingOverride, error)
}

// SettingsPublisher delivers overrides to the services (Kafka topic config.updated).
type SettingsPublisher interface {
	Set(ctx context.Context, key, value string, at time.Time) error
	Delete(ctx context.Context, key string) error
}

// ConfigProcess is what one running process last reported about its configuration.
type ConfigProcess struct {
	Name           string
	Sections       []string
	EnvLocked      []string
	RestartPending []string
	ReportedAt     time.Time
}

// SettingView is one admin-editable key as the admin page shows it.
type SettingView struct {
	Key, Group, Title, Type string
	Min, Max                string
	Values                  []string
	FileValue               string
	FileSet                 bool
	Override                *SettingOverride
	EnvLockedBy             []string
}

// SettingsPage is the whole settings page.
type SettingsPage struct {
	Settings  []SettingView
	Processes []ConfigProcess
}
