package redis

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Needs a Redis; skipped without REDIS_TEST_ADDRESS (e.g. REDIS_TEST_ADDRESS=localhost:8052 with
// REDIS_TEST_PASSWORD set). Every key it writes carries a random suffix and is removed afterwards.
func testClient(t *testing.T) *redis.Client {
	t.Helper()

	address := os.Getenv("REDIS_TEST_ADDRESS")
	if address == "" {
		t.Skip("REDIS_TEST_ADDRESS not set")
	}

	client := redis.NewClient(&redis.Options{Addr: address, Password: os.Getenv("REDIS_TEST_PASSWORD")})
	t.Cleanup(func() { _ = client.Close() })

	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis unreachable: %v", err)
	}

	return client
}

func TestAdminGrantStoreRoundTrip(t *testing.T) {
	client := testClient(t)
	store := NewAdminGrantStore(client)
	ctx := context.Background()
	session := "it-" + uuid.NewString()

	defer store.Revoke(ctx, session)

	if fp, err := store.Fingerprint(ctx, session); err != nil || fp != "" {
		t.Fatalf("no grant yet: %q, %v", fp, err)
	}

	if err := store.Grant(ctx, session, "abc123", time.Hour); err != nil {
		t.Fatal(err)
	}

	if fp, err := store.Fingerprint(ctx, session); err != nil || fp != "abc123" {
		t.Fatalf("grant = %q, %v", fp, err)
	}

	ttl, err := client.TTL(ctx, grantKey(session)).Result()
	if err != nil || ttl <= 0 || ttl > time.Hour {
		t.Errorf("ttl = %v, %v: a grant must expire with the session", ttl, err)
	}

	if removed, err := store.Revoke(ctx, session); err != nil || !removed {
		t.Errorf("revoke = %v, %v", removed, err)
	}

	if removed, err := store.Revoke(ctx, session); err != nil || removed {
		t.Errorf("a second revoke must report nothing to remove, got %v, %v", removed, err)
	}

	if fp, _ := store.Fingerprint(ctx, session); fp != "" {
		t.Errorf("fingerprint after revoke = %q", fp)
	}
}

func TestAttemptGuardLocksAtTheLimitAndResets(t *testing.T) {
	client := testClient(t)
	guard := NewAttemptGuard(client, 3, 2*time.Second)
	ctx := context.Background()
	ip, user := "ip:it-"+uuid.NewString(), "user:it-"+uuid.NewString()

	defer func() {
		client.Del(ctx, failKey(ip), lockKey(ip), failKey(user), lockKey(user))
	}()

	for i := 1; i <= 2; i++ {
		locked, err := guard.RecordFailure(ctx, ip, user)
		if err != nil || locked {
			t.Fatalf("failure %d: locked %v, err %v", i, locked, err)
		}
	}

	if left, err := guard.LockedFor(ctx, ip, user); err != nil || left != 0 {
		t.Fatalf("below the limit: locked for %v, %v", left, err)
	}

	locked, err := guard.RecordFailure(ctx, ip, user)
	if err != nil || !locked {
		t.Fatalf("third failure: locked %v, err %v", locked, err)
	}

	left, err := guard.LockedFor(ctx, ip)
	if err != nil || left <= 0 || left > 2*time.Second {
		t.Errorf("ip lock = %v, %v", left, err)
	}

	if left, _ := guard.LockedFor(ctx, "ip:other-"+uuid.NewString(), user); left <= 0 {
		t.Error("the account key must lock on its own, whatever the IP")
	}

	if left, _ := guard.LockedFor(ctx, "ip:other-"+uuid.NewString(), "user:other-"+uuid.NewString()); left != 0 {
		t.Errorf("unrelated keys must not be locked, got %v", left)
	}

	time.Sleep(2200 * time.Millisecond)

	if left, err := guard.LockedFor(ctx, ip, user); err != nil || left != 0 {
		t.Errorf("after the lock period: still locked for %v, %v", left, err)
	}

	// A fresh count starts after the lock ended: one failure must not lock again.
	if locked, _ := guard.RecordFailure(ctx, ip); locked {
		t.Error("the count must restart after a lock")
	}

	if err := guard.Reset(ctx, ip); err != nil {
		t.Fatal(err)
	}

	if n, _ := client.Exists(ctx, failKey(ip)).Result(); n != 0 {
		t.Error("reset must forget the failures")
	}
}

func TestAttemptGuardFailureWindowExpires(t *testing.T) {
	client := testClient(t)
	guard := NewAttemptGuard(client, 3, time.Second)
	ctx := context.Background()
	key := "ip:it-window-" + uuid.NewString()

	defer client.Del(ctx, failKey(key), lockKey(key))

	_, _ = guard.RecordFailure(ctx, key)
	_, _ = guard.RecordFailure(ctx, key)

	time.Sleep(1200 * time.Millisecond)

	if locked, _ := guard.RecordFailure(ctx, key); locked {
		t.Error("old failures must age out: two, a pause, and one more is not three in a row")
	}
}

func TestSessionDeleteRoundTrip(t *testing.T) {
	client := testClient(t)
	prefix := "it-session-" + uuid.NewString() + ":"
	sessions := NewSessionRepository(client, time.Minute, prefix)
	ctx := context.Background()
	user := uuid.New()

	id, err := sessions.CreateSession(ctx, user)
	if err != nil {
		t.Fatal(err)
	}

	defer client.Del(ctx, prefix+id)

	if got, err := sessions.GetSession(ctx, id); err != nil || got != user {
		t.Fatalf("get = %v, %v", got, err)
	}

	if err := sessions.DeleteSession(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := sessions.GetSession(ctx, id); err == nil {
		t.Error("a deleted session must not be found")
	}

	if err := sessions.DeleteSession(ctx, id); err == nil {
		t.Error("deleting a missing session must report it (the logout treats that as done)")
	}
}
