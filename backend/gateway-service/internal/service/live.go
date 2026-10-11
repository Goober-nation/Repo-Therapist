package service

import "time"

// SetTimeouts changes the RPC timeouts at runtime (config reloads, admin overrides).
func (s *GatewayService) SetTimeouts(aggregator, rating, yandex time.Duration) {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	s.aggregatorTimeout = aggregator
	s.ratingTimeout = rating
	s.yandexTimeout = yandex
}

func (s *GatewayService) aggregatorRPCTimeout() time.Duration {
	s.liveMu.RLock()
	defer s.liveMu.RUnlock()
	return s.aggregatorTimeout
}

func (s *GatewayService) ratingRPCTimeout() time.Duration {
	s.liveMu.RLock()
	defer s.liveMu.RUnlock()
	return s.ratingTimeout
}

func (s *GatewayService) yandexRPCTimeout() time.Duration {
	s.liveMu.RLock()
	defer s.liveMu.RUnlock()
	return s.yandexTimeout
}
