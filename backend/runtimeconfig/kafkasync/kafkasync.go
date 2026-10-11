// Package kafkasync connects a runtimeconfig.Store to the admin overrides (docs/config.md).
//
// The Aggregator, the only writer of the settings_override table, publishes every override to the
// compacted topic config.updated: key = the app.yaml path, value = Update, and a tombstone (nil
// value) when an admin resets the key to its app.yaml value. Every service reads that topic from the
// first offset on startup, so it holds the current overrides before it starts work, then keeps
// applying changes. Each process also publishes its Status (which of its keys the environment sets,
// which static keys wait for a restart) to the compacted topic config.status, keyed by process name;
// the Aggregator uses it to refuse overrides the environment would shadow.
package kafkasync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"repo-therapist/runtimeconfig"
)

// Topic names; both are compacted, one partition.
const (
	TopicUpdates = "config.updated"
	TopicStatus  = "config.status"
)

// RehydrateTimeout bounds the startup read of config.updated. A service whose Kafka is down starts
// on app.yaml and the environment, logs it, and picks the overrides up once Kafka answers.
const RehydrateTimeout = 15 * time.Second

// Update is one override as it travels on config.updated.
type Update struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Status is what a process reports about its own configuration on config.status.
type Status struct {
	Service        string    `json:"service"`
	Sections       []string  `json:"sections"`
	EnvLocked      []string  `json:"env_locked"`
	RestartPending []string  `json:"restart_pending"`
	ReportedAt     time.Time `json:"reported_at"`
}

// EnsureTopics creates both topics with cleanup.policy=compact, and sets that policy on topics that
// already exist (e.g. auto-created by a broker with auto-create on), so the latest value per key is
// kept forever and older ones are dropped.
func EnsureTopics(ctx context.Context, brokers []string) error {
	if len(brokers) == 0 {
		return errors.New("no Kafka brokers")
	}
	client := &kafkago.Client{Addr: kafkago.TCP(brokers...), Timeout: 10 * time.Second}
	compact := []kafkago.ConfigEntry{{ConfigName: "cleanup.policy", ConfigValue: "compact"}}
	var topics []kafkago.TopicConfig
	for _, t := range []string{TopicUpdates, TopicStatus} {
		topics = append(topics, kafkago.TopicConfig{Topic: t, NumPartitions: 1, ReplicationFactor: 1, ConfigEntries: compact})
	}
	resp, err := client.CreateTopics(ctx, &kafkago.CreateTopicsRequest{Topics: topics})
	if err != nil {
		return fmt.Errorf("create config topics: %w", err)
	}
	for topic, terr := range resp.Errors {
		if terr != nil && !errors.Is(terr, kafkago.TopicAlreadyExists) {
			return fmt.Errorf("create %s: %w", topic, terr)
		}
	}
	var resources []kafkago.AlterConfigRequestResource
	for _, t := range []string{TopicUpdates, TopicStatus} {
		resources = append(resources, kafkago.AlterConfigRequestResource{
			ResourceType: kafkago.ResourceTypeTopic, ResourceName: t,
			Configs: []kafkago.AlterConfigRequestConfig{{Name: "cleanup.policy", Value: "compact"}},
		})
	}
	alter, err := client.AlterConfigs(ctx, &kafkago.AlterConfigsRequest{Resources: resources})
	if err != nil {
		return fmt.Errorf("set compaction on config topics: %w", err)
	}
	for res, aerr := range alter.Errors {
		if aerr != nil {
			return fmt.Errorf("set compaction on %s: %w", res.Name, aerr)
		}
	}
	return nil
}

// Sync keeps one store in step with config.updated and reports its status on config.status.
type Sync struct {
	store   *runtimeconfig.Store
	brokers []string
	service string

	status *kafkago.Writer

	mu   sync.Mutex
	seen map[string]string // keys currently on the topic (non-tombstoned) -> value
	// OnUpdate, when set before Start, sees every applied message (the Aggregator keeps its own copy).
	OnUpdate func(key, value string, deleted bool)
}

// New prepares a Sync for service (a process name such as "gateway" or "llm-summary").
func New(store *runtimeconfig.Store, brokers []string, service string) *Sync {
	return &Sync{
		store: store, brokers: brokers, service: service, seen: map[string]string{},
		status: &kafkago.Writer{Addr: kafkago.TCP(brokers...), Topic: TopicStatus, Balancer: &kafkago.Hash{}},
	}
}

// Start reads config.updated up to its current end (bounded by RehydrateTimeout), then follows it in
// the background until ctx ends, and publishes this process's status now and after every change.
func (s *Sync) Start(ctx context.Context) {
	setupCtx, cancel := context.WithTimeout(ctx, RehydrateTimeout)
	defer cancel()
	if err := EnsureTopics(setupCtx, s.brokers); err != nil {
		log.Printf("config: %v; admin overrides will apply once Kafka is reachable", err)
	}
	reader := s.newReader()
	first, end, err := s.offsets(setupCtx)
	if err != nil {
		log.Printf("config: reading %s offsets: %v; starting without admin overrides for now", TopicUpdates, err)
	} else {
		n := 0
		for first < end {
			msg, err := reader.ReadMessage(setupCtx)
			if err != nil {
				log.Printf("config: rehydrating admin overrides stopped after %d messages: %v", n, err)
				break
			}
			s.apply(msg)
			n++
			if msg.Offset+1 >= end {
				break
			}
		}
		log.Printf("config: %d admin override message(s) read from %s", n, TopicUpdates)
	}
	s.store.OnChange(func() { go s.publishStatus(context.Background()) })
	s.publishStatus(ctx)
	go func() {
		defer reader.Close()
		defer s.status.Close()
		for {
			msg, err := reader.ReadMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("config: reading %s: %v", TopicUpdates, err)
				time.Sleep(2 * time.Second)
				continue
			}
			s.apply(msg)
		}
	}()
}

