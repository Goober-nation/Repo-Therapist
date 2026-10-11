package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"aggregator-service/internal/domain"

	"repo-therapist/runtimeconfig"
)

const settingsYAML = `
shared:
  clone_timeout: 10m
gateway:
  rate_limit: 60
llm:
  llm_docs_mode: algorithmic
admin_editable:
  - {key: gateway.rate_limit, type: int, min: "1", max: "1000", group: rate_limits, title: Лимит}
  - {key: shared.clone_timeout, type: duration, min: 30s, max: 2h, group: grading, title: Клон}
  - {key: llm.llm_docs_mode, type: enum, values: [auto, llm, algorithmic], group: llm, title: Режим}
`

type fakeSettingsRepo struct {
	rows map[string]domain.SettingOverride
}

func (f *fakeSettingsRepo) ListOverrides(context.Context) ([]domain.SettingOverride, error) {
	var out []domain.SettingOverride
	for _, o := range f.rows {
		out = append(out, o)
	}
	return out, nil
}

func (f *fakeSettingsRepo) UpsertOverride(_ context.Context, o domain.SettingOverride) (*domain.SettingOverride, error) {
	prev, had := f.rows[o.Key]
	f.rows[o.Key] = o
	if !had {
		return nil, nil
	}
	return &prev, nil
}

func (f *fakeSettingsRepo) DeleteOverride(_ context.Context, key string) (*domain.SettingOverride, error) {
	prev, had := f.rows[key]
	delete(f.rows, key)
	if !had {
		return nil, nil
	}
	return &prev, nil
}

type fakePublisher struct {
	set     map[string]string
	deleted []string
	err     error
}

func (p *fakePublisher) Set(_ context.Context, key, value string, _ time.Time) error {
	if p.err != nil {
		return p.err
	}
	p.set[key] = value
	return nil
}

func (p *fakePublisher) Delete(_ context.Context, key string) error {
	p.deleted = append(p.deleted, key)
	return p.err
}

func settingsService(t *testing.T, processes []domain.ConfigProcess, ownLocked map[string]bool) (*AggregatorService, *fakeSettingsRepo, *fakePublisher, *fakeAuditRepo) {
	t.Helper()
	file, err := runtimeconfig.Parse([]byte(settingsYAML))
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeSettingsRepo{rows: map[string]domain.SettingOverride{}}
	pub := &fakePublisher{set: map[string]string{}}
	audit := &fakeAuditRepo{}
	s := auditService(audit)
	s.Settings = &Settings{
		Repo: repo, Publisher: pub, File: func() *runtimeconfig.File { return file },
		Processes:    func() []domain.ConfigProcess { return processes },
		OwnEnvLocked: func(key string) bool { return ownLocked[key] },
	}
	return s, repo, pub, audit
}

func lastAudit(t *testing.T, audit *fakeAuditRepo) (domain.AuditEvent, map[string]any) {
	t.Helper()
	if len(audit.inserted) == 0 {
		t.Fatal("nothing audited")
	}
	event := audit.inserted[len(audit.inserted)-1]
	var details map[string]any
	_ = json.Unmarshal(event.Details, &details)
	return event, details
}

func TestSetSettingStoresPublishesAndAuditsOldNew(t *testing.T) {
	s, repo, pub, audit := settingsService(t, nil, nil)
	ctx := context.Background()

	if _, err := s.SetSetting(ctx, "gateway.rate_limit", " 120 ", "abc"); err != nil {
		t.Fatal(err)
	}
	if repo.rows["gateway.rate_limit"].Value != "120" || pub.set["gateway.rate_limit"] != "120" {
		t.Fatalf("stored %v, published %v", repo.rows, pub.set)
	}
	event, details := lastAudit(t, audit)
	if event.Action != domain.AuditActionSettingSet || event.TargetKind != domain.AuditTargetSetting ||
		event.Target != "gateway.rate_limit" || event.Outcome != domain.AuditOutcomeOK || event.ActorHash != "abc" {
		t.Errorf("audit event %+v", event)
	}
	if details["old"] != "60" || details["old_source"] != "file" || details["new"] != "120" {
		t.Errorf("details %v", details)
	}

	if _, err := s.SetSetting(ctx, "gateway.rate_limit", "200", "abc"); err != nil {
		t.Fatal(err)
	}
	if _, details := lastAudit(t, audit); details["old"] != "120" || details["old_source"] != "override" {
		t.Errorf("second change details %v", details)
	}
}

