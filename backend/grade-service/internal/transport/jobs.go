package transport

import (
	"sync"
	"time"
)

// jobTTL bounds how long a user token can sit in memory if repo.ready never arrives.
const jobTTL = 30 * time.Minute

type job struct {
	token     string
	createdAt time.Time
}

// jobStore holds per-request state between grading.requests and repo.ready, keyed by
// "org/repo" (the sourcecraft_uuid isn't known until repo.ready). The token lives only here,
// never on repo.ready.
type jobStore struct {
	mu   sync.Mutex
	jobs map[string]job
	now  func() time.Time
}

func newJobStore() *jobStore {
	return &jobStore{jobs: map[string]job{}, now: time.Now}
}

func (s *jobStore) put(key, token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, j := range s.jobs {
		if now.Sub(j.createdAt) > jobTTL {
			delete(s.jobs, k)
		}
	}
	s.jobs[key] = job{token: token, createdAt: now}
}

// take returns and removes the job for key. ok is false for unknown or expired jobs.
func (s *jobStore) take(key string) (job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[key]
	delete(s.jobs, key)
	if !ok || s.now().Sub(j.createdAt) > jobTTL {
		return job{}, false
	}
	return j, true
}
