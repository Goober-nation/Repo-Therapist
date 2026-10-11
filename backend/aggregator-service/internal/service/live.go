package service

import "time"

// The fields below change at runtime (config/app.yaml reloads, admin overrides): app.go writes them
// through UpdateLive and the service reads them through these accessors, both under liveMu.

// UpdateLive runs fn with the runtime-tunable fields locked for writing.
func (as *AggregatorService) UpdateLive(fn func(*AggregatorService)) {
	as.liveMu.Lock()
	defer as.liveMu.Unlock()
	fn(as)
}

func (as *AggregatorService) rpcTimeout() time.Duration {
	as.liveMu.RLock()
	defer as.liveMu.RUnlock()
	return as.RPCTimeout
}

func (as *AggregatorService) regradeMissingAnalytics() bool {
	as.liveMu.RLock()
	defer as.liveMu.RUnlock()
	return as.RegradeMissingAnalytics
}

func (as *AggregatorService) tokenTTL() time.Duration {
	as.liveMu.RLock()
	defer as.liveMu.RUnlock()
	return as.TokenTTL
}

func (as *AggregatorService) regradeLimit() int {
	as.liveMu.RLock()
	defer as.liveMu.RUnlock()
	return as.RegradeLimit
}

func (as *AggregatorService) llmRequestTTL() time.Duration {
	as.liveMu.RLock()
	defer as.liveMu.RUnlock()
	return as.LLMRequestTTL
}

func (as *AggregatorService) liveCatalogMaxPages() int {
	as.liveMu.RLock()
	defer as.liveMu.RUnlock()
	return as.CatalogMaxPages
}

func (as *AggregatorService) liveBatchItemTimeout() time.Duration {
	as.liveMu.RLock()
	defer as.liveMu.RUnlock()
	return as.BatchItemTimeout
}

func (as *AggregatorService) batchMaxLimit() int {
	as.liveMu.RLock()
	defer as.liveMu.RUnlock()
	return as.BatchMaxLimit
}

func (as *AggregatorService) auditRetentionDays() int {
	as.liveMu.RLock()
	defer as.liveMu.RUnlock()
	return as.AuditRetentionDays
}

// SetRegradeInterval changes the public regrade period of a running schedule; the next run moves
// to one new interval from now. It is a no-op when the schedule is off: turning it on needs a
// restart (GRADING_REGRADE_INTERVAL > 0 and SERVICE_TOKEN at startup).
func (as *AggregatorService) SetRegradeInterval(interval time.Duration) {
	if interval <= 0 {
		return
	}
	as.regradeMu.Lock()
	defer as.regradeMu.Unlock()
	if !as.regradeState.enabled || as.ticker == nil || as.regradeState.interval == interval {
		return
	}
	as.regradeState.interval = interval
	as.regradeState.nextRunAt = as.currentTime().Add(interval)
	as.ticker.Reset(interval)
}
