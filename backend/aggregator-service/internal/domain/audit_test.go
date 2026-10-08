package domain

import (
	"errors"
	"strings"
	"testing"
)

func validEvent() AuditEvent {
	return AuditEvent{
		ActorKind:  AuditActorAdmin,
		ActorHash:  "0123456789abcdef",
		Action:     "admin.denied",
		TargetKind: AuditTargetRoute,
		Target:     "/admin/audit",
		Outcome:    AuditOutcomeDenied,
		Details:    []byte(`{"reason":"not_admin"}`),
	}
}

func TestAuditEventValidate(t *testing.T) {
	if err := validEvent().Validate(); err != nil {
		t.Fatalf("a valid event was rejected: %v", err)
	}

	cases := map[string]func(*AuditEvent){
		"unknown actor":          func(e *AuditEvent) { e.ActorKind = "root" },
		"uppercase hash":         func(e *AuditEvent) { e.ActorHash = "ABCDEF" },
		"hash too long":          func(e *AuditEvent) { e.ActorHash = strings.Repeat("a", 65) },
		"hash with raw id chars": func(e *AuditEvent) { e.ActorHash = "user@yandex.ru" },
		"action without a dot":   func(e *AuditEvent) { e.Action = "denied" },
		"action with capitals":   func(e *AuditEvent) { e.Action = "Admin.Denied" },
		"action with a space":    func(e *AuditEvent) { e.Action = "admin. denied" },
		"action too long":        func(e *AuditEvent) { e.Action = "admin." + strings.Repeat("a", 70) },
		"empty action":           func(e *AuditEvent) { e.Action = "" },
		"unknown target kind":    func(e *AuditEvent) { e.TargetKind = "file" },
		"target too long":        func(e *AuditEvent) { e.Target = strings.Repeat("x", MaxAuditTargetLength+1) },
		"target without a kind":  func(e *AuditEvent) { e.TargetKind = AuditTargetNone },
		"unknown outcome":        func(e *AuditEvent) { e.Outcome = "maybe" },
		"anonymous with a hash":  func(e *AuditEvent) { e.ActorKind = AuditActorAnonymous },
		"details not an object":  func(e *AuditEvent) { e.Details = []byte(`["a"]`) },
		"details null":           func(e *AuditEvent) { e.Details = []byte(`null`) },
		"details broken":         func(e *AuditEvent) { e.Details = []byte(`{`) },
		"details over the limit": func(e *AuditEvent) { e.Details = []byte(`{"k":"` + strings.Repeat("v", MaxAuditDetailsBytes) + `"}`) },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			event := validEvent()
			mutate(&event)

			if err := event.Validate(); !errors.Is(err, ErrInvalidAudit) {
				t.Fatalf("err = %v, want ErrInvalidAudit", err)
			}
		})
	}
}

func TestAuditEventAllowsMinimalForms(t *testing.T) {
	anonymous := AuditEvent{ActorKind: AuditActorAnonymous, Action: "admin.denied", Outcome: AuditOutcomeDenied}
	if err := anonymous.Validate(); err != nil {
		t.Errorf("anonymous event without hash, target or details: %v", err)
	}

	system := AuditEvent{ActorKind: AuditActorSystem, Action: "audit.purge", Outcome: AuditOutcomeOK, Details: []byte(`{}`)}
	if err := system.Validate(); err != nil {
		t.Errorf("system event: %v", err)
	}
}
