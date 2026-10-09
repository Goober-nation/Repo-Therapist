package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"gateway-service/internal/domain"
)

func TestDevBypassClientAcceptsAnyToken(t *testing.T) {
	client := NewDevBypassClient("debug-user")

	user, err := client.GetUser(context.Background(), "anything")
	if err != nil || user.ID != "debug-user" {
		t.Errorf("user = %+v err = %v, want debug-user", user, err)
	}

	if _, err := client.GetUser(context.Background(), "  "); !errors.Is(err, domain.ErrIdentityInvalid) {
		t.Errorf("empty token err = %v, want ErrIdentityInvalid", err)
	}
}

func TestSessionCookieSecureUnlessInsecureCookies(t *testing.T) {
	for _, insecure := range []bool{false, true} {
		handler := NewHandler(&fakeService{})
		handler.SetInsecureCookies(insecure)
		mux := stdhttp.NewServeMux()
		handler.Register(mux)

		req := httptest.NewRequest("POST", "/auth/yandex", nil)
		req.Header.Set("OAuth", "token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		cookies := rec.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name != sessionCookieName {
			t.Fatalf("insecure=%v cookies = %+v", insecure, cookies)
		}
		if cookies[0].Secure == insecure || !cookies[0].HttpOnly {
			t.Errorf("insecure=%v cookie = %+v", insecure, cookies[0])
		}
	}
}
