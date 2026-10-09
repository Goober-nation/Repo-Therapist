package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAdminConfigParsesList(t *testing.T) {
	t.Setenv("ADMIN_YANDEX_IDS", " 1001 , ,2002,3003,")
	t.Setenv("AUDIT_HASH_SECRET", "s3cret")

	admin := LoadConfig().Admin

	if want := []string{"1001", "2002", "3003"}; !reflect.DeepEqual(admin.YandexIDs, want) {
		t.Errorf("ids = %q, want %q", admin.YandexIDs, want)
	}

	if admin.AuditHashSecret != "s3cret" {
		t.Errorf("secret = %q", admin.AuditHashSecret)
	}
}

func TestAdminConfigDefaultsToNoAdmins(t *testing.T) {
	t.Setenv("ADMIN_YANDEX_IDS", "")
	t.Setenv("AUDIT_HASH_SECRET", "")

	admin := LoadConfig().Admin

	if len(admin.YandexIDs) != 0 || admin.AuditHashSecret != "" {
		t.Errorf("unset admin config must mean no admins and no secret, got %+v", admin)
	}

	if admin.DeniedLog.Limit != 5 || admin.DeniedLog.Window != 10*time.Minute {
		t.Errorf("denied-log budget = %+v, want 5 per 10m", admin.DeniedLog)
	}
}

func TestAdminUnlockConfig(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "")
	t.Setenv("ADMIN_UNLOCK_MAX_ATTEMPTS", "")
	t.Setenv("ADMIN_UNLOCK_LOCK", "")

	admin := LoadConfig().Admin
	if admin.Password != "" || admin.UnlockMaxAttempts != 5 || admin.UnlockLock != 15*time.Minute {
		t.Errorf("defaults = %+v, want no password, 5 tries, 15m", admin)
	}

	t.Setenv("ADMIN_PASSWORD", "  keep spaces  ")
	t.Setenv("ADMIN_UNLOCK_MAX_ATTEMPTS", "3")
	t.Setenv("ADMIN_UNLOCK_LOCK", "1h")

	admin = LoadConfig().Admin
	if admin.Password != "  keep spaces  " || admin.UnlockMaxAttempts != 3 || admin.UnlockLock != time.Hour {
		t.Errorf("override = %+v (the password must be taken exactly as written)", admin)
	}
}

func TestHashSecretIsOptional(t *testing.T) {
	explicit := AdminConfig{AuditHashSecret: "from-env", Password: "pw", YandexIDs: []string{"1"}}
	if explicit.HashSecret() != "from-env" {
		t.Errorf("an explicit AUDIT_HASH_SECRET must win, got %q", explicit.HashSecret())
	}

	if (AdminConfig{}).HashSecret() != "" {
		t.Error("with nothing configured there is nothing to derive from: the admin routes stay off")
	}

	onlyPassword := AdminConfig{Password: "pw"}
	derived := onlyPassword.HashSecret()

	if derived == "" || derived != onlyPassword.HashSecret() {
		t.Errorf("a password alone must give a stable derived key, got %q", derived)
	}

	if strings.Contains(derived, "pw") {
		t.Error("the derived key must not contain the password")
	}

	if (AdminConfig{Password: "other"}).HashSecret() == derived {
		t.Error("a different password must give a different key")
	}

	if (AdminConfig{YandexIDs: []string{"1"}}).HashSecret() == "" {
		t.Error("an admin list alone must be enough too")
	}

	if (AdminConfig{Password: "pw", YandexIDs: []string{"1"}}).HashSecret() == derived {
		t.Error("the list is part of the derivation")
	}
}

func TestUserAuditWindow(t *testing.T) {
	t.Setenv("USER_AUDIT_WINDOW", "")

	if got := LoadConfig().Admin.UserAuditWindow; got != 5*time.Minute {
		t.Errorf("default window = %s, want 5m", got)
	}

	t.Setenv("USER_AUDIT_WINDOW", "90s")

	if got := LoadConfig().Admin.UserAuditWindow; got != 90*time.Second {
		t.Errorf("window = %s, want 90s", got)
	}

	t.Setenv("USER_AUDIT_WINDOW", "0")

	if got := LoadConfig().Admin.UserAuditWindow; got != 0 {
		t.Errorf("window = %s, want 0 (the audit of user actions off)", got)
	}
}

func TestDevAuthBypassConfig(t *testing.T) {
	t.Setenv("DEV_AUTH_BYPASS_USER_ID", "")
	if got := LoadConfig().Yandex.DevBypassUserID; got != "" {
		t.Errorf("default = %q, want off", got)
	}

	t.Setenv("DEV_AUTH_BYPASS_USER_ID", " debug-user ")
	if got := LoadConfig().Yandex.DevBypassUserID; got != "debug-user" {
		t.Errorf("bypass = %q, want debug-user", got)
	}
}
