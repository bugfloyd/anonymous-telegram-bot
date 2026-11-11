package ephemeral

import (
	"context"
	"testing"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/dynamotest"
)

var start = time.Unix(1_800_000_000, 0)

func TestNonces(t *testing.T) {
	client := dynamotest.Client(t)
	clock := start
	n := &Nonces{DB: client, Table: dynamotest.Table(t, client), Now: func() time.Time { return clock }}
	ctx := context.Background()

	use := func(nonce string) bool {
		t.Helper()
		ok, err := n.Use(ctx, nonce, clock.Add(2*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !use("nonce-a") {
		t.Fatal("first use refused")
	}
	if use("nonce-a") {
		t.Fatal("replay accepted")
	}
	if !use("nonce-b") {
		t.Fatal("another nonce refused")
	}
	clock = start.Add(3 * time.Minute)
	if !use("nonce-a") {
		t.Fatal("expired nonce still blocked")
	}
}

func TestLimiter(t *testing.T) {
	client := dynamotest.Client(t)
	clock := start
	l := &Limiter{DB: client, Table: dynamotest.Table(t, client), Now: func() time.Time { return clock }}
	ctx := context.Background()

	allow := func(key string) bool {
		t.Helper()
		ok, err := l.Allow(ctx, key, 2, 24*time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !allow("k") || !allow("k") {
		t.Fatal("refused within the limit")
	}
	if allow("k") {
		t.Fatal("allowed past the limit")
	}
	if !allow("other") {
		t.Fatal("keys aren't separate")
	}
	clock = start.Add(24 * time.Hour)
	if !allow("k") {
		t.Fatal("next window refused")
	}
}