func TestSetSettingRejects(t *testing.T) {
	processes := []domain.ConfigProcess{
		{Name: "gateway", Sections: []string{"shared", "gateway", "frontend"}, EnvLocked: []string{"gateway.rate_limit"}},
		// llm does not read gateway: its lock on a gateway key does not count.
		{Name: "llm-service", Sections: []string{"shared", "llm"}, EnvLocked: []string{"gateway.rate_limit"}},
	}
	s, repo, pub, audit := settingsService(t, processes, map[string]bool{"shared.clone_timeout": true})
	ctx := context.Background()

	cases := []struct {
		key, value string
		want       error
	}{
		{"gateway.static.http_address", ":1", domain.ErrSettingUnknown},
		{"llm.llm_docs_mode", "magic", domain.ErrSettingInvalid},
		{"gateway.rate_limit", "0", domain.ErrSettingInvalid},
		{"gateway.rate_limit", "100", domain.ErrSettingEnvLocked},
		{"shared.clone_timeout", "5m", domain.ErrSettingEnvLocked},
	}
	for _, c := range cases {
		if _, err := s.SetSetting(ctx, c.key, c.value, "abc"); !errors.Is(err, c.want) {
			t.Errorf("SetSetting(%s, %s) = %v, want %v", c.key, c.value, err, c.want)
		}
	}
	if len(repo.rows) != 0 || len(pub.set) != 0 {
		t.Errorf("a rejected change was stored %v or published %v", repo.rows, pub.set)
	}
	if event, details := lastAudit(t, audit); event.Outcome != domain.AuditOutcomeDenied || details["reason"] != "env_locked" {
		t.Errorf("denied audit %+v %v", event, details)
	}

	page, err := s.ListSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	locks := map[string][]string{}
	for _, v := range page.Settings {
		locks[v.Key] = v.EnvLockedBy
	}
	if got := locks["gateway.rate_limit"]; len(got) != 1 || got[0] != "gateway" {
		t.Errorf("gateway.rate_limit locked by %v, want [gateway]", got)
	}
	if got := locks["shared.clone_timeout"]; len(got) != 1 || got[0] != "aggregator" {
		t.Errorf("shared.clone_timeout locked by %v, want [aggregator]", got)
	}
}

func TestResetSettingTombstonesAndAudits(t *testing.T) {
	s, repo, pub, audit := settingsService(t, nil, nil)
	ctx := context.Background()
	repo.rows["llm.llm_docs_mode"] = domain.SettingOverride{Key: "llm.llm_docs_mode", Value: "auto"}

	view, err := s.ResetSetting(ctx, "llm.llm_docs_mode", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if view.Override != nil || len(repo.rows) != 0 || len(pub.deleted) != 1 || pub.deleted[0] != "llm.llm_docs_mode" {
		t.Errorf("view %+v rows %v deleted %v", view, repo.rows, pub.deleted)
	}
	event, details := lastAudit(t, audit)
	if event.Action != domain.AuditActionSettingReset || details["old"] != "auto" || details["new"] != "algorithmic" {
		t.Errorf("reset audit %+v %v", event, details)
	}
}

func TestPublishFailureIsAnErrorAndAudited(t *testing.T) {
	s, _, pub, audit := settingsService(t, nil, nil)
	pub.err = errors.New("kafka down")

	if _, err := s.SetSetting(context.Background(), "gateway.rate_limit", "100", "abc"); err == nil {
		t.Fatal("a failed publish must be reported")
	}
	if event, _ := lastAudit(t, audit); event.Outcome != domain.AuditOutcomeError {
		t.Errorf("outcome %s", event.Outcome)
	}
}

func TestReconcileSettings(t *testing.T) {
	s, repo, pub, _ := settingsService(t, nil, nil)
	repo.rows["gateway.rate_limit"] = domain.SettingOverride{Key: "gateway.rate_limit", Value: "100"}
	repo.rows["llm.llm_docs_mode"] = domain.SettingOverride{Key: "llm.llm_docs_mode", Value: "auto"}
	onTopic := map[string]string{"llm.llm_docs_mode": "auto", "gateway.rate_limit": "90", "shared.clone_timeout": "5m"}

	if err := s.ReconcileSettings(context.Background(), onTopic); err != nil {
		t.Fatal(err)
	}
	if len(pub.set) != 1 || pub.set["gateway.rate_limit"] != "100" {
		t.Errorf("republished %v, want only the changed row", pub.set)
	}
	if len(pub.deleted) != 1 || pub.deleted[0] != "shared.clone_timeout" {
		t.Errorf("tombstoned %v, want the key without a row", pub.deleted)
	}
}