func (s *Sync) newReader() *kafkago.Reader {
	r := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers: s.brokers, Topic: TopicUpdates, Partition: 0, MinBytes: 1, MaxBytes: 1 << 20,
		MaxWait: time.Second,
	})
	_ = r.SetOffset(kafkago.FirstOffset)
	return r
}

// offsets returns the first and the next (end) offset of config.updated.
func (s *Sync) offsets(ctx context.Context) (first, end int64, err error) {
	conn, err := kafkago.DialLeader(ctx, "tcp", s.brokers[0], TopicUpdates, 0)
	if err != nil {
		return 0, 0, err
	}
	defer conn.Close()
	return conn.ReadOffsets()
}

func (s *Sync) apply(msg kafkago.Message) {
	key := string(msg.Key)
	if key == "" {
		return
	}
	if msg.Value == nil {
		s.mu.Lock()
		delete(s.seen, key)
		s.mu.Unlock()
		s.store.DeleteOverride(key)
		if s.OnUpdate != nil {
			s.OnUpdate(key, "", true)
		}
		return
	}
	var u Update
	if err := json.Unmarshal(msg.Value, &u); err != nil || u.Key != key {
		log.Printf("config: skipping malformed %s message for %q", TopicUpdates, key)
		return
	}
	s.mu.Lock()
	s.seen[key] = u.Value
	s.mu.Unlock()
	if err := s.store.SetOverride(key, u.Value); err != nil {
		log.Printf("config: override of %s not applied: %v", key, err)
	}
	if s.OnUpdate != nil {
		s.OnUpdate(key, u.Value, false)
	}
}

// Seen returns the overrides currently on the topic as this process read them.
func (s *Sync) Seen() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.seen))
	for k, v := range s.seen {
		out[k] = v
	}
	return out
}

func (s *Sync) publishStatus(ctx context.Context) {
	st := Status{
		Service:        s.service,
		Sections:       s.store.Sections(),
		EnvLocked:      nonNil(s.store.EnvLockedPaths()),
		RestartPending: nonNil(s.store.RestartPending()),
		ReportedAt:     time.Now().UTC(),
	}
	data, _ := json.Marshal(st)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.status.WriteMessages(ctx, kafkago.Message{Key: []byte(s.service), Value: data}); err != nil {
		log.Printf("config: publishing status to %s: %v", TopicStatus, err)
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Publisher writes overrides to config.updated (the Aggregator only).
type Publisher struct {
	writer *kafkago.Writer
}

func NewPublisher(brokers []string) *Publisher {
	return &Publisher{writer: &kafkago.Writer{
		Addr: kafkago.TCP(brokers...), Topic: TopicUpdates, Balancer: &kafkago.Hash{},
		RequiredAcks: kafkago.RequireAll,
	}}
}

// Set publishes key's override.
func (p *Publisher) Set(ctx context.Context, key, value string, at time.Time) error {
	data, _ := json.Marshal(Update{Key: key, Value: value, UpdatedAt: at.UTC()})
	return p.writer.WriteMessages(ctx, kafkago.Message{Key: []byte(key), Value: data})
}

// Delete publishes key's tombstone: services drop the override and use app.yaml again.
func (p *Publisher) Delete(ctx context.Context, key string) error {
	return p.writer.WriteMessages(ctx, kafkago.Message{Key: []byte(key), Value: nil})
}

func (p *Publisher) Close() error { return p.writer.Close() }

// StatusReader follows config.status from the first offset and keeps the latest Status per process.
type StatusReader struct {
	mu       sync.Mutex
	statuses map[string]Status
}

// ReadStatuses starts following config.status in the background until ctx ends.
func ReadStatuses(ctx context.Context, brokers []string) *StatusReader {
	sr := &StatusReader{statuses: map[string]Status{}}
	r := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers: brokers, Topic: TopicStatus, Partition: 0, MinBytes: 1, MaxBytes: 1 << 20, MaxWait: time.Second,
	})
	_ = r.SetOffset(kafkago.FirstOffset)
	go func() {
		defer r.Close()
		for {
			msg, err := r.ReadMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				time.Sleep(2 * time.Second)
				continue
			}
			sr.mu.Lock()
			if msg.Value == nil {
				delete(sr.statuses, string(msg.Key))
			} else {
				var st Status
				if json.Unmarshal(msg.Value, &st) == nil {
					sr.statuses[string(msg.Key)] = st
				}
			}
			sr.mu.Unlock()
		}
	}()
	return sr
}

// Statuses returns the latest status of every process that reported.
func (sr *StatusReader) Statuses() map[string]Status {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	out := make(map[string]Status, len(sr.statuses))
	for k, v := range sr.statuses {
		out[k] = v
	}
	return out
}

// Brokers parses a comma-separated KAFKA_BROKERS value.
func Brokers(value string) []string {
	var out []string
	for _, b := range strings.Split(value, ",") {
		if b = strings.TrimSpace(b); b != "" {
			out = append(out, b)
		}
	}
	return out
}

// Run starts a Sync for store with the store's KAFKA_BROKERS (default localhost:9092): the one call
// a service's main needs after runtimeconfig.Start.
func Run(ctx context.Context, store *runtimeconfig.Store, service string) *Sync {
	brokers := Brokers(store.Getenv("KAFKA_BROKERS"))
	if len(brokers) == 0 {
		brokers = []string{"localhost:9092"}
	}
	s := New(store, brokers, service)
	s.Start(ctx)
	return s
}
