package transport

import (
	"testing"
	"time"
)

func TestJobStoreTakeRemoves(t *testing.T) {
	s := newJobStore()
	s.put("org/repo", "tok")

	j, ok := s.take("org/repo")
	if !ok || j.token != "tok" {
		t.Fatalf("take = %+v, %v; want token tok", j, ok)
	}
	if _, ok := s.take("org/repo"); ok {
		t.Error("second take should find nothing")
	}
}

func TestJobStoreExpiry(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	s := newJobStore()
	s.now = func() time.Time { return now }
	s.put("old/repo", "tok")

	now = now.Add(jobTTL + time.Minute)
	if _, ok := s.take("old/repo"); ok {
		t.Error("expired job should not be returned")
	}

	s.put("a/b", "x")
	now = now.Add(jobTTL + time.Minute)
	s.put("c/d", "y")
	if _, exists := s.jobs["a/b"]; exists {
		t.Error("put should sweep expired jobs")
	}
}
