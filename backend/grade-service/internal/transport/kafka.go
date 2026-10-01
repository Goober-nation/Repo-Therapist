// Package transport wires grade-service's Kafka topics to the app package's grading logic.
package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"os/signal"
	"syscall"
	"time"

	"os"

	"repo-therapist/grade-service/internal/app"
	"repo-therapist/grade-service/internal/config"
	"repo-therapist/grade-service/internal/contracts"
	kafkaclient "repo-therapist/grade-service/internal/kafka"
	"repo-therapist/grade-service/internal/scoring"
)

// RunListen runs two consumers concurrently for as long as the process lives:
//   - grading.requests (the Aggregator's trigger) -> relayed as repo.grade.requested, our own
//     internal trigger to Repository service; the job's token is kept in memory for repo.ready.
//   - repo.ready (Repository service's handoff) -> graded, and the result published to
//     grading.results for the Aggregator to consume.
//
// Repository service is untouched by any of this — it only ever sees repo.grade.requested/
// repo.ready, exactly as before. See agents_memory/topics/aggregator-integration.md.
func RunListen(configPath string) error {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	catalog, err := config.LoadRecommendations(app.EnvOrDefault("RECOMMENDATIONS_PATH", "config/recommendations.yaml"))
	if err != nil {
		return fmt.Errorf("loading recommendations catalog: %w", err)
	}

	brokers := []string{app.EnvOrDefault("KAFKA_BROKERS", "localhost:9092")}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	provisionCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = kafkaclient.EnsureTopics(provisionCtx, brokers, contracts.AllTopics, 1)
	cancel()
	if err != nil {
		return fmt.Errorf("provisioning topics: %w", err)
	}

	// Two independent consumer loops, one goroutine each. If either returns (error or shutdown),
	// stop() cancels the shared ctx so the other one exits too — Consumer.Run treats a cancelled
	// ctx as a clean stop (returns nil), not an error, so this doesn't mask a real first error.
	jobs := newJobStore()
	docs := newDocsSettings(cfg)
	errCh := make(chan error, 3)
	go func() { errCh <- runGradingRequestsRelay(ctx, brokers, jobs) }()
	go func() { errCh <- runRepoReadyGrader(ctx, brokers, cfg, catalog, jobs, docs) }()
	go func() { errCh <- runDocsGradedConsumer(ctx, brokers, docs.waiters) }()

	firstErr := <-errCh
	stop()
	<-errCh
	<-errCh
	return firstErr
}

// runGradingRequestsRelay consumes the Aggregator's grading.requests, remembers the job's token
// for when repo.ready arrives, and republishes it as our internal repo.grade.requested.
func runGradingRequestsRelay(ctx context.Context, brokers []string, jobs *jobStore) error {
	consumer := kafkaclient.NewConsumer(brokers, contracts.TopicGradingRequests, "grade-service")
	defer consumer.Close()
	producer := kafkaclient.NewProducer(brokers, contracts.TopicGradeRequested)
	defer producer.Close()

	log.Printf("grade-service listening on %s, relaying to %s", contracts.TopicGradingRequests, contracts.TopicGradeRequested)
	return consumer.Run(ctx, func(ctx context.Context, value []byte) error {
		var req contracts.GradingRequestedEvent
		if err := json.Unmarshal(value, &req); err != nil {
			log.Printf("skipping unparseable %s message: %v", contracts.TopicGradingRequests, err)
			return nil
		}

		key := req.OrgSlug + "/" + req.RepoSlug
		visibility, ok := gradingVisibility(req)
		if !ok {
			log.Printf("dropping grading request for %s/%s: visibility=%q needs a user token", req.OrgSlug, req.RepoSlug, req.Visibility)
			return nil
		}
		jobs.put(key, req.Token, visibility)
		out := contracts.GradeRequested{OrgSlug: req.OrgSlug, RepoSlug: req.RepoSlug, RequestedAt: req.RequestedAt, Token: req.Token}
		if err := producer.Publish(ctx, key, out); err != nil {
			return fmt.Errorf("relaying to %s: %w", contracts.TopicGradeRequested, err)
		}
		log.Printf("relayed %s grading request for %s/%s to %s", visibility, req.OrgSlug, req.RepoSlug, contracts.TopicGradeRequested)
		return nil
	})
}

