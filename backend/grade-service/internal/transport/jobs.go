package transport

import (
	"sync"
	"time"

	"repo-therapist/grade-service/internal/app"
	"repo-therapist/grade-service/internal/contracts"
)

type job struct {
	token      string
	visibility string
	createdAt  time.Time
}

// jobStore holds per-request state between grading.requests and repo.ready, keyed by
// "org/repo" (the sourcecraft_uuid isn't known until repo.ready). The token lives only here,
// never on repo.ready.
type jobStore struct {
	mu   sync.Mutex
	jobs map[string]job
	now  func() time.Time
	ttl  time.Duration // how long a user token can sit in memory if repo.ready never arrives
}

func newJobStore() *jobStore {
	return &jobStore{jobs: map[string]job{}, now: time.Now, ttl: app.JobTTL()}
}

func (s *jobStore) put(key, token, visibility string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, j := range s.jobs {
		if now.Sub(j.createdAt) > s.ttl {
			delete(s.jobs, k)
		}
	}
	s.jobs[key] = job{token: token, visibility: visibility, createdAt: now}
}

// take returns and removes the job for key. An unknown or expired job comes back as a public
// job with no token, so repo.ready without a matching request is still graded, publicly.
func (s *jobStore) take(key string) job {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[key]
	delete(s.jobs, key)
	if !ok || s.now().Sub(j.createdAt) > s.ttl {
		return job{visibility: contracts.VisibilityPublic}
	}
	return j
}

// gradingVisibility decides how a request is graded: private when it carries a user token,
// public otherwise. ok is false for a non-public repo requested without a token — it can't be
// graded at all, and the request is dropped before cloning.
func gradingVisibility(req contracts.GradingRequestedEvent) (visibility string, ok bool) {
	if req.Token != "" {
		return contracts.VisibilityPrivate, true
	}
	if req.Visibility != "" && req.Visibility != contracts.VisibilityPublic {
		return "", false
	}
	return contracts.VisibilityPublic, true
}
