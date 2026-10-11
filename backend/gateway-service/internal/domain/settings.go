package domain

import "time"

// Setting is one admin-editable key of config/app.yaml (docs/config.md) as the admin page shows it.
type Setting struct {
	Key, Group, Title, Type string
	Min, Max                string
	Values                  []string
	FileValue               string
	FileSet                 bool
	Overridden              bool
	OverrideValue           string
	UpdatedAt               *time.Time
	UpdatedBy               string
	// EnvLockedBy lists the processes whose environment sets the key; no override applies there.
	EnvLockedBy []string
}

// ConfigProcess is what one running process last reported about its configuration.
type ConfigProcess struct {
	Name           string
	Sections       []string
	RestartPending []string
	ReportedAt     *time.Time
}

// SettingsPage is the admin settings page.
type SettingsPage struct {
	Settings  []Setting
	Processes []ConfigProcess
}

// PublicConfig is what the SPA reads on load (GET /config): the `frontend` section of app.yaml.
type PublicConfig struct {
	PublicURL      string
	YandexClientID string
	DevAuthBypass  bool
	PollTimeoutMS  int
}
