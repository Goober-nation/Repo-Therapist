package transport

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"repo-therapist/grade-service/internal/contracts"
	kafkaclient "repo-therapist/grade-service/internal/kafka"
)

// docsWaiters hands repo.docs.graded replies to the repo.ready handler waiting on that job_id,
// so overlapping runs of one repo never take each other's reply. A reply nobody is waiting for (late, after the timeout; or a duplicate —
// the LLM service delivers at least once) is dropped.
type docsWaiters struct {
	mu      sync.Mutex
	waiting map[string]chan contracts.DocsGraded
}

func newDocsWaiters() *docsWaiters {
	return &docsWaiters{waiting: map[string]chan contracts.DocsGraded{}}
}

func (w *docsWaiters) register(jobID string) chan contracts.DocsGraded {
	ch := make(chan contracts.DocsGraded, 1)
	w.mu.Lock()
	w.waiting[jobID] = ch
	w.mu.Unlock()
	return ch
}

func (w *docsWaiters) unregister(jobID string) {
	w.mu.Lock()
	delete(w.waiting, jobID)
	w.mu.Unlock()
}

// deliver reports whether a waiter took the reply.
func (w *docsWaiters) deliver(reply contracts.DocsGraded) bool {
	w.mu.Lock()
	ch, ok := w.waiting[reply.JobID]
	delete(w.waiting, reply.JobID)
	w.mu.Unlock()
	if ok {
		ch <- reply // buffered, never blocks
	}
	return ok
}

// wait returns the reply, or nil once deadline passes or ctx ends. Always unregisters.
func (w *docsWaiters) wait(ctx context.Context, jobID string, ch chan contracts.DocsGraded, deadline time.Time) *contracts.DocsGraded {
	defer w.unregister(jobID)
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case reply := <-ch:
		return &reply
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return nil
	}
}

// runDocsGradedConsumer feeds repo.docs.graded replies to docsWaiters.
func runDocsGradedConsumer(ctx context.Context, brokers []string, waiters *docsWaiters) error {
	consumer := kafkaclient.NewConsumer(brokers, contracts.TopicDocsGraded, "grade-service")
	defer consumer.Close()

	log.Printf("grade-service listening on %s", contracts.TopicDocsGraded)
	return consumer.Run(ctx, func(ctx context.Context, value []byte) error {
		var reply contracts.DocsGraded
		if err := json.Unmarshal(value, &reply); err != nil {
			log.Printf("skipping unparseable %s message: %v", contracts.TopicDocsGraded, err)
			return nil
		}
		if !waiters.deliver(reply) {
			log.Printf("dropping %s reply for %s (job %s): no grading is waiting for it (late or duplicate)", contracts.TopicDocsGraded, reply.SourcecraftUUID, reply.JobID)
		}
		return nil
	})
}
