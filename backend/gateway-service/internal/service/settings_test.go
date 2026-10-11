package service

import "testing"

func TestPublicConfig(t *testing.T) {
	t.Setenv("VITE_PUBLIC_URL", " https://health.example ")
	t.Setenv("VITE_YANDEX_CLIENT_ID", "client")
	t.Setenv("VITE_DEV_AUTH_BYPASS", "true")
	t.Setenv("POLL_TIMEOUT_MS", "nope")

	s := &GatewayService{}
	c := s.PublicConfig()
	if c.PublicURL != "https://health.example" || c.YandexClientID != "client" || c.PollTimeoutMS != DefaultPollTimeoutMS {
		t.Errorf("config %+v", c)
	}
	if c.DevAuthBypass {
		t.Error("the dev button must stay off while the Gateway does not run the bypass")
	}

	s.SetDevAuthBypass(true)
	t.Setenv("POLL_TIMEOUT_MS", "1200000")
	if c := s.PublicConfig(); !c.DevAuthBypass || c.PollTimeoutMS != 1200000 {
		t.Errorf("config %+v", c)
	}
}