// runRepoReadyGrader consumes repo.ready, grades each ready repo, and publishes the result to
// grading.results for the Aggregator. Failed/skipped repos are logged, not silently dropped, but
// don't kill the loop — one repo's failure shouldn't stop grading for every other repo.
func runRepoReadyGrader(ctx context.Context, brokers []string, cfg *config.Config, catalog map[string]string, jobs *jobStore, docs docsSettings) error {
	consumer := kafkaclient.NewConsumer(brokers, contracts.TopicRepoReady, "grade-service")
	defer consumer.Close()
	producer := kafkaclient.NewProducer(brokers, contracts.TopicGradingResults)
	defer producer.Close()
	docsProducer := kafkaclient.NewProducer(brokers, contracts.TopicDocsGradeRequested)
	defer docsProducer.Close()

	cleanup := repoDataCleanup(app.KeepRepoData(), app.DeleteRepoData)

	log.Printf("grade-service listening on %s, publishing results to %s", contracts.TopicRepoReady, contracts.TopicGradingResults)
	return consumer.Run(ctx, withRepoDataCleanup(cleanup, func(ctx context.Context, value []byte) error {
		var ready contracts.RepoReady
		if err := json.Unmarshal(value, &ready); err != nil {
			log.Printf("skipping unparseable %s message: %v", contracts.TopicRepoReady, err)
			return nil
		}

		// Taken before the status check so a failed job's token is dropped too. A missing job
		// (unknown/expired, or relayed by another instance) is graded publicly with the service PAT.
		job := jobs.take(ready.OrgSlug + "/" + ready.RepoSlug)

		if ready.Status != contracts.StatusReady {
			log.Printf("not grading %s/%s: status=%s error=%q", ready.OrgSlug, ready.RepoSlug, ready.Status, ready.Error)
			return publishFailure(ctx, producer, ready, job.visibility, contracts.GradingErrRepoFailed)
		}

		log.Printf("grading %s/%s %s (sourcecraft_uuid=%s)", ready.OrgSlug, ready.RepoSlug, job.visibility, ready.SourcecraftUUID)
		// Documentation is asked for first so the LLM works while the other categories are graded.
		docsCh, docsDeadline := requestDocs(ctx, docsProducer, docs, ready)
		gradeCtx, cancel := context.WithTimeout(ctx, app.GradeTimeout())
		out, err := app.Grade(gradeCtx, ready.SourcecraftUUID, cfg, job.token, job.visibility)
		cancel()
		var reply *contracts.DocsGraded
		if docsCh != nil {
			reply = docs.waiters.wait(ctx, ready.SourcecraftUUID, docsCh, docsDeadline)
			if reply == nil {
				log.Printf("no %s reply for %s/%s within %s: Documentation will be no-data", contracts.TopicDocsGraded, ready.OrgSlug, ready.RepoSlug, docs.timeout)
			} else if reply.Error != "" {
				log.Printf("LLM documentation evaluation for %s/%s failed with %s: Documentation will be no-data", ready.OrgSlug, ready.RepoSlug, reply.Error)
			}
		}
		if err != nil {
			log.Printf("grading %s/%s failed: %v", ready.OrgSlug, ready.RepoSlug, err)
			return publishFailure(ctx, producer, ready, job.visibility, gradingErrCode(ctx))
		}
		out.Categories["documentation"] = scoring.ComputeDocumentation(reply)
		if docs := out.Categories["documentation"]; !docs.NoData {
			out.DocsAdvice = reply.Advice
		}
		app.PrintResult(out)

		result, err := app.BuildGradingResult(out, cfg, catalog, job.visibility, time.Now())
		if err != nil {
			log.Printf("building grading result for %s/%s failed: %v", ready.OrgSlug, ready.RepoSlug, err)
			return publishFailure(ctx, producer, ready, job.visibility, contracts.GradingErrBuildResult)
		}
		if err := producer.Publish(ctx, ready.SourcecraftUUID, result); err != nil {
			return fmt.Errorf("publishing %s: %w", contracts.TopicGradingResults, err)
		}
		log.Printf("published grading result for %s/%s to %s", ready.OrgSlug, ready.RepoSlug, contracts.TopicGradingResults)
		return nil
	}))
}

// repoDataCleanupTimeout bounds one repo's S3 export deletion.
const repoDataCleanupTimeout = time.Minute

// repoDataCleanup returns the function that deletes a graded repo's S3 export, or nil when
// S3_KEEP_REPO_DATA keeps it. A failed delete is only logged: the grade is already published,
// and Repository service clears the prefix again before its next upload.
func repoDataCleanup(keep bool, deleteRepoData func(ctx context.Context, repoID string) (int, error)) func(repoID string) {
	if keep {
		log.Printf("S3_KEEP_REPO_DATA is set: repo exports stay in S3 after grading")
		return nil
	}
	return func(repoID string) {
		ctx, cancel := context.WithTimeout(context.Background(), repoDataCleanupTimeout)
		defer cancel()
		n, err := deleteRepoData(ctx, repoID)
		if err != nil {
			log.Printf("deleting S3 export for %s failed: %v", repoID, err)
			return
		}
		log.Printf("deleted %d S3 objects for %s", n, repoID)
	}
}

