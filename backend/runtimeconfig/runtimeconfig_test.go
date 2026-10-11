package runtimeconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const baseYAML = `
shared:
  kafka_brokers: localhost:8053
  static:
    s3_bucket: repo-content
gateway:
  rate_limit: 60
  rate_limit_window: 1m
  export_rate_limit:
  static:
    http_address: ":8080"
rating:
  rate_limit: 999
admin_editable:
  - key: gateway.rate_limit
    type: int
    min: "1"
    max: "10000"
    group: rate_limits
    title: Запросов в окно
  - key: gateway.rate_limit_window
    type: duration
    min: 1s
    max: 1h
    group: rate_limits
    title: Окно
`

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func rewrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newStore(t *testing.T, path string) *Store {
	t.Helper()
	s, err := New(path, "gateway")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func expect(t *testing.T, s *Store, key, value string, src Source) {
	t.Helper()
	if got := s.Getenv(key); got != value {
		t.Errorf("%s = %q, want %q", key, got, value)
	}
	if got := s.Source(key); got != src {
		t.Errorf("%s source = %s, want %s", key, got, src)
	}
}

func TestLayersWithoutEnv(t *testing.T) {
	s := newStore(t, writeConfig(t, baseYAML))
	expect(t, s, "RATE_LIMIT", "60", SourceFile)                // own section, not rating's 999
	expect(t, s, "KAFKA_BROKERS", "localhost:8053", SourceFile) // shared
	expect(t, s, "HTTP_ADDRESS", ":8080", SourceFile)           // static
	expect(t, s, "EXPORT_RATE_LIMIT", "", SourceDefault)        // documented, no value
	expect(t, s, "NOT_IN_FILE", "", SourceDefault)
	if _, ok := s.LookupEnv("EXPORT_RATE_LIMIT"); ok {
		t.Error("a key without a value must look unset")
	}
}

// Env wins over every other layer, in every order the layers can change.
func TestEnvWinsEverywhere(t *testing.T) {
	for _, envValue := range []string{"7", ""} {
		name := "value"
		if envValue == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("RATE_LIMIT", envValue)
			t.Setenv("HTTP_ADDRESS", envValue)
			t.Setenv("KAFKA_BROKERS", envValue)
			path := writeConfig(t, baseYAML)
			s := newStore(t, path)

			// against the file
			expect(t, s, "RATE_LIMIT", envValue, SourceEnv)
			expect(t, s, "HTTP_ADDRESS", envValue, SourceEnv)
			expect(t, s, "KAFKA_BROKERS", envValue, SourceEnv)
			if v, ok := s.LookupEnv("RATE_LIMIT"); !ok || v != envValue {
				t.Errorf("LookupEnv = %q, %v", v, ok)
			}

			// against an admin override (Kafka): rejected, nothing stored
			if err := s.SetOverride("gateway.rate_limit", "500"); err == nil {
				t.Error("override of an env-set key must be rejected")
			}
			if len(s.Overrides()) != 0 {
				t.Errorf("overrides = %v, want none", s.Overrides())
			}
			expect(t, s, "RATE_LIMIT", envValue, SourceEnv)

			// against a reset (tombstone)
			s.DeleteOverride("gateway.rate_limit")
			expect(t, s, "RATE_LIMIT", envValue, SourceEnv)

			// against a file reload
			rewrite(t, path, strings.Replace(baseYAML, "rate_limit: 60", "rate_limit: 61", 1))
			if changed, err := s.Reload(); err != nil || !changed {
				t.Fatalf("reload: %v %v", changed, err)
			}
			expect(t, s, "RATE_LIMIT", envValue, SourceEnv)

			// against the environment changing after startup: the snapshot holds
			os.Setenv("RATE_LIMIT", "12345")
			expect(t, s, "RATE_LIMIT", envValue, SourceEnv)

			if !s.EnvLocked("gateway.rate_limit") {
				t.Error("EnvLocked = false")
			}
			locked := strings.Join(s.EnvLockedPaths(), ",")
			for _, want := range []string{"gateway.rate_limit", "gateway.static.http_address", "shared.kafka_brokers"} {
				if !strings.Contains(locked, want) {
					t.Errorf("EnvLockedPaths %s misses %s", locked, want)
				}
			}
		})
	}
}

func TestLoaderNeverWritesProcessEnv(t *testing.T) {
	os.Unsetenv("RATE_LIMIT_WINDOW")
	s := newStore(t, writeConfig(t, baseYAML))
	_ = s.Getenv("RATE_LIMIT_WINDOW")
	if err := s.SetOverride("gateway.rate_limit_window", "5m"); err != nil {
		t.Fatal(err)
	}
	if _, ok := os.LookupEnv("RATE_LIMIT_WINDOW"); ok {
		t.Error("the loader wrote the process environment")
	}
}

func TestOverrideOverFileAndReset(t *testing.T) {
	path := writeConfig(t, baseYAML)
	s := newStore(t, path)
	calls := 0
	s.OnChange(func() { calls++ })

	if err := s.SetOverride("gateway.rate_limit", "500"); err != nil {
		t.Fatal(err)
	}
	expect(t, s, "RATE_LIMIT", "500", SourceOverride)

	// a file reload keeps the override on top
	rewrite(t, path, strings.Replace(baseYAML, "rate_limit: 60", "rate_limit: 61", 1))
	if _, err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	expect(t, s, "RATE_LIMIT", "500", SourceOverride)

	s.DeleteOverride("gateway.rate_limit")
	expect(t, s, "RATE_LIMIT", "61", SourceFile)
	if calls != 3 {
		t.Errorf("OnChange ran %d times, want 3", calls)
	}
}

