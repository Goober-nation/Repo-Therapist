package transport

import (
	"sync"
	"time"

	"repo-therapist/grade-service/internal/app"
	"repo-therapist/grade-service/internal/contracts"
)

type job struct {
	visibility string
	createdAt  time.Time
}

// jobStore holds each grading job's mode between grading.requests and repo.ready, keyed by the
// Aggregator's job_id, so overlapping runs of one repo never share state. The user token is not
// kept here: it stays in Redis (tokenstore) until the Security call.
type jobStore struct {
	mu   sync.Mutex
	jobs map[string]job
	now  func() time.Time
	// ttl is how long a job is remembered if repo.ready never arrives; read per call so a
	// GRADE_JOB_TTL change in config/app.yaml or the admin panel applies without a restart.
	ttl func() time.Duration
}

func newJobStore() *jobStore {
	return &jobStore{jobs: map[string]job{}, now: time.Now, ttl: app.JobTTL}
}

func (s *jobStore) put(jobID, visibility string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, j := range s.jobs {
		if now.Sub(j.createdAt) > s.ttl() {
			delete(s.jobs, k)
		}
	}
	s.jobs[jobID] = job{visibility: visibility, createdAt: now}
}

// take returns and removes jobID's job; ok is false for an unknown or expired job.
func (s *jobStore) take(jobID string) (j job, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok = s.jobs[jobID]
	delete(s.jobs, jobID)
	if !ok || s.now().Sub(j.createdAt) > s.ttl() {
		return job{}, false
	}
	return j, true
}

// gradingVisibility decides how a request is graded: private when a user token is stored for its
// job, public otherwise. ok is false for a non-public repo requested without a token — it can't
// be graded at all, and the request is dropped before cloning.
func gradingVisibility(req contracts.GradingRequestedEvent, hasToken bool) (visibility string, ok bool) {
	if hasToken {
		return contracts.VisibilityPrivate, true
	}
	if req.Visibility != "" && req.Visibility != contracts.VisibilityPublic {
		return "", false
	}
	return contracts.VisibilityPublic, true
}

// readyVisibility is the grading mode for a repo.ready message: the mode remembered at
// grading.requests when known; otherwise (restart, another instance relayed it) decided the same
// way, by whether a token is stored. A private job whose token is gone (expired) cannot be
// graded: ok is false.
func readyVisibility(j job, known bool, token string) (visibility string, ok bool) {
	if !known {
		if token != "" {
			return contracts.VisibilityPrivate, true
		}
		return contracts.VisibilityPublic, true
	}
	if j.visibility == contracts.VisibilityPrivate && token == "" {
		return contracts.VisibilityPrivate, false
	}
	return j.visibility, true
}