// withRepoDataCleanup runs cleanup on the repo.ready message's repo once handler has published
// its result or failure (handler returned nil). Every reader of the export is done by then: Grade
// read its inputs, and the LLM service replied or timed out. A handler error stops the consumer
// without committing, so the message is redelivered and its export must stay.
func withRepoDataCleanup(cleanup func(repoID string), handler func(ctx context.Context, value []byte) error) func(ctx context.Context, value []byte) error {
	if cleanup == nil {
		return handler
	}
	return func(ctx context.Context, value []byte) error {
		if err := handler(ctx, value); err != nil {
			return err
		}
		var ready contracts.RepoReady
		if json.Unmarshal(value, &ready) == nil && ready.SourcecraftUUID != "" {
			cleanup(ready.SourcecraftUUID)
		}
		return nil
	}
}

// publishFailure reports a repo that couldn't be graded, so the Aggregator releases its lock. A
// failure without a sourcecraft_uuid can't be correlated to a lock and is only logged.
func publishFailure(ctx context.Context, producer *kafkaclient.Producer, ready contracts.RepoReady, visibility, code string) error {
	if ready.SourcecraftUUID == "" {
		log.Printf("no sourcecraft_uuid for %s/%s: cannot publish %s failure", ready.OrgSlug, ready.RepoSlug, code)
		return nil
	}
	// On shutdown ctx is already cancelled; the failure must still reach the Aggregator so it
	// releases the repo lock instead of leaving the repo busy.
	publishCtx, cancel := detachedOnShutdown(ctx)
	defer cancel()
	if err := producer.Publish(publishCtx, ready.SourcecraftUUID, app.GradingFailure(ready.SourcecraftUUID, visibility, code, time.Now())); err != nil {
		return fmt.Errorf("publishing %s failure: %w", contracts.TopicGradingResults, err)
	}
	log.Printf("published %s failure for %s/%s to %s", code, ready.OrgSlug, ready.RepoSlug, contracts.TopicGradingResults)
	return nil
}

// gradingErrCode tells an interrupted grading (shutdown) apart from one that failed on its own.
func gradingErrCode(ctx context.Context) string {
	if ctx.Err() != nil {
		return contracts.GradingErrInterrupted
	}
	return contracts.GradingErrGrading
}

// shutdownPublishTimeout bounds the last publish made after shutdown was requested.
const shutdownPublishTimeout = 5 * time.Second

// detachedOnShutdown returns ctx unchanged while it is live. Once it is cancelled (SIGTERM), it
// returns a short context that ignores the cancellation, so a final result can still be sent.
func detachedOnShutdown(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx.Err() == nil {
		return ctx, func() {}
	}
	return context.WithTimeout(context.WithoutCancel(ctx), shutdownPublishTimeout)
}

// docsSettings controls the Documentation round trip to the LLM service. enabled is false when
// the documentation category is not_calculated or LLM_ENABLED=false; Documentation is then
// no-data without asking.
type docsSettings struct {
	enabled bool
	timeout time.Duration
	waiters *docsWaiters
}

// defaultLLMTimeout bounds how long a grading waits for repo.docs.graded; the LLM service has no
// failure message, so a refused or crashed request is only ever noticed by this timeout.
const defaultLLMTimeout = 70 * time.Second

func newDocsSettings(cfg *config.Config) docsSettings {
	docCfg, ok := cfg.Categories["documentation"]
	enabled := ok && docCfg.Status == config.StatusCalculated && os.Getenv("LLM_ENABLED") != "false"
	timeout := app.DurationEnv("LLM_TIMEOUT", defaultLLMTimeout)
	if enabled && timeout >= app.GradeTimeout() {
		log.Printf("LLM_TIMEOUT (%s) is not below GRADE_TIMEOUT (%s): the grade limit may expire before the Documentation reply", timeout, app.GradeTimeout())
	}
	return docsSettings{enabled: enabled, timeout: timeout, waiters: newDocsWaiters()}
}

// requestDocs publishes repo.docs.grade.requested and returns the channel its reply arrives on
// plus the wait deadline, or a nil channel when docs grading is off or the publish failed.
func requestDocs(ctx context.Context, producer *kafkaclient.Producer, docs docsSettings, ready contracts.RepoReady) (chan contracts.DocsGraded, time.Time) {
	if !docs.enabled {
		return nil, time.Time{}
	}
	ch := docs.waiters.register(ready.SourcecraftUUID)
	req := contracts.DocsGradeRequested{SourcecraftUUID: ready.SourcecraftUUID, S3Prefix: ready.S3Prefix, RequestedAt: time.Now()}
	if err := producer.Publish(ctx, ready.SourcecraftUUID, req); err != nil {
		docs.waiters.unregister(ready.SourcecraftUUID)
		log.Printf("publishing %s for %s/%s failed: %v (Documentation will be no-data)", contracts.TopicDocsGradeRequested, ready.OrgSlug, ready.RepoSlug, err)
		return nil, time.Time{}
	}
	return ch, time.Now().Add(docs.timeout)
}