func TestOverrideRejections(t *testing.T) {
	s := newStore(t, writeConfig(t, baseYAML))
	cases := map[string]string{
		"gateway.rate_limit":          "0",   // below min
		"gateway.rate_limit_window":   "2h",  // above max
		"gateway.static.http_address": ":1",  // static
		"shared.kafka_brokers":        "x:1", // not editable
	}
	for path, value := range cases {
		if err := s.SetOverride(path, value); err == nil {
			t.Errorf("SetOverride(%s, %s) accepted", path, value)
		}
	}
	// another service's key is silently not ours
	if err := s.SetOverride("rating.rate_limit", "1"); err != nil {
		t.Errorf("foreign key: %v", err)
	}
	if len(s.Overrides()) != 0 {
		t.Errorf("overrides = %v", s.Overrides())
	}
}

func TestInvalidReloadKeepsLastGood(t *testing.T) {
	path := writeConfig(t, baseYAML)
	s := newStore(t, path)
	for _, bad := range []string{
		"gateway: [1, 2",                                           // syntax
		"gateway:\n  rate_limit: [1]\n",                            // non-scalar
		strings.Replace(baseYAML, "min: \"1\"", "min: \"100\"", 1), // default below its own min
	} {
		rewrite(t, path, bad)
		if _, err := s.Reload(); err == nil {
			t.Errorf("reload accepted %q", bad)
		}
		// the same broken content is reported once
		if _, err := s.Reload(); err != nil {
			t.Errorf("second reload of the same broken file: %v", err)
		}
		expect(t, s, "RATE_LIMIT", "60", SourceFile)
	}
}

func TestStaticKeysNeedRestart(t *testing.T) {
	path := writeConfig(t, baseYAML)
	s := newStore(t, path)
	next := strings.Replace(baseYAML, `http_address: ":8080"`, `http_address: ":9999"`, 1)
	next = strings.Replace(next, "export_rate_limit:", "export_rate_limit: 3\n  static:\n    session_ttl: 1h", 1)
	next = strings.Replace(next, "  static:\n    http_address", "    http_address", 1)
	rewrite(t, path, next)
	if _, err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	expect(t, s, "HTTP_ADDRESS", ":8080", SourceFile)
	expect(t, s, "SESSION_TTL", "", SourceDefault) // new static key waits for restart
	expect(t, s, "EXPORT_RATE_LIMIT", "3", SourceFile)
	if got := s.RestartPending(); len(got) != 1 || got[0] != "gateway.static.http_address" {
		t.Errorf("RestartPending = %v", got)
	}

	rewrite(t, path, baseYAML)
	if _, err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := s.RestartPending(); len(got) != 0 {
		t.Errorf("RestartPending after reverting = %v, want none", got)
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]string{
		"duplicate env in section": "gateway:\n  rate_limit: 1\n  static:\n    rate_limit: 2\n",
		"bad key":                  "gateway:\n  RateLimit: 1\n",
		"nested section":           "gateway:\n  deep:\n    x: 1\n",
		"editable missing key":     "gateway:\n  a: 1\nadmin_editable:\n  - {key: gateway.b, type: int, group: g}\n",
		"editable static key":      "gateway:\n  static:\n    a: 1\nadmin_editable:\n  - {key: gateway.static.a, type: int, group: g}\n",
		"editable bad type":        "gateway:\n  a: 1\nadmin_editable:\n  - {key: gateway.a, type: number, group: g}\n",
		"enum without values":      "gateway:\n  a: x\nadmin_editable:\n  - {key: gateway.a, type: enum, group: g}\n",
		"default not in enum":      "gateway:\n  a: x\nadmin_editable:\n  - {key: gateway.a, type: enum, values: [y], group: g}\n",
	}
	for name, content := range cases {
		if _, err := Parse([]byte(content)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestSharedAndServiceSectionConflict(t *testing.T) {
	path := writeConfig(t, "shared:\n  rate_limit: 1\ngateway:\n  rate_limit: 2\n")
	if _, err := New(path, "gateway"); err == nil {
		t.Error("the same env name in shared and the service section must be rejected")
	}
	if _, err := New(path, "rating"); err != nil {
		t.Errorf("a service without the conflict: %v", err)
	}
}

func TestMissingFileFallsBackToEnvAndDefaults(t *testing.T) {
	t.Setenv("RATE_LIMIT", "3")
	s, err := New(filepath.Join(t.TempDir(), "absent.yaml"), "gateway")
	if err != nil {
		t.Fatal(err)
	}
	expect(t, s, "RATE_LIMIT", "3", SourceEnv)
	expect(t, s, "HTTP_ADDRESS", "", SourceDefault)
}

func TestPackageFunctionsBeforeInit(t *testing.T) {
	if Default() != nil {
		t.Skip("a store is installed")
	}
	t.Setenv("SOME_KEY", "live")
	if Getenv("SOME_KEY") != "live" {
		t.Error("Getenv before Init must read the live environment")
	}
}

// The real config/app.yaml parses, and every service's view is free of env-name conflicts.
func TestRepoConfig(t *testing.T) {
	path := filepath.Join("..", "..", "config", "app.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skip("no config/app.yaml")
	}
	f, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, svc := range Services {
		if _, err := f.view(append([]string{SharedSection}, svc...)); err != nil {
			t.Errorf("%v: %v", svc, err)
		}
	}
	for path := range f.Settings {
		section, _, _ := strings.Cut(path, ".")
		known := section == SharedSection
		for _, svc := range Services {
			for _, s := range svc {
				known = known || s == section
			}
		}
		if !known {
			t.Errorf("%s: section %s is read by no service", path, section)
		}
	}
}
