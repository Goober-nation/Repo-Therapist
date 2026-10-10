package redis

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCooldownLetsOneThroughPerPeriod(t *testing.T) {
	client := testClient(t)
	cooldown := NewCooldown(client, time.Minute)
	ctx := context.Background()
	key := "it-cooldown-" + uuid.NewString()

	defer client.Del(ctx, key)

	if wait, err := cooldown.Acquire(ctx, key); err != nil || wait != 0 {
		t.Fatalf("first acquire: wait=%v err=%v, want through", wait, err)
	}

	wait, err := cooldown.Acquire(ctx, key)
	if err != nil || wait <= 0 || wait > time.Minute {
		t.Fatalf("second acquire: wait=%v err=%v, want the rest of the minute", wait, err)
	}
}
